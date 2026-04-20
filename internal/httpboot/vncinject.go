package httpboot

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// findVNCDir locates the directory containing UltraVNC portable binaries.
// Searches relative to executable and working directory for "remote/winvnc/".
func (s *Server) findVNCDir() string {
	candidates := []string{"remote/winvnc", "remote\\winvnc"}
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		candidates = append([]string{
			filepath.Join(exeDir, "remote", "winvnc"),
			filepath.Join(exeDir, "..", "remote", "winvnc"),
			filepath.Join(exeDir, "..", "..", "remote", "winvnc"),
		}, candidates...)
	}
	for _, dir := range candidates {
		absDir, _ := filepath.Abs(dir)
		if info, err := os.Stat(absDir); err == nil && info.IsDir() {
			// Verify it has at least winvnc.exe
			if _, err := os.Stat(filepath.Join(absDir, "winvnc.exe")); err == nil {
				return absDir
			}
		}
	}
	return ""
}

// findNoVNCDir locates the noVNC-master directory for serving static files.
func (s *Server) findNoVNCDir() string {
	candidates := []string{"noVNC-master"}
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		candidates = append([]string{
			filepath.Join(exeDir, "noVNC-master"),
			filepath.Join(exeDir, "..", "noVNC-master"),
			filepath.Join(exeDir, "..", "..", "noVNC-master"),
		}, candidates...)
	}
	for _, dir := range candidates {
		absDir, _ := filepath.Abs(dir)
		if info, err := os.Stat(absDir); err == nil && info.IsDir() {
			if _, err := os.Stat(filepath.Join(absDir, "core", "rfb.js")); err == nil {
				return absDir
			}
		}
	}
	return ""
}

// injectCurlIntoWIM copies curl.exe into X:\IBootTime\vnc\ inside the WIM.
// This ensures autounattend.xml download works even without VNC enabled.
// If VNC is enabled, injectVNCIntoWIM will overwrite with the full set of files.
func (s *Server) injectCurlIntoWIM(mountDir string) {
	destDir := filepath.Join(mountDir, "IBootTime", "vnc")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		s.log.Warn("HTTP", "Failed to create curl dir in WIM: %v", err)
		return
	}

	// Already has curl.exe (e.g. from a previous VNC injection)
	destCurl := filepath.Join(destDir, "curl.exe")
	if _, err := os.Stat(destCurl); err == nil {
		return
	}

	// Look for curl.exe in the VNC directory
	vncDir := s.findVNCDir()
	if vncDir != "" {
		srcCurl := filepath.Join(vncDir, "curl.exe")
		if _, err := os.Stat(srcCurl); err == nil {
			if err := copyFile(srcCurl, destCurl); err == nil {
				s.log.Info("HTTP", "Injected curl.exe into WIM for autounattend support")
				return
			}
		}
	}

	// Fallback: look for curl.exe next to executable
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		for _, candidate := range []string{
			filepath.Join(exeDir, "curl.exe"),
			filepath.Join(exeDir, "remote", "curl.exe"),
		} {
			if _, err := os.Stat(candidate); err == nil {
				if err := copyFile(candidate, destCurl); err == nil {
					s.log.Info("HTTP", "Injected curl.exe into WIM (from %s)", candidate)
					return
				}
			}
		}
	}

	s.log.Warn("HTTP", "curl.exe not found — autounattend.xml download may not work in WinPE")
}

// injectVNCIntoWIM copies UltraVNC files and a startup script into a mounted WIM.
// This enables VNC remote control during WinPE boot.
// mountDir: the DISM mount directory
// serverIP: IBootTime server IP
// httpPort: IBootTime HTTP port
// vncPort: VNC listen port on client
func (s *Server) injectVNCIntoWIM(mountDir, serverIP string, httpPort, vncPort int) error {
	vncDir := s.findVNCDir()
	if vncDir == "" {
		return fmt.Errorf("UltraVNC directory not found (expected remote/winvnc/winvnc.exe)")
	}

	// Destination inside the WIM
	destDir := filepath.Join(mountDir, "IBootTime", "vnc")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create VNC dir in WIM: %w", err)
	}

	// Copy all files and subdirectories from winvnc directory
	if err := s.copyDirRecursive(vncDir, destDir); err != nil {
		return fmt.Errorf("copy VNC files: %w", err)
	}

	s.log.Info("VNC", "Copied UltraVNC files to WIM (%s)", destDir)

	// Create the VNC startup script (called from startnet.cmd)
	vncScript := buildVNCStartScript(serverIP, httpPort, vncPort)
	scriptPath := filepath.Join(destDir, "start_vnc.cmd")
	if err := os.WriteFile(scriptPath, []byte(vncScript), 0644); err != nil {
		return fmt.Errorf("write VNC script: %w", err)
	}

	s.log.Info("VNC", "VNC startup script written to WIM")
	return nil
}

// copyDirRecursive copies all files and subdirectories from src to dst.
func (s *Server) copyDirRecursive(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("read dir %s: %w", src, err)
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := os.MkdirAll(dstPath, 0755); err != nil {
				s.log.Warn("VNC", "Failed to create dir %s: %v", entry.Name(), err)
				continue
			}
			if err := s.copyDirRecursive(srcPath, dstPath); err != nil {
				s.log.Warn("VNC", "Failed to copy dir %s: %v", entry.Name(), err)
			}
			continue
		}
		if err := copyFile(srcPath, dstPath); err != nil {
			s.log.Warn("VNC", "Failed to copy %s: %v", entry.Name(), err)
		}
	}
	return nil
}

// buildVNCStartScript creates a batch script that:
// 1. Calls the server to get a per-session VNC password + INI
// 2. Writes the UltraVNC INI
// 3. Starts winvnc.exe
// 4. Beacons back to IBootTime with IP/port/password
func buildVNCStartScript(serverIP string, httpPort, vncPort int) string {
	var sb strings.Builder
	sb.WriteString("@echo off\r\n")
	sb.WriteString("echo [IBootTime] ==============================\r\n")
	sb.WriteString("echo [IBootTime] Configurando control remoto VNC...\r\n")
	sb.WriteString("echo [IBootTime] ==============================\r\n")
	sb.WriteString("\r\n")

	// Set server URL and VNC port
	sb.WriteString(fmt.Sprintf("set IBTSERVER=http://%s:%d\r\n", serverIP, httpPort))
	sb.WriteString(fmt.Sprintf("set IBTSERVERIP=%s\r\n", serverIP))
	sb.WriteString(fmt.Sprintf("set VNCPORT=%d\r\n", vncPort))
	sb.WriteString(fmt.Sprintf("set VNCREVPORT=%d\r\n", DefaultReverseVNCPort))
	sb.WriteString("\r\n")

	// Get IP address — WinPE lacks findstr, use 'find' instead
	sb.WriteString(":: Get our IP (WinPE compatible)\r\n")
	sb.WriteString("set MYIP=\r\n")
	sb.WriteString("for /f \"tokens=1-2 delims=:\" %%a in ('ipconfig') do (\r\n")
	sb.WriteString("  echo %%a | find \"IPv4\" >nul 2>&1 && for /f \"tokens=1\" %%c in (\"%%b\") do set MYIP=%%c\r\n")
	sb.WriteString(")\r\n")
	sb.WriteString("if \"%MYIP%\"==\"\" (\r\n")
	sb.WriteString("  echo [IBootTime] VNC: No se pudo obtener IP. Abortando VNC.\r\n")
	sb.WriteString("  goto :vnc_end\r\n")
	sb.WriteString(")\r\n")
	sb.WriteString("echo [IBootTime] VNC: IP del cliente: %MYIP%\r\n")
	sb.WriteString("\r\n")

	// curl.exe se copia dentro del WIM junto con los archivos VNC
	sb.WriteString("set CURL=X:\\IBootTime\\vnc\\curl.exe\r\n")
	sb.WriteString("if not exist \"%CURL%\" (\r\n")
	sb.WriteString("  echo [IBootTime] VNC: curl.exe no encontrado. Abortando.\r\n")
	sb.WriteString("  goto :vnc_end\r\n")
	sb.WriteString(")\r\n")
	sb.WriteString("echo [IBootTime] VNC: curl.exe OK\r\n")
	sb.WriteString("\r\n")

	// Show server URL for diagnostics
	sb.WriteString("echo [IBootTime] VNC: Servidor: %IBTSERVER%\r\n")
	sb.WriteString("\r\n")

	// Verify HTTP connectivity before VNC-specific endpoints
	sb.WriteString(":: Quick HTTP connectivity check\r\n")
	sb.WriteString("echo [IBootTime] VNC: Verificando conectividad HTTP...\r\n")
	sb.WriteString("%CURL% -s -o nul --connect-timeout 5 --max-time 8 \"%IBTSERVER%/health\"\r\n")
	sb.WriteString("if errorlevel 1 (\r\n")
	sb.WriteString("  echo [IBootTime] VNC: No se pudo contactar servidor HTTP. Reintentando...\r\n")
	sb.WriteString("  ping -n 6 127.0.0.1 >nul\r\n")
	sb.WriteString("  %CURL% -s -o nul --connect-timeout 10 --max-time 15 \"%IBTSERVER%/health\"\r\n")
	sb.WriteString("  if errorlevel 1 (\r\n")
	sb.WriteString("    echo [IBootTime] VNC: Servidor HTTP no disponible. Abortando VNC.\r\n")
	sb.WriteString("    goto :vnc_end\r\n")
	sb.WriteString("  )\r\n")
	sb.WriteString(")\r\n")
	sb.WriteString("echo [IBootTime] VNC: Conectividad HTTP OK\r\n")
	sb.WriteString("\r\n")

	// Fetch VNC password from dedicated plaintext endpoint (no JSON parsing needed)
	sb.WriteString(":: Fetch VNC password (plain text)\r\n")
	sb.WriteString("echo [IBootTime] VNC: Obteniendo password del servidor...\r\n")
	sb.WriteString("%CURL% -s -o X:\\IBootTime\\vnc\\_pw.txt --connect-timeout 8 --max-time 15 --retry 2 --retry-delay 2 \"%IBTSERVER%/api/winpe/vnc-password\"\r\n")
	sb.WriteString("if errorlevel 1 (\r\n")
	sb.WriteString("  echo [IBootTime] VNC: Error obteniendo password.\r\n")
	sb.WriteString("  goto :vnc_end\r\n")
	sb.WriteString(")\r\n")
	sb.WriteString("set /p VNCPW=<X:\\IBootTime\\vnc\\_pw.txt\r\n")
	sb.WriteString("if \"%VNCPW%\"==\"\" (\r\n")
	sb.WriteString("  echo [IBootTime] VNC: Password vacio.\r\n")
	sb.WriteString("  goto :vnc_end\r\n")
	sb.WriteString(")\r\n")
	sb.WriteString("echo [IBootTime] VNC: Password OK\r\n")
	sb.WriteString("\r\n")

	// Fetch ultravnc.ini directly (plain text, no JSON)
	sb.WriteString(":: Fetch ultravnc.ini\r\n")
	sb.WriteString("echo [IBootTime] VNC: Obteniendo ultravnc.ini...\r\n")
	sb.WriteString("%CURL% -s -o X:\\IBootTime\\vnc\\ultravnc.ini --connect-timeout 15 --retry 3 --retry-delay 2 \"%IBTSERVER%/api/winpe/vnc-ini\"\r\n")
	sb.WriteString("if errorlevel 1 (\r\n")
	sb.WriteString("  echo [IBootTime] VNC: Error obteniendo ultravnc.ini.\r\n")
	sb.WriteString("  goto :vnc_end\r\n")
	sb.WriteString(")\r\n")
	sb.WriteString("if not exist \"X:\\IBootTime\\vnc\\ultravnc.ini\" (\r\n")
	sb.WriteString("  echo [IBootTime] VNC: ultravnc.ini no creado.\r\n")
	sb.WriteString("  goto :vnc_end\r\n")
	sb.WriteString(")\r\n")
	sb.WriteString("echo [IBootTime] VNC: INI OK\r\n")
	sb.WriteString("\r\n")

	// Open firewall for VNC port (WinPE may block incoming connections at WFP layer
	// even when the MpsSvc service is disabled, so disable all profiles first).
	sb.WriteString("echo [IBootTime] VNC: Desactivando firewall WinPE...\r\n")
	sb.WriteString("netsh advfirewall set allprofiles state off >nul 2>&1\r\n")
	sb.WriteString("netsh firewall set opmode disable >nul 2>&1\r\n")
	// Fallback: explicit allow rule in case MpsSvc gets re-enabled.
	sb.WriteString("netsh advfirewall firewall add rule name=\"IBootTime VNC\" dir=in action=allow protocol=tcp localport=%VNCPORT% profile=any >nul 2>&1\r\n")
	sb.WriteString("netsh firewall add portopening TCP %VNCPORT% \"IBootTime VNC\" >nul 2>&1\r\n")
	sb.WriteString("\r\n")

	// Start WinVNC (cd to dir so it finds ultravnc.ini)
	sb.WriteString("echo [IBootTime] VNC: Iniciando winvnc.exe...\r\n")
	sb.WriteString("cd /d X:\\IBootTime\\vnc\r\n")
	sb.WriteString("start \"\" /B winvnc.exe -run\r\n")
	sb.WriteString("echo [IBootTime] VNC: Esperando inicio (5s)...\r\n")
	sb.WriteString("ping -n 6 127.0.0.1 >nul\r\n")
	sb.WriteString("\r\n")

	// Beacon back to server — report readiness but do NOT connect yet.
	// The server operator must press "Conectar" in the UI which sets a flag;
	// the client polls /api/winpe/vnc-check and only then dials out.
	sb.WriteString(":: Beacon back to IBootTime server\r\n")
	sb.WriteString("echo [IBootTime] VNC: Enviando beacon al servidor...\r\n")
	sb.WriteString("%CURL% -s -X POST -H \"Content-Type: application/json\" --connect-timeout 15 --retry 3 --retry-delay 2 ^\r\n")
	sb.WriteString("  -d \"{\\\"ip\\\":\\\"%MYIP%\\\",\\\"port\\\":%VNCPORT%,\\\"password\\\":\\\"%VNCPW%\\\"}\" ^\r\n")
	sb.WriteString("  \"%IBTSERVER%/api/winpe/remote-ready\"\r\n")
	sb.WriteString("if errorlevel 1 (\r\n")
	sb.WriteString("  echo [IBootTime] VNC: Beacon FALLO.\r\n")
	sb.WriteString("  goto :vnc_end\r\n")
	sb.WriteString(")\r\n")
	sb.WriteString("echo [IBootTime] VNC: Beacon enviado OK.\r\n")
	sb.WriteString("\r\n")

	// Poll loop: wait for the server to tell us to connect
	sb.WriteString(fmt.Sprintf("echo [IBootTime] VNC: Listo en %%MYIP%%:%d — esperando orden del servidor...\r\n", vncPort))
	sb.WriteString("echo [IBootTime] ==============================\r\n")
	sb.WriteString("\r\n")
	sb.WriteString(":: Poll /api/winpe/vnc-check every 5 seconds until server says connect\r\n")
	sb.WriteString(":vnc_poll\r\n")
	sb.WriteString("%CURL% -s -o X:\\IBootTime\\vnc\\_check.txt --connect-timeout 10 \"%IBTSERVER%/api/winpe/vnc-check?ip=%MYIP%\"\r\n")
	sb.WriteString("if errorlevel 1 (\r\n")
	sb.WriteString("  echo [IBootTime] VNC: Error contactando servidor, reintentando...\r\n")
	sb.WriteString("  ping -n 6 127.0.0.1 >nul\r\n")
	sb.WriteString("  goto :vnc_poll\r\n")
	sb.WriteString(")\r\n")
	sb.WriteString("set VNCCHECK=\r\n")
	sb.WriteString("set /p VNCCHECK=<X:\\IBootTime\\vnc\\_check.txt\r\n")
	// The response is JSON {"connect":true} — in WinPE we just check for the
	// string "true" inside the response.
	sb.WriteString("echo %VNCCHECK% | find \"true\" >nul 2>&1\r\n")
	sb.WriteString("if errorlevel 1 (\r\n")
	sb.WriteString("  ping -n 6 127.0.0.1 >nul\r\n")
	sb.WriteString("  goto :vnc_poll\r\n")
	sb.WriteString(")\r\n")
	sb.WriteString("\r\n")

	// Server said "connect" — now dial out via reverse VNC
	sb.WriteString(":: Server triggered connect — dial out via reverse VNC\r\n")
	sb.WriteString("echo [IBootTime] VNC: Servidor solicito conexion! Conectando...\r\n")
	sb.WriteString("echo [IBootTime] VNC: Iniciando conexion reversa a %IBTSERVERIP%:%VNCREVPORT%...\r\n")
	sb.WriteString("start \"\" /B winvnc.exe -connect %IBTSERVERIP%:%VNCREVPORT%\r\n")
	sb.WriteString("echo [IBootTime] VNC: Conexion reversa iniciada.\r\n")
	sb.WriteString("\r\n")
	// After connecting, go back to polling so the operator can reconnect if needed
	sb.WriteString(":: Wait and return to polling for future reconnections\r\n")
	sb.WriteString("ping -n 3 127.0.0.1 >nul\r\n")
	sb.WriteString("goto :vnc_poll\r\n")
	sb.WriteString("\r\n")
	sb.WriteString(":vnc_end\r\n")

	return sb.String()
}
