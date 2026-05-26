package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"iboottime/screen_agent/capture"
	agentinput "iboottime/screen_agent/input"
	"iboottime/screen_agent/session"

	"github.com/gorilla/websocket"
)

const (
	defaultFPS     = 20
	defaultQuality = 92
	installPath    = `C:\IBootTime\screen_agent.exe`
	setupComplete  = `C:\Windows\Setup\Scripts\SetupComplete.cmd`
	runOnceKey     = `HKLM\Software\Microsoft\Windows\CurrentVersion\RunOnce`
	runKey         = `HKLM\Software\Microsoft\Windows\CurrentVersion\Run`
	startupCMD     = `C:\ProgramData\Microsoft\Windows\Start Menu\Programs\Startup\IBootTimeScreenAgent.cmd`
)

func main() {
	server := flag.String("server", "", "ws://server:port/ws/remote")
	fps := flag.Int("fps", defaultFPS, "capture frames per second")
	quality := flag.Int("quality", defaultQuality, "JPEG quality 35-95")
	interactive := flag.Bool("interactive", false, "run capture and input in the current interactive session")
	service := flag.Bool("service", false, "run as Windows service (auto-start at boot)")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	setupLogging(*interactive || *service)

	if *server == "" {
		*server = os.Getenv("IBOOTTIME_REMOTE_WS")
	}
	if *server == "" {
		*server = readServerConfig()
	}
	if *server == "" {
		*server = "ws://127.0.0.1:8080/ws/remote"
	}

	if runtime.GOOS == "windows" && *service {
		log.Printf("starting as Windows service (server=%s)", *server)
		if err := runAsService(*server, *fps, *quality); err != nil {
			log.Printf("service dispatcher failed: %v — falling back to interactive", err)
			// Fall through to normal mode (useful when testing from command line)
		} else {
			return
		}
	}

	if runtime.GOOS == "windows" {
		session.LogSessionInfo()
		if err := ensurePersistence(*server, *fps, *quality); err != nil {
			log.Printf("persistence warning: %v", err)
		}
		if !isWinPE() && !session.InActiveConsoleSession() {
			log.Printf("starting supervisor mode; capture/input will be launched in the active console session")
			session.SuperviseInteractive(*server, *fps, *quality, log.Printf)
			return
		}
		if *interactive {
			log.Printf("interactive mode requested; proceeding with capture regardless of session")
		}
	}

	log.Printf("starting interactive capture/input mode")
	runForever(*server, *fps, *quality)
}

// readServerConfig reads the server WebSocket URL from a config file.
// This avoids quoting/escaping issues when embedding URLs in registry or batch files.
func readServerConfig() string {
	paths := []string{
		`C:\IBootTime\server.cfg`,
		`X:\IBootTime\remote\server.cfg`,
	}
	if exe, err := os.Executable(); err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(exe), "server.cfg"))
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		url := strings.TrimSpace(string(data))
		if url != "" {
			log.Printf("server URL from config %s: %s", p, url)
			return url
		}
	}
	return ""
}

func runForever(server string, fps, quality int) {
	backoff := time.Second
	for {
		start := time.Now()
		err := runSession(server, fps, quality)
		sessionDuration := time.Since(start)
		if err != nil {
			log.Printf("session ended after %s: %v; retrying in %s", sessionDuration.Round(time.Millisecond), err, backoff)
		} else {
			log.Printf("session ended cleanly after %s; reconnecting in %s", sessionDuration.Round(time.Millisecond), backoff)
		}
		time.Sleep(backoff)
		// If the session was alive long enough that the connection clearly worked,
		// reset the backoff so the next reconnect happens quickly instead of
		// inheriting a multi-second wait from earlier startup retries.
		if sessionDuration >= 5*time.Second {
			backoff = time.Second
		} else if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func runSession(server string, fps, quality int) error {
	u, err := url.Parse(server)
	if err != nil {
		return err
	}
	log.Printf("dialing remote server %s", u.String())
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second
	conn, resp, err := dialer.Dial(u.String(), nil)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("websocket dial failed: %w (http %s)", err, resp.Status)
		}
		return err
	}
	defer conn.Close()
	log.Printf("connected to %s", u.String())

	cap, err := capture.New(quality)
	if err != nil {
		return err
	}
	input := agentinput.NewController()
	errCh := make(chan error, 2)

	go func() {
		for {
			mt, pkt, err := conn.ReadMessage()
			if err != nil {
				errCh <- err
				return
			}
			if mt != websocket.BinaryMessage {
				continue
			}
			if err := input.HandlePacket(pkt); err != nil {
				log.Printf("input warning: %v", err)
			}
		}
	}()

	go func() {
		// Pin this goroutine to a single OS thread. SetThreadDesktop and GetDC
		// are per-thread; without this, the Go scheduler can migrate the goroutine
		// to a different thread between AttachInputDesktop() and GetDC(0), causing
		// the capture to read from Session 0's blank desktop (black screen).
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		interval := time.Second / time.Duration(max(1, fps))
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			packets, err := cap.CapturePackets()
			if err != nil {
				errCh <- err
				return
			}
			for _, pkt := range packets {
				if err := conn.WriteMessage(websocket.BinaryMessage, pkt); err != nil {
					errCh <- err
					return
				}
			}
		}
	}()

	err = <-errCh
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func ensurePersistence(server string, fps, quality int) error {
	winPE := isWinPE()
	if !winPE {
		if err := ensureRun(server, fps, quality); err != nil {
			log.Printf("Run warning: %v", err)
		}
		if err := ensureStartupCMD(server, fps, quality); err != nil {
			log.Printf("Startup warning: %v", err)
		}
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}

	// In WinPE, scan all drive letters to find the installed Windows partition.
	// C: is not guaranteed — Setup may install to D:, E:, etc.
	deployed := 0
	for letter := 'C'; letter <= 'Z'; letter++ {
		if letter == 'X' {
			continue // X: is WinPE's RAM disk
		}
		root := fmt.Sprintf("%c:\\", letter)
		sysCheck := filepath.Join(root, "Windows", "System32", "config", "SYSTEM")
		if _, err := os.Stat(sysCheck); err != nil {
			continue
		}
		targetInstall := fmt.Sprintf("%c:\\IBootTime\\screen_agent.exe", letter)
		targetSetupComplete := fmt.Sprintf("%c:\\Windows\\Setup\\Scripts\\SetupComplete.cmd", letter)
		log.Printf("persistence: found Windows partition at %c:\\", letter)

		if err := os.MkdirAll(filepath.Dir(targetInstall), 0755); err != nil {
			log.Printf("persistence: mkdir %s: %v", filepath.Dir(targetInstall), err)
			continue
		}
		if !samePath(exe, targetInstall) {
			ensureInstalledCopy(exe, targetInstall)
		}
		if err := writeSetupCompleteAt(targetSetupComplete, server, fps, quality, letter); err != nil {
			log.Printf("persistence: SetupComplete on %c: %v", letter, err)
			continue
		}
		deployed++
	}
	if deployed == 0 {
		// Fallback: write to C: anyway (will be picked up when partition appears)
		log.Printf("persistence: no Windows partition found yet, writing to C:\\ as fallback")
		if err := os.MkdirAll(filepath.Dir(installPath), 0755); err != nil {
			return err
		}
		if !samePath(exe, installPath) {
			ensureInstalledCopy(exe, installPath)
		}
		if err := writeSetupComplete(server, fps, quality); err != nil {
			return err
		}
	}
	return nil
}

func setupLogging(interactive bool) {
	mode := "supervisor"
	if interactive {
		mode = "interactive"
	}
	pid := os.Getpid()
	paths := []string{
		`X:\IBootTime\remote\screen_agent.log`,
		fmt.Sprintf(`X:\IBootTime\remote\screen_agent_%s_%d.log`, mode, pid),
		`C:\IBootTime\screen_agent.log`,
		fmt.Sprintf(`C:\IBootTime\screen_agent_%s_%d.log`, mode, pid),
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		paths = append(paths,
			filepath.Join(dir, "screen_agent.log"),
			filepath.Join(dir, fmt.Sprintf("screen_agent_%s_%d.log", mode, pid)),
		)
	}
	if tmp := os.TempDir(); tmp != "" {
		paths = append(paths,
			filepath.Join(tmp, "screen_agent.log"),
			filepath.Join(tmp, fmt.Sprintf("screen_agent_%s_%d.log", mode, pid)),
		)
	}

	writers := []io.Writer{os.Stdout}
	var opened []string
	var failures []string
	for _, path := range uniqueStrings(paths) {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			failures = append(failures, fmt.Sprintf("%s: mkdir: %v", path, err))
			continue
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: open: %v", path, err))
			continue
		}
		writers = append(writers, f)
		opened = append(opened, path)
	}
	log.SetOutput(io.MultiWriter(writers...))
	log.Printf("logging mode=%s pid=%d paths=%s", mode, pid, strings.Join(opened, "; "))
	if len(opened) == 0 {
		log.Printf("logging warning: no file log could be opened")
	}
	for _, failure := range failures {
		log.Printf("logging path skipped: %s", failure)
	}
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func isWinPE() bool {
	if _, err := os.Stat(`X:\Windows\System32\wpeinit.exe`); err == nil {
		return true
	}
	if strings.EqualFold(os.Getenv("SystemDrive"), "X:") {
		return true
	}
	if _, err := os.Stat(`X:\`); err == nil && strings.Contains(strings.ToLower(os.Getenv("USERNAME")), "system") {
		return true
	}
	return false
}

func ensureInstalledCopy(src, dst string) {
	if _, err := os.Stat(dst); err == nil {
		return
	}
	if err := copyFile(src, dst); err != nil {
		log.Printf("install copy skipped: %v", err)
	}
}

func writeSetupComplete(server string, fps, quality int) error {
	return writeSetupCompleteAt(setupComplete, server, fps, quality, 'C')
}

func writeSetupCompleteAt(path, server string, fps, quality int, driveLetter rune) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	agentPath := fmt.Sprintf(`%c:\IBootTime\screen_agent.exe`, driveLetter)
	// Use -interactive so it works during OOBE (Session 0, no user token)
	line := fmt.Sprintf("start \"IBootTime Remote\" /B \"%s\" -server \"%s\" -fps %d -quality %d -interactive", agentPath, server, fps, quality)
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "screen_agent.exe") {
		log.Printf("persistence: SetupComplete at %s already contains agent entry", path)
		return nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	defer f.Close()
	if len(data) == 0 {
		if _, err := f.WriteString("@echo off\r\n"); err != nil {
			return err
		}
	}
	_, err = f.WriteString("\r\nrem IBootTime screen agent\r\n" + line + "\r\n")
	log.Printf("persistence: wrote SetupComplete entry to %s", path)
	return err
}

func ensureRunOnce(server string, fps, quality int) error {
	cmdLine := fmt.Sprintf(`"%s" -server "%s" -fps %d -quality %d -interactive`, installPath, server, fps, quality)
	return exec.Command("reg", "add", runOnceKey, "/v", "IBootTimeScreenAgent", "/t", "REG_SZ", "/d", cmdLine, "/f").Run()
}

func ensureRun(server string, fps, quality int) error {
	cmdLine := fmt.Sprintf(`"%s" -server "%s" -fps %d -quality %d -interactive`, installPath, server, fps, quality)
	return exec.Command("reg", "add", runKey, "/v", "IBootTimeScreenAgent", "/t", "REG_SZ", "/d", cmdLine, "/f").Run()
}

func ensureStartupCMD(server string, fps, quality int) error {
	if err := os.MkdirAll(filepath.Dir(startupCMD), 0755); err != nil {
		return err
	}
	line := fmt.Sprintf("@echo off\r\nstart \"\" /B \"%s\" -server \"%s\" -fps %d -quality %d -interactive\r\n", installPath, server, fps, quality)
	return os.WriteFile(startupCMD, []byte(line), 0644)
}

func ensureScheduledTask(server string, fps, quality int) error {
	schtasks, err := exec.LookPath("schtasks.exe")
	if err != nil {
		schtasks = `C:\Windows\System32\schtasks.exe`
		if _, statErr := os.Stat(schtasks); statErr != nil {
			return nil
		}
	}
	tr := fmt.Sprintf(`"%s" -server "%s" -fps %d -quality %d`, installPath, server, fps, quality)
	return exec.Command(schtasks, "/Create", "/TN", "IBootTimeScreenAgent", "/TR", tr, "/SC", "ONLOGON", "/RL", "HIGHEST", "/F").Run()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func samePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	return errA == nil && errB == nil && strings.EqualFold(aa, bb)
}
