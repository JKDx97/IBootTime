package httpboot

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"IBootTime/internal/config"
	"IBootTime/internal/isomgr"
	"IBootTime/internal/logger"
	"IBootTime/internal/session"
)

//go:embed assets/wimboot
var wimbootBin []byte

type Server struct {
	port       int
	serverIP   string
	httpServer *http.Server
	isoMgr     *isomgr.Manager
	log        *logger.Logger
	sessions   *session.Manager
	cfg        *config.Config

	// Windows mount fallback: isoPath -> driveLetter
	mountedISOs sync.Map
	// SMB shares created for Windows ISO install: shareName -> isoPath
	smbShares sync.Map
	// Cached modified boot.wim paths: isoPath -> cachedWimPath
	bootWimCache sync.Map
	// Results from background Windows ISO preparation: isoPath -> shareName (string)
	winPrepResults sync.Map
	// Drive letter assigned to each Windows ISO: isoPath -> driveLetter (string, e.g. "Z")
	winDriveLetters sync.Map
}

func New(port int, serverIP string, isoMgr *isomgr.Manager, log *logger.Logger, sessions *session.Manager, cfg *config.Config) *Server {
	return &Server{
		port:     port,
		serverIP: serverIP,
		isoMgr:   isoMgr,
		log:      log,
		sessions: sessions,
		cfg:      cfg,
	}
}

func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/boot.ipxe", s.handleBootScript)
	mux.HandleFunc("/grub.cfg", s.handleGRUBConfig)
	mux.HandleFunc("/wimboot", s.handleWimboot)
	mux.HandleFunc("/iso/", s.handleISOFile)
	mux.HandleFunc("/health", s.handleHealth)

	addr := fmt.Sprintf(":%d", s.port)

	s.httpServer = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext: func(l net.Listener) context.Context {
			return ctx
		},
	}

	s.log.Info("HTTP", "Starting HTTP boot server on %s", addr)

	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.log.Error("HTTP", "Server error: %v", err)
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.httpServer.Shutdown(shutdownCtx)
	}()

	// Pre-load cached boot.wim files from disk (instant — no DISM needed)
	s.preloadBootWimCache()

	// Phase 1: Mount ALL ISOs and create SMB shares (fast, ~5s per ISO)
	// Phase 2: DISM modify boot.wim for Windows ISOs (only if cache is stale)
	go func() {
		s.shareAllISOs()
		s.prepareWindowsBootWims()
	}()

	return nil
}

func (s *Server) Stop() {
	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.httpServer.Shutdown(ctx)
	}
	s.cleanupSMBShares()
	s.unmountAllISOs()
	s.log.Info("HTTP", "HTTP boot server stopped")
}

// cleanupSMBShares removes all SMB shares created for Windows ISO installation.
func (s *Server) cleanupSMBShares() {
	s.smbShares.Range(func(key, value interface{}) bool {
		shareName := key.(string)
		s.log.Info("HTTP", "Removing SMB share: %s", shareName)
		exec.Command("powershell", "-NoProfile", "-Command",
			fmt.Sprintf("Remove-SmbShare -Name '%s' -Force -ErrorAction SilentlyContinue", shareName)).Run()
		s.smbShares.Delete(key)
		return true
	})
}

const smbUser = "Administrador"
const smbPass = "P0s31d0n"
const cacheVersion = "v11-serial-dism"

// preloadBootWimCache scans disk for previously cached boot.wim files and
// populates in-memory maps so modified boot.wim is served instantly on startup.
func (s *Server) preloadBootWimCache() {
	isos := s.isoMgr.ListEnabled()
	nextLetter := byte('Z')
	for _, iso := range isos {
		if iso.OSType != isomgr.OSTypeWindows && iso.OSType != isomgr.OSTypeWinPE {
			continue
		}
		// Assign drive letter
		letter := string(nextLetter)
		s.winDriveLetters.Store(iso.Path, letter)
		if nextLetter > 'N' {
			nextLetter--
		}

		// Check if valid cached boot.wim exists on disk
		cacheID := sanitizeMenuID(iso.Name)
		isoDir := filepath.Dir(iso.Path)
		cacheDir := filepath.Join(isoDir, ".bootcache", cacheID)
		cachedWim := filepath.Join(cacheDir, "boot.wim")
		versionFile := filepath.Join(cacheDir, ".version")

		if _, err := os.Stat(cachedWim); err != nil {
			continue
		}
		if data, err := os.ReadFile(versionFile); err != nil || string(data) != cacheVersion {
			continue
		}

		s.bootWimCache.Store(iso.Path, cachedWim)
		s.log.Info("HTTP", "Pre-loaded cached boot.wim for %s (drive %s:)", iso.Name, letter)
	}
}

// shareAllISOs mounts ALL enabled ISOs and creates SMB shares immediately.
// This is fast (~5s per ISO) and runs at server start.
func (s *Server) shareAllISOs() {
	s.log.Info("HTTP", "=== Creating SMB shares for all ISOs ===")

	isos := s.isoMgr.ListEnabled()
	for _, iso := range isos {
		shareName := "IB_" + sanitizeMenuID(iso.Name)

		// Check if share already exists
		if _, exists := s.smbShares.Load(shareName); exists {
			continue
		}

		// Mount the ISO
		driveLetter, err := s.mountISO(iso.Path)
		if err != nil {
			s.log.Warn("HTTP", "Could not mount %s: %v", iso.Name, err)
			continue
		}

		// Remove existing share (idempotent)
		exec.Command("powershell", "-NoProfile", "-Command",
			fmt.Sprintf("Remove-SmbShare -Name '%s' -Force -ErrorAction SilentlyContinue", shareName)).Run()

		// Create share with FullAccess for Everyone (visible in network + accessible)
		smbCmd := exec.Command("powershell", "-NoProfile", "-Command",
			fmt.Sprintf(
				"$sid = New-Object System.Security.Principal.SecurityIdentifier('S-1-1-0'); "+
					"$everyone = $sid.Translate([System.Security.Principal.NTAccount]).Value; "+
					"New-SmbShare -Name '%s' -Path '%s:\\' -FullAccess $everyone",
				shareName, driveLetter))
		if out, err := smbCmd.CombinedOutput(); err != nil {
			s.log.Warn("HTTP", "SMB share failed for %s: %s", iso.Name, strings.TrimSpace(string(out)))
		} else {
			s.smbShares.Store(shareName, iso.Path)
			s.log.Info("HTTP", "SMB share: \\\\%s\\%s -> %s:\\ (%s)", s.serverIP, shareName, driveLetter, iso.Name)
		}
	}
	s.log.Info("HTTP", "=== All %d SMB shares created ===", s.countShares())
}

func (s *Server) countShares() int {
	count := 0
	s.smbShares.Range(func(_, _ interface{}) bool { count++; return true })
	return count
}

// prepareWindowsBootWims modifies boot.wim for Windows ISOs with DISM (slow).
// Runs in background after shares are created.
func (s *Server) prepareWindowsBootWims() {
	isos := s.isoMgr.ListEnabled()
	nextLetter := byte('Z')
	// DISM operations MUST run sequentially — concurrent WIM mounts cause failures
	for _, iso := range isos {
		if iso.OSType != isomgr.OSTypeWindows && iso.OSType != isomgr.OSTypeWinPE {
			continue
		}
		letter := string(nextLetter)
		s.winDriveLetters.Store(iso.Path, letter)
		if nextLetter > 'N' {
			nextLetter--
		}
		shareName, err := s.prepareWindowsInstall(&iso, letter)
		if err != nil {
			s.log.Warn("HTTP", "DISM prep failed for %s: %v", iso.Name, err)
			s.winPrepResults.Store(iso.Path, "")
		} else {
			s.winPrepResults.Store(iso.Path, shareName)
			s.log.Info("HTTP", "DISM prep complete for %s (share: %s, drive: %s:)", iso.Name, shareName, letter)
		}
	}
	s.log.Info("HTTP", "All Windows boot.wim preparations finished")
}

// prepareWindowsInstall mounts a Windows ISO, creates an SMB share, and
// produces a modified boot.wim with an injected startnet.cmd that automatically
// connects to the SMB share and launches setup.exe. Returns the share name.
func (s *Server) prepareWindowsInstall(iso *isomgr.ISOInfo, driveLetter string) (string, error) {
	// Check if already prepared
	if cached, ok := s.bootWimCache.Load(iso.Path); ok {
		shareName := "IB_" + sanitizeMenuID(iso.Name)
		if _, exists := s.smbShares.Load(shareName); exists {
			s.log.Info("HTTP", "Windows install already prepared for %s (cache: %s)", iso.Name, cached)
			return shareName, nil
		}
	}

	s.log.Info("HTTP", "=== Preparing Windows install for %s ===", iso.Name)

	// 1. Mount ISO
	isoDrive, err := s.mountISO(iso.Path)
	if err != nil {
		return "", fmt.Errorf("mount ISO: %w", err)
	}

	// 2. Verify boot.wim exists on mounted drive
	srcBootWim := ""
	for _, candidate := range []string{"sources\\boot.wim", "Sources\\boot.wim", "SOURCES\\BOOT.WIM"} {
		p := filepath.Join(isoDrive+":\\", candidate)
		if _, err := os.Stat(p); err == nil {
			srcBootWim = p
			break
		}
	}
	if srcBootWim == "" {
		return "", fmt.Errorf("boot.wim not found on mounted ISO %s (%s:\\)", iso.Name, isoDrive)
	}
	s.log.Info("HTTP", "Found boot.wim: %s", srcBootWim)

	// 3. Create cache directory
	isoDir := filepath.Dir(iso.Path)
	cacheID := sanitizeMenuID(iso.Name)
	cacheDir := filepath.Join(isoDir, ".bootcache", cacheID)
	mountDir := filepath.Join(cacheDir, "mount")
	cachedWim := filepath.Join(cacheDir, "boot.wim")

	if err := os.MkdirAll(mountDir, 0755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}

	// 4. Check if cached boot.wim already exists and is fresh
	isoStat, _ := os.Stat(iso.Path)
	wimStat, wimErr := os.Stat(cachedWim)
	needRebuild := wimErr != nil || (isoStat != nil && wimStat.ModTime().Before(isoStat.ModTime()))

	// Version marker — if injection logic changes, bump this to force rebuild
	versionFile := filepath.Join(cacheDir, ".version")
	if versionData, err := os.ReadFile(versionFile); err == nil && string(versionData) == cacheVersion {
		needRebuild = needRebuild // keep existing decision
	} else {
		needRebuild = true // version mismatch or missing, force rebuild
	}

	if needRebuild {
		s.log.Info("HTTP", "Building modified boot.wim for %s (cache: %s)...", iso.Name, cacheDir)

		// Remove old cached wim if exists
		os.Remove(cachedWim)
		os.Remove(versionFile)

		// Copy boot.wim to cache
		if err := copyFile(srcBootWim, cachedWim); err != nil {
			return "", fmt.Errorf("copy boot.wim: %w", err)
		}

		// Remove read-only attribute (ISO files are read-only)
		exec.Command("attrib", "-r", cachedWim).Run()

		// Get WIM info to see available indexes
		wimInfoCmd := exec.Command("dism", "/get-wiminfo", "/wimfile:"+cachedWim)
		wimInfoOut, _ := wimInfoCmd.CombinedOutput()
		wimInfoStr := string(wimInfoOut)
		s.log.Info("HTTP", "WIM info for %s:\n%s", iso.Name, wimInfoStr)

		// Count indexes — handle multiple DISM output languages/encodings:
		// English: "Index : 2", Spanish: "Índice: 2" (or garbled encoding)
		maxIndex := 1
		for i := 10; i >= 2; i-- {
			n := fmt.Sprintf("%d", i)
			if strings.Contains(wimInfoStr, "Index : "+n) ||
				strings.Contains(wimInfoStr, "Index: "+n) ||
				strings.Contains(wimInfoStr, "ndice: "+n) ||
				strings.Contains(wimInfoStr, "ndice : "+n) ||
				strings.Contains(wimInfoStr, "dice: "+n) ||
				strings.Contains(wimInfoStr, "dice : "+n) {
				maxIndex = i
				break
			}
		}
		s.log.Info("HTTP", "Detected %d WIM index(es) in boot.wim", maxIndex)

		shareName := "IB_" + cacheID

		// Modify each index: inject startnet.cmd + winpeshl.ini
		// Index 2 (Windows Setup) is what the BCD boots — CRITICAL to modify
		// Index 1 (WinPE) is fallback
		for idx := maxIndex; idx >= 1; idx-- {
			idxMountDir := filepath.Join(cacheDir, fmt.Sprintf("mount_%d", idx))
			os.MkdirAll(idxMountDir, 0755)

			// Clean any stale mount
			exec.Command("dism", "/unmount-wim", "/mountdir:"+idxMountDir, "/discard").Run()

			s.log.Info("HTTP", "DISM: mounting boot.wim index %d...", idx)
			dismMount := exec.Command("dism", "/mount-wim",
				"/wimfile:"+cachedWim, fmt.Sprintf("/index:%d", idx), "/mountdir:"+idxMountDir)
			if out, err := dismMount.CombinedOutput(); err != nil {
				s.log.Warn("HTTP", "DISM mount index %d failed (may not exist): %v\n%s", idx, err, string(out))
				continue
			}
			s.log.Info("HTTP", "DISM mount index %d OK", idx)

			// -- Inject NIC drivers (critical for Win11 WinPE networking) --
			if driverDir := s.findDriversDir(); driverDir != "" {
				s.log.Info("HTTP", "DISM: injecting drivers from %s into index %d...", driverDir, idx)
				dismDriver := exec.Command("dism", "/image:"+idxMountDir,
					"/add-driver", "/driver:"+driverDir, "/recurse", "/forceunsigned")
				if out, err := dismDriver.CombinedOutput(); err != nil {
					s.log.Warn("HTTP", "Driver injection index %d (non-fatal): %v\n%s", idx, err, strings.TrimSpace(string(out)))
				} else {
					s.log.Info("HTTP", "Drivers injected into index %d OK", idx)
				}
			}

			// -- Inject startnet.cmd --
			startnetPath := filepath.Join(idxMountDir, "Windows", "System32", "startnet.cmd")
			var startnetContent string

			if idx >= 2 {
				// Index 2+ (Windows Setup): wpeinit handled by winpeshl.ini.
				// startnet.cmd: force network init, wait for network, map drive via IP with user/pass, launch setup.
				startnetContent = fmt.Sprintf("@echo off\r\n"+
					"echo [IBootTime] Inicializando red...\r\n"+
					"wpeutil initializenetwork >nul 2>&1\r\n"+
					"wpeutil waitfornetwork >nul 2>&1\r\n"+
					"ping -n 3 127.0.0.1 >nul\r\n"+
					"ipconfig /renew >nul 2>&1\r\n"+
					"ping -n 2 127.0.0.1 >nul\r\n"+
					"echo.\r\n"+
					"ipconfig\r\n"+
					"echo.\r\n"+
					"echo [IBootTime] Esperando conexion con %s ...\r\n"+
					":waitnet\r\n"+
					"ping -n 1 -w 1000 %s >nul 2>&1\r\n"+
					"if errorlevel 1 goto waitnet\r\n"+
					"echo [IBootTime] Conectando \\\\%s\\%s ...\r\n"+
					"set R=0\r\n"+
					":retry\r\n"+
					"net use %s: \\\\%s\\%s /user:%s %s /persistent:yes >nul 2>&1\r\n"+
					"if not errorlevel 1 goto ok\r\n"+
					"set /a R+=1\r\n"+
					"if %%R%% GEQ 20 goto fail\r\n"+
					"echo [IBootTime] Reintentando (%%R%%/20)...\r\n"+
					"ping -n 3 127.0.0.1 >nul\r\n"+
					"goto retry\r\n"+
					":ok\r\n"+
					"echo [IBootTime] %s: conectada. Lanzando instalador...\r\n"+
					"%s:\\setup.exe\r\n"+
					"echo.\r\n"+
					"echo [IBootTime] El instalador se cerro o fallo.\r\n"+
					"echo Para reintentar: %s:\\setup.exe\r\n"+
					"cmd /k\r\n"+
					":fail\r\n"+
					"echo [IBootTime] Error de conexion.\r\n"+
					"echo.\r\n"+
					"ipconfig\r\n"+
					"echo.\r\n"+
					"echo Escribe: net use %s: \\\\%s\\%s /user:%s %s\r\n"+
					"echo Luego: %s:\\setup.exe\r\n"+
					"cmd /k\r\n",
					s.serverIP,
					s.serverIP,
					s.serverIP, shareName,
					driveLetter, s.serverIP, shareName, smbUser, smbPass,
					driveLetter,
					driveLetter,
					driveLetter,
					driveLetter, s.serverIP, shareName, smbUser, smbPass,
					driveLetter,
				)
			} else {
				// Index 1 (plain WinPE): wpeinit + network init + wait for IP + net use
				startnetContent = fmt.Sprintf("@echo off\r\n"+
					"echo [IBootTime] Inicializando red...\r\n"+
					"wpeinit\r\n"+
					"wpeutil initializenetwork >nul 2>&1\r\n"+
					"wpeutil waitfornetwork >nul 2>&1\r\n"+
					"ping -n 3 127.0.0.1 >nul\r\n"+
					"ipconfig /renew >nul 2>&1\r\n"+
					"ping -n 2 127.0.0.1 >nul\r\n"+
					"echo.\r\n"+
					"ipconfig\r\n"+
					"echo.\r\n"+
					"echo [IBootTime] Esperando conexion con %s ...\r\n"+
					":waitnet\r\n"+
					"ping -n 1 -w 1000 %s >nul 2>&1\r\n"+
					"if errorlevel 1 goto waitnet\r\n"+
					"echo [IBootTime] Conectando \\\\%s\\%s ...\r\n"+
					"set RETRIES=0\r\n"+
					":retry\r\n"+
					"net use %s: \\\\%s\\%s /user:%s %s /persistent:yes >nul 2>&1\r\n"+
					"if not errorlevel 1 goto connected\r\n"+
					"set /a RETRIES+=1\r\n"+
					"if %%RETRIES%% GEQ 15 goto manual\r\n"+
					"echo [IBootTime] Reintentando (%%RETRIES%%/15)...\r\n"+
					"ping -n 3 127.0.0.1 >nul\r\n"+
					"goto retry\r\n"+
					":connected\r\n"+
					"echo [IBootTime] %s: conectada. Lanzando instalador...\r\n"+
					"%s:\\setup.exe\r\n"+
					"echo.\r\n"+
					"echo [IBootTime] El instalador se cerro o fallo.\r\n"+
					"echo Para reintentar: %s:\\setup.exe\r\n"+
					"cmd /k\r\n"+
					":manual\r\n"+
					"echo [IBootTime] Error de conexion.\r\n"+
					"echo.\r\n"+
					"ipconfig\r\n"+
					"echo.\r\n"+
					"echo Escribe: net use %s: \\\\%s\\%s /user:%s %s\r\n"+
					"echo Luego: %s:\\setup.exe\r\n"+
					"cmd /k\r\n",
					s.serverIP,
					s.serverIP,
					s.serverIP, shareName,
					driveLetter, s.serverIP, shareName, smbUser, smbPass,
					driveLetter,
					driveLetter,
					driveLetter,
					driveLetter, s.serverIP, shareName, smbUser, smbPass,
					driveLetter,
				)
			}

			if err := os.WriteFile(startnetPath, []byte(startnetContent), 0644); err != nil {
				s.log.Warn("HTTP", "Failed to write startnet.cmd for index %d: %v", idx, err)
				exec.Command("dism", "/unmount-wim", "/mountdir:"+idxMountDir, "/discard").Run()
				continue
			}
			s.log.Info("HTTP", "Injected startnet.cmd into index %d", idx)

			// -- For Index 2+: also inject/modify winpeshl.ini --
			// This controls what runs after WinPE boots.
			// Instead of X:\sources\setup.exe (which can't find install.wim),
			// we run startnet.cmd first (maps Z:\), then Z:\setup.exe.
			if idx >= 2 {
				// winpeshl.ini controls WinPE boot sequence:
				// 1. wpeinit.exe initializes networking
				// 2. cmd.exe /c startnet.cmd maps SMB + launches Z:\setup.exe
				winpeshlPath := filepath.Join(idxMountDir, "Windows", "System32", "winpeshl.ini")
				winpeshlContent := "[LaunchApps]\r\n" +
					"%SYSTEMROOT%\\System32\\wpeinit.exe\r\n" +
					"%SYSTEMROOT%\\System32\\cmd.exe, /c %SYSTEMROOT%\\System32\\startnet.cmd\r\n"
				if err := os.WriteFile(winpeshlPath, []byte(winpeshlContent), 0644); err != nil {
					s.log.Warn("HTTP", "Failed to write winpeshl.ini for index %d: %v", idx, err)
				} else {
					s.log.Info("HTTP", "Injected winpeshl.ini into index %d (wpeinit -> cmd /c startnet.cmd)", idx)
				}
			}

			// -- Commit changes --
			s.log.Info("HTTP", "DISM: committing index %d...", idx)
			dismUnmount := exec.Command("dism", "/unmount-wim",
				"/mountdir:"+idxMountDir, "/commit")
			if out, err := dismUnmount.CombinedOutput(); err != nil {
				s.log.Error("HTTP", "DISM commit index %d failed: %s\n%s", idx, err, string(out))
				exec.Command("dism", "/unmount-wim", "/mountdir:"+idxMountDir, "/discard").Run()
			} else {
				s.log.Info("HTTP", "DISM commit index %d OK", idx)
			}
		}

		// Write version marker
		os.WriteFile(versionFile, []byte(cacheVersion), 0644)
		s.log.Info("HTTP", "Modified boot.wim saved to cache (version: %s)", cacheVersion)
	} else {
		s.log.Info("HTTP", "Using cached boot.wim for %s (version OK)", iso.Name)
	}

	// 9. Share already created by shareAllISOs — just cache the modified boot.wim
	shareName := "IB_" + cacheID

	// Cache the modified boot.wim path
	s.bootWimCache.Store(iso.Path, cachedWim)

	s.log.Info("HTTP", "=== Windows install ready: %s (share: \\\\%s\\%s) ===", iso.Name, s.serverIP, shareName)
	return shareName, nil
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

// findDriversDir locates the drivers/ directory for NIC driver injection.
// Searches relative to the executable and the working directory.
func (s *Server) findDriversDir() string {
	candidates := []string{"drivers"}
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		candidates = append([]string{
			filepath.Join(exeDir, "drivers"),
			filepath.Join(exeDir, "..", "drivers"),
		}, candidates...)
	}
	for _, dir := range candidates {
		absDir, _ := filepath.Abs(dir)
		if info, err := os.Stat(absDir); err == nil && info.IsDir() {
			return absDir
		}
	}
	return ""
}

// mountISO mounts an ISO using Windows' built-in Mount-DiskImage and returns
// the drive letter. This is used as fallback when the ISO9660 library cannot
// read the filesystem (e.g. UDF-only Windows 11 ISOs).
func (s *Server) mountISO(isoPath string) (string, error) {
	// Check cache first
	if dl, ok := s.mountedISOs.Load(isoPath); ok {
		return dl.(string), nil
	}

	s.log.Info("HTTP", "Mounting ISO via Windows: %s", filepath.Base(isoPath))

	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		fmt.Sprintf("(Mount-DiskImage -ImagePath '%s' -PassThru | Get-Volume).DriveLetter", isoPath))
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("mount failed: %w", err)
	}

	driveLetter := strings.TrimSpace(string(out))
	if driveLetter == "" {
		return "", fmt.Errorf("mount returned empty drive letter")
	}

	s.mountedISOs.Store(isoPath, driveLetter)
	s.log.Info("HTTP", "ISO mounted at %s:\\ -> %s", driveLetter, filepath.Base(isoPath))
	return driveLetter, nil
}

// unmountAllISOs unmounts all ISOs that were mounted during this session.
func (s *Server) unmountAllISOs() {
	s.mountedISOs.Range(func(key, value interface{}) bool {
		isoPath := key.(string)
		s.log.Info("HTTP", "Unmounting ISO: %s", filepath.Base(isoPath))
		cmd := exec.Command("powershell", "-NoProfile", "-Command",
			fmt.Sprintf("Dismount-DiskImage -ImagePath '%s'", isoPath))
		cmd.Run()
		s.mountedISOs.Delete(key)
		return true
	})
}

func (s *Server) handleBootScript(w http.ResponseWriter, r *http.Request) {
	clientIP := strings.Split(r.RemoteAddr, ":")[0]
	mac := r.URL.Query().Get("mac")
	arch := r.URL.Query().Get("arch")

	s.log.Info("HTTP", ">>> boot.ipxe requested by %s (MAC=%s arch=%s UA=%s)", clientIP, mac, arch, r.UserAgent())

	if mac != "" {
		s.sessions.Register(mac, clientIP, arch)
		s.sessions.UpdateState(mac, session.StateMenu)
	}

	isos := s.isoMgr.ListEnabled()

	var script strings.Builder
	script.WriteString("#!ipxe\n\n")

	// Variables
	script.WriteString("set menu-timeout 0\n")
	script.WriteString("set server-ip " + s.serverIP + "\n")
	script.WriteString(fmt.Sprintf("set http-root http://${server-ip}:%d\n", s.port))
	script.WriteString("set boot-url ${http-root}\n\n")

	// Menu
	script.WriteString(":start\n")
	script.WriteString("menu IBootTime - Network Boot Server [${server-ip}]\n")
	script.WriteString("item --gap --             ========================================\n")
	script.WriteString("item --gap --                    Sistema de Boot por Red\n")
	script.WriteString("item --gap --             ========================================\n")

	if len(isos) > 0 {
		script.WriteString("item --gap --\n")
		script.WriteString("item --gap -- --- ISOs disponibles ---\n")
		for _, iso := range isos {
			itemID := sanitizeMenuID(iso.Name)
			label := fmt.Sprintf("  %s  [%s] (%s)", iso.Name, iso.OSType, iso.SizeHR)
			script.WriteString(fmt.Sprintf("item %s %s\n", itemID, label))
		}
	}

	script.WriteString("item --gap --\n")
	script.WriteString("item --gap -- ========================================\n")
	script.WriteString("item reboot           Reiniciar equipo\n")
	script.WriteString("item shell            iPXE Shell\n")
	script.WriteString("item exit             Salir al firmware\n")
	script.WriteString("choose --timeout ${menu-timeout} --default reboot selected || goto exit\n")
	script.WriteString("goto ${selected}\n\n")

	// === Real ISO handlers ===
	for _, iso := range isos {
		itemID := sanitizeMenuID(iso.Name)
		encodedName := strings.ReplaceAll(iso.Name, " ", "%20")
		script.WriteString(fmt.Sprintf(":%s\n", itemID))
		script.WriteString(fmt.Sprintf("echo Cargando %s ...\n", iso.Name))

		switch iso.OSType {
		case isomgr.OSTypeWindows, isomgr.OSTypeWinPE:
			// Windows: wimboot loads BCD + boot.sdi + boot.wim via HTTP.
			// boot.wim endpoint auto-serves modified version (with startnet.cmd
			// that does net use) when ready, or original from ISO otherwise.
			shareName := "IB_" + sanitizeMenuID(iso.Name)
			dl := "Z"
			if letter, ok := s.winDriveLetters.Load(iso.Path); ok {
				dl = letter.(string)
			}

			script.WriteString("imgfree\n")
			script.WriteString(fmt.Sprintf("echo Instalacion de %s via red...\n", iso.Name))
			// Detect UEFI vs BIOS — wimboot on UEFI REQUIRES bootx64.efi
			script.WriteString(fmt.Sprintf("iseq ${platform} efi && goto win_uefi_%s ||\n\n", itemID))

			// ==== BIOS path ====
			script.WriteString(fmt.Sprintf(":win_bios_%s\n", itemID))
			script.WriteString(fmt.Sprintf("kernel ${boot-url}/wimboot || goto winfail_%s\n", itemID))
			// BCD: try boot/bcd (BIOS) first, then efi/microsoft/boot/bcd
			script.WriteString(fmt.Sprintf("initrd --name BCD ${boot-url}/iso/%s/file/boot/bcd BCD || goto win_bcd_efi_%s\n", encodedName, itemID))
			script.WriteString(fmt.Sprintf("goto win_sdi_%s\n", itemID))
			script.WriteString(fmt.Sprintf(":win_bcd_efi_%s\n", itemID))
			script.WriteString(fmt.Sprintf("initrd --name BCD ${boot-url}/iso/%s/file/efi/microsoft/boot/bcd BCD || goto winfail_%s\n", encodedName, itemID))
			script.WriteString(fmt.Sprintf(":win_sdi_%s\n", itemID))
			script.WriteString(fmt.Sprintf("initrd --name boot.sdi ${boot-url}/iso/%s/file/boot/boot.sdi boot.sdi || goto winfail_%s\n", encodedName, itemID))
			script.WriteString(fmt.Sprintf("initrd --name boot.wim ${boot-url}/iso/%s/file/sources/boot.wim boot.wim || goto winfail_%s\n", encodedName, itemID))
			script.WriteString(fmt.Sprintf("boot || goto winfail_%s\n\n", itemID))

			// ==== UEFI path ====
			script.WriteString(fmt.Sprintf(":win_uefi_%s\n", itemID))
			script.WriteString(fmt.Sprintf("kernel ${boot-url}/wimboot || goto winfail_%s\n", itemID))
			// UEFI: bootx64.efi is REQUIRED for wimboot to hand off to Windows Boot Manager
			script.WriteString(fmt.Sprintf("initrd --name bootx64.efi ${boot-url}/iso/%s/file/efi/boot/bootx64.efi bootx64.efi || goto winfail_%s\n", encodedName, itemID))
			// BCD: try efi/microsoft/boot/bcd first, then boot/bcd
			script.WriteString(fmt.Sprintf("initrd --name BCD ${boot-url}/iso/%s/file/efi/microsoft/boot/bcd BCD || goto win_uefi_bcd_%s\n", encodedName, itemID))
			script.WriteString(fmt.Sprintf("goto win_uefi_sdi_%s\n", itemID))
			script.WriteString(fmt.Sprintf(":win_uefi_bcd_%s\n", itemID))
			script.WriteString(fmt.Sprintf("initrd --name BCD ${boot-url}/iso/%s/file/boot/bcd BCD || goto winfail_%s\n", encodedName, itemID))
			script.WriteString(fmt.Sprintf(":win_uefi_sdi_%s\n", itemID))
			script.WriteString(fmt.Sprintf("initrd --name boot.sdi ${boot-url}/iso/%s/file/boot/boot.sdi boot.sdi || goto winfail_%s\n", encodedName, itemID))
			script.WriteString(fmt.Sprintf("initrd --name boot.wim ${boot-url}/iso/%s/file/sources/boot.wim boot.wim || goto winfail_%s\n", encodedName, itemID))
			script.WriteString(fmt.Sprintf("boot || goto winfail_%s\n", itemID))
			// Failure handler
			script.WriteString(fmt.Sprintf(":winfail_%s\n", itemID))
			script.WriteString("echo\n")
			script.WriteString("echo =============================================\n")
			script.WriteString(fmt.Sprintf("echo   Error cargando %s\n", iso.Name))
			script.WriteString("echo   Si llegas a Windows PE, presiona Shift+F10:\n")
			script.WriteString("echo     wpeinit\n")
			script.WriteString(fmt.Sprintf("echo     net use %s: \\\\%s\\%s /user:%s %s\n", dl, s.serverIP, shareName, smbUser, smbPass))
			script.WriteString(fmt.Sprintf("echo     %s:\\setup.exe\n", dl))
			script.WriteString("echo =============================================\n")
			script.WriteString("prompt Presiona ENTER para volver al menu...\n")
			script.WriteString("goto start\n\n")

		case isomgr.OSTypeLinux:
			// Linux: try kernel+initrd (casper, live, isolinux), sanboot fallback
			script.WriteString("imgfree\n")
			// Ubuntu/Casper
			script.WriteString(fmt.Sprintf("kernel ${boot-url}/iso/%s/file/casper/vmlinuz boot=casper netboot=url url=${boot-url}/iso/%s/raw ip=dhcp -- || goto linux_live_%s\n", encodedName, encodedName, itemID))
			script.WriteString(fmt.Sprintf("initrd ${boot-url}/iso/%s/file/casper/initrd || goto linux_live_%s\n", encodedName, itemID))
			script.WriteString(fmt.Sprintf("boot || goto sanboot_%s\n\n", itemID))
			// Debian/Live
			script.WriteString(fmt.Sprintf(":linux_live_%s\n", itemID))
			script.WriteString("imgfree\n")
			script.WriteString(fmt.Sprintf("kernel ${boot-url}/iso/%s/file/live/vmlinuz boot=live fetch=${boot-url}/iso/%s/file/live/filesystem.squashfs ip=dhcp -- || goto linux_isolinux_%s\n", encodedName, encodedName, itemID))
			script.WriteString(fmt.Sprintf("initrd ${boot-url}/iso/%s/file/live/initrd.img || goto linux_isolinux_%s\n", encodedName, itemID))
			script.WriteString(fmt.Sprintf("boot || goto sanboot_%s\n\n", itemID))
			// Isolinux/Syslinux
			script.WriteString(fmt.Sprintf(":linux_isolinux_%s\n", itemID))
			script.WriteString("imgfree\n")
			script.WriteString(fmt.Sprintf("kernel ${boot-url}/iso/%s/file/isolinux/vmlinuz -- || goto sanboot_%s\n", encodedName, itemID))
			script.WriteString(fmt.Sprintf("initrd ${boot-url}/iso/%s/file/isolinux/initrd.img || goto sanboot_%s\n", encodedName, itemID))
			script.WriteString(fmt.Sprintf("boot || goto sanboot_%s\n\n", itemID))
			// Fallback: SAN boot
			script.WriteString(fmt.Sprintf(":sanboot_%s\n", itemID))
			script.WriteString(fmt.Sprintf("echo Kernel no encontrado, usando SAN boot para %s...\n", iso.Name))
			script.WriteString("imgfree\n")
			script.WriteString(fmt.Sprintf("sanboot --no-describe ${boot-url}/iso/%s/raw || goto failed\n\n", encodedName))

		default:
			// Unknown: SAN boot directly
			script.WriteString("imgfree\n")
			script.WriteString(fmt.Sprintf("sanboot --no-describe ${boot-url}/iso/%s/raw || goto failed\n\n", encodedName))
		}
	}

	// === Utility handlers ===
	script.WriteString(":failed\n")
	script.WriteString("echo\n")
	script.WriteString("echo ERROR: No se pudo arrancar la imagen seleccionada.\n")
	script.WriteString("echo Verifica que la ISO este correctamente configurada.\n")
	script.WriteString("prompt Presiona ENTER para volver al menu...\n")
	script.WriteString("goto start\n\n")

	script.WriteString(":reboot\nreboot\n\n")
	script.WriteString(":shell\necho Escribe 'exit' para volver al menu\nshell\ngoto start\n\n")
	script.WriteString(":exit\nexit\n")

	scriptContent := script.String()
	s.log.Info("HTTP", "<<< Serving boot.ipxe (%d bytes, %d ISOs) to %s", len(scriptContent), len(isos), clientIP)

	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(scriptContent))
}

func (s *Server) handleGRUBConfig(w http.ResponseWriter, r *http.Request) {
	clientIP := strings.Split(r.RemoteAddr, ":")[0]
	s.log.Info("HTTP", ">>> grub.cfg requested by %s", clientIP)

	isos := s.isoMgr.ListEnabled()

	var cfg strings.Builder
	cfg.WriteString("# GRUB Configuration - Generated by IBootTime\n")
	cfg.WriteString("set default=0\n")
	cfg.WriteString("set timeout=30\n")
	cfg.WriteString(fmt.Sprintf("set server_ip=%s\n", s.serverIP))
	cfg.WriteString(fmt.Sprintf("set http_port=%d\n", s.port))
	cfg.WriteString("\n")

	cfg.WriteString("menuentry \"========================================\" {\n  true\n}\n")
	cfg.WriteString("menuentry \"     IBootTime - Network Boot Server\" {\n  true\n}\n")
	cfg.WriteString("menuentry \"========================================\" {\n  true\n}\n\n")

	if len(isos) > 0 {
		for _, iso := range isos {
			cfg.WriteString(fmt.Sprintf("menuentry \"%s [%s] (%s)\" {\n", iso.Name, iso.OSType, iso.SizeHR))

			switch iso.OSType {
			case isomgr.OSTypeWindows, isomgr.OSTypeWinPE:
				cfg.WriteString(fmt.Sprintf("  echo \"Cargando %s...\"\n", iso.Name))
				cfg.WriteString(fmt.Sprintf("  set isofile=\"(http,$server_ip:$http_port)/iso/%s/raw\"\n", iso.Name))
				cfg.WriteString("  loopback loop $isofile\n")
				cfg.WriteString("  set root=(loop)\n")
				cfg.WriteString("  chainloader /efi/boot/bootx64.efi\n")
			case isomgr.OSTypeLinux:
				cfg.WriteString(fmt.Sprintf("  echo \"Cargando %s...\"\n", iso.Name))
				cfg.WriteString(fmt.Sprintf("  set isofile=\"(http,$server_ip:$http_port)/iso/%s/raw\"\n", iso.Name))
				cfg.WriteString("  loopback loop $isofile\n")
				cfg.WriteString("  linux (loop)/casper/vmlinuz boot=casper iso-scan/filename=$isofile\n")
				cfg.WriteString("  initrd (loop)/casper/initrd\n")
			default:
				cfg.WriteString(fmt.Sprintf("  echo \"Cargando %s...\"\n", iso.Name))
				cfg.WriteString(fmt.Sprintf("  set isofile=\"(http,$server_ip:$http_port)/iso/%s/raw\"\n", iso.Name))
				cfg.WriteString("  loopback loop $isofile\n")
			}

			cfg.WriteString("  boot\n")
			cfg.WriteString("}\n\n")
		}
	}

	cfg.WriteString("menuentry \"Reiniciar equipo\" {\n  reboot\n}\n\n")
	cfg.WriteString("menuentry \"Apagar equipo\" {\n  halt\n}\n")

	grubCfg := cfg.String()
	s.log.Info("HTTP", "<<< Serving grub.cfg (%d bytes, %d ISOs) to %s", len(grubCfg), len(isos), clientIP)

	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(grubCfg))
}

func (s *Server) handleISOFile(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/iso/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}

	isoName := parts[0]
	action := parts[1]

	iso, err := s.isoMgr.GetByName(isoName)
	if err != nil {
		s.log.Warn("HTTP", "ISO not found: %s", isoName)
		http.NotFound(w, r)
		return
	}

	clientIP := strings.Split(r.RemoteAddr, ":")[0]
	// Only log non-raw requests to avoid flooding from sanboot range requests
	if action != "raw" {
		s.log.Info("HTTP", "ISO request: %s/%s from %s", isoName, action, clientIP)
	}

	switch {
	case action == "raw":
		s.serveRawISO(w, r, iso)
	case action == "tree":
		s.serveISOTree(w, r, iso)
	case strings.HasPrefix(action, "file/"):
		filePath := strings.TrimPrefix(action, "file/")
		s.serveISOInternalFile(w, r, iso, filePath)
	case action == "wimboot":
		s.handleWimboot(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) serveRawISO(w http.ResponseWriter, r *http.Request, iso *isomgr.ISOInfo) {
	f, err := os.Open(iso.Path)
	if err != nil {
		s.log.Error("HTTP", "Cannot open ISO %s: %v", iso.Name, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	stat, _ := f.Stat()
	http.ServeContent(w, r, iso.Name, stat.ModTime(), f)
}

func (s *Server) serveISOInternalFile(w http.ResponseWriter, r *http.Request, iso *isomgr.ISOInfo, filePath string) {
	s.log.Info("HTTP", "Extracting from ISO %s: /%s", iso.Name, filePath)

	// ---- Attempt 0: Serve modified boot.wim from cache (Windows ISOs) ----
	if s.tryServeModifiedBootWim(w, r, iso, filePath) {
		return
	}

	// ---- Attempt 1: ISO9660 in-memory extraction ----
	if s.tryServeFromISO9660(w, r, iso, filePath) {
		return
	}

	// ---- Attempt 2: Windows mount fallback (handles UDF, Win11, etc.) ----
	if s.tryServeFromWindowsMount(w, r, iso, filePath) {
		return
	}

	s.log.Warn("HTTP", "File not found in ISO %s: %s (all methods failed)", iso.Name, filePath)
	http.NotFound(w, r)
}

// tryServeModifiedBootWim serves the cached modified boot.wim (with injected
// startnet.cmd) instead of the original from the ISO. Only applies to
// Windows ISOs and only for boot.wim requests.
func (s *Server) tryServeModifiedBootWim(w http.ResponseWriter, r *http.Request, iso *isomgr.ISOInfo, filePath string) bool {
	if iso.OSType != isomgr.OSTypeWindows && iso.OSType != isomgr.OSTypeWinPE {
		return false
	}

	normalized := strings.ToLower(filePath)
	if !strings.HasSuffix(normalized, "boot.wim") {
		return false
	}

	cached, ok := s.bootWimCache.Load(iso.Path)
	if !ok {
		return false
	}

	cachedPath := cached.(string)
	if _, err := os.Stat(cachedPath); err != nil {
		s.bootWimCache.Delete(iso.Path)
		return false
	}

	s.log.Info("HTTP", "Serving MODIFIED boot.wim for %s (with startnet.cmd injected)", iso.Name)
	http.ServeFile(w, r, cachedPath)
	return true
}

// tryServeFromISO9660 attempts to read a file from the ISO using the ISO9660 library.
func (s *Server) tryServeFromISO9660(w http.ResponseWriter, r *http.Request, iso *isomgr.ISOInfo, filePath string) bool {
	f, err := os.Open(iso.Path)
	if err != nil {
		return false
	}
	defer f.Close()

	isoImg, err := isoOpen(f)
	if err != nil {
		return false
	}

	resolvedPath := filePath
	var reader interface{ Read([]byte) (int, error); Seek(int64, int) (int64, error) }
	var size int64
	if iso.OSType == isomgr.OSTypeWindows || iso.OSType == isomgr.OSTypeWinPE {
		reader, size, resolvedPath, err = isoGetWindowsBootFileReader(isoImg, filePath)
	} else {
		reader, size, err = isoGetFileReader(isoImg, resolvedPath)
	}
	if err != nil {
		s.log.Info("HTTP", "ISO9660 lookup failed for %s/%s: %v", iso.Name, filePath, err)
		return false
	}

	s.log.Info("HTTP", "Serving %s from ISO %s via ISO9660 (%d bytes)", resolvedPath, iso.Name, size)
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, resolvedPath, time.Now(), reader)
	return true
}

// tryServeFromWindowsMount mounts the ISO using Windows' built-in support and
// serves the file directly from the mounted drive. This handles UDF, hybrid,
// and any format Windows can read. Files are streamed (no memory loading).
func (s *Server) tryServeFromWindowsMount(w http.ResponseWriter, r *http.Request, iso *isomgr.ISOInfo, filePath string) bool {
	driveLetter, err := s.mountISO(iso.Path)
	if err != nil {
		s.log.Warn("HTTP", "Windows mount failed for %s: %v", iso.Name, err)
		return false
	}

	// Try the exact path first
	fullPath := filepath.Join(driveLetter+":\\", filepath.FromSlash(filePath))
	if _, err := os.Stat(fullPath); err == nil {
		s.log.Info("HTTP", "Serving %s from ISO %s via Windows mount", filePath, iso.Name)
		http.ServeFile(w, r, fullPath)
		return true
	}

	// For Windows boot files, try common alternate paths
	normalized := strings.ToLower(filePath)
	var alternates []string
	switch {
	case strings.HasSuffix(normalized, "bcd"):
		alternates = []string{"boot\\bcd", "Boot\\BCD", "efi\\microsoft\\boot\\bcd", "EFI\\Microsoft\\Boot\\BCD"}
	case strings.HasSuffix(normalized, "boot.sdi"):
		alternates = []string{"boot\\boot.sdi", "Boot\\boot.sdi"}
	case strings.HasSuffix(normalized, "boot.wim"):
		alternates = []string{"sources\\boot.wim", "Sources\\boot.wim"}
	}

	for _, alt := range alternates {
		p := filepath.Join(driveLetter+":\\", alt)
		if _, err := os.Stat(p); err == nil {
			s.log.Info("HTTP", "Serving %s (resolved to %s) from ISO %s via Windows mount", filePath, alt, iso.Name)
			http.ServeFile(w, r, p)
			return true
		}
	}

	s.log.Info("HTTP", "File %s not found on mounted drive %s:\\", filePath, driveLetter)
	return false
}

func (s *Server) handleWimboot(w http.ResponseWriter, r *http.Request) {
	clientIP := strings.Split(r.RemoteAddr, ":")[0]
	s.log.Info("HTTP", ">>> wimboot requested by %s (%d bytes)", clientIP, len(wimbootBin))

	if len(wimbootBin) < 100 {
		s.log.Error("HTTP", "wimboot binary is a placeholder — download the real one")
		http.Error(w, "wimboot not available — replace assets/wimboot with real binary", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, "wimboot", time.Now(), bytes.NewReader(wimbootBin))
}

func (s *Server) serveISOTree(w http.ResponseWriter, r *http.Request, iso *isomgr.ISOInfo) {
	f, err := os.Open(iso.Path)
	if err != nil {
		http.Error(w, "Cannot open ISO", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	img, err := isoOpen(f)
	if err != nil {
		http.Error(w, "Cannot parse ISO: "+err.Error(), http.StatusInternalServerError)
		return
	}

	tree := isoListTree(img)
	s.log.Info("HTTP", "ISO tree for %s: %d entries", iso.Name, len(tree))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"iso":   iso.Name,
		"count": len(tree),
		"files": tree,
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) SetServerIP(ip string) {
	s.serverIP = ip
}

func sanitizeMenuID(name string) string {
	name = strings.TrimSuffix(strings.ToLower(name), ".iso")
	replacer := strings.NewReplacer(" ", "-", ".", "-", "_", "-", "(", "", ")", "")
	return replacer.Replace(name)
}

