package httpboot

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// injectScreenAgentIntoWIM copies the native Go screen agent into WinPE and
// writes launch/bootstrap scripts so remote control works before and after
// Windows Setup formats the target disk.
func (s *Server) injectScreenAgentIntoWIM(mountDir, serverIP string, httpPort int) error {
	agentExe, err := s.findOrBuildScreenAgent()
	if err != nil {
		return err
	}

	destDir := filepath.Join(mountDir, "IBootTime", "remote")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create native remote dir in WIM: %w", err)
	}

	destExe := filepath.Join(destDir, "screen_agent.exe")
	if err := copyFile(agentExe, destExe); err != nil {
		return fmt.Errorf("copy native screen agent: %w", err)
	}

	wsURL := fmt.Sprintf("ws://%s:%d/ws/remote", serverIP, httpPort)

	// Server config file — the agent reads this to discover the server URL.
	// Avoids quoting/escaping issues in registry ImagePath and batch files.
	if err := os.WriteFile(filepath.Join(destDir, "server.cfg"), []byte(wsURL), 0644); err != nil {
		return fmt.Errorf("write server.cfg: %w", err)
	}

	// WinPE start script (runs from X:\IBootTime\remote\)
	startScript := s.buildScreenAgentStartCMD(wsURL)
	if err := os.WriteFile(filepath.Join(destDir, "start_screen_agent.cmd"), []byte(startScript), 0644); err != nil {
		return fmt.Errorf("write native remote start script: %w", err)
	}

	// Post-install launcher — real file in WIM, watcher will copy /Y to target.
	// This avoids the echo/escaping corruption that broke auto-start.
	launcher := s.buildScreenAgentPostInstallLauncherCMD(wsURL)
	if err := os.WriteFile(filepath.Join(destDir, "start_screen_agent_post.cmd"), []byte(launcher), 0644); err != nil {
		return fmt.Errorf("write post-install launcher: %w", err)
	}

	// SetupComplete.cmd — real file in WIM, watcher will copy /Y to target.
	setupComplete := s.buildScreenAgentSetupCompleteCMD(wsURL)
	if err := os.WriteFile(filepath.Join(destDir, "SetupComplete.cmd"), []byte(setupComplete), 0644); err != nil {
		return fmt.Errorf("write SetupComplete.cmd: %w", err)
	}

	// NOTE: unattend.xml REMOVED — it caused error 0x80FE0000 during specialize
	// ("Windows cannot set offline regional configuration"). The service auto-starts
	// via SCM because Start=2 is set in the offline SYSTEM hive by the watcher.
	// SetupComplete.cmd and GP startup scripts provide additional redundancy.

	// GP startup script — non-blocking launcher for Group Policy machine startup.
	// Backup mechanism: GP engine runs it at boot as SYSTEM before logon.
	gpScript := fmt.Sprintf("@echo off\r\nstart \"\" /B \"C:\\IBootTime\\screen_agent.exe\" -server \"%s\" -fps 20 -quality 92 -interactive\r\nexit /b 0\r\n", wsURL)
	if err := os.WriteFile(filepath.Join(destDir, "gp_startup.cmd"), []byte(gpScript), 0644); err != nil {
		return fmt.Errorf("write GP startup script: %w", err)
	}

	// GP scripts.ini — tells the GP engine which startup scripts to run
	scriptsINI := "[Startup]\r\n0CmdLine=IBootTimeAgent.cmd\r\n0Parameters=\r\n"
	if err := os.WriteFile(filepath.Join(destDir, "scripts.ini"), []byte(scriptsINI), 0644); err != nil {
		return fmt.Errorf("write scripts.ini: %w", err)
	}

	// GP gpt.ini — enables the Scripts client-side extension for local GP
	gptINI := "[General]\r\ngPCFunctionality=0\r\ngPCMachineExtensionNames=[{42B5FAAE-6536-11D2-AE5A-0000F87571E3}{40B6664F-4972-11D1-A7CA-0000F87571E3}]\r\nVersion=1\r\n"
	if err := os.WriteFile(filepath.Join(destDir, "gpt.ini"), []byte(gptINI), 0644); err != nil {
		return fmt.Errorf("write gpt.ini: %w", err)
	}

	// Watcher script (runs in WinPE background)
	watcher := s.buildScreenAgentPostInstallWatcherCMD(wsURL)
	if err := os.WriteFile(filepath.Join(destDir, "install_post_screen_agent.cmd"), []byte(watcher), 0644); err != nil {
		return fmt.Errorf("write native remote post-install watcher: %w", err)
	}

	s.log.Info("Remote", "Native screen agent injected into WIM (%s -> %s)", agentExe, destExe)
	return nil
}

func (s *Server) findOrBuildScreenAgent() (string, error) {
	exe, _ := os.Executable()
	exeDir := filepath.Dir(exe)
	wd, _ := os.Getwd()

	candidates := []string{
		filepath.Join(exeDir, "remote", "screen_agent", "screen_agent.exe"),
		filepath.Join(exeDir, "..", "..", "remote", "screen_agent", "screen_agent.exe"),
		filepath.Join(wd, "remote", "screen_agent", "screen_agent.exe"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}

	sourceDirs := []string{
		filepath.Join(exeDir, "remote", "screen_agent"),
		filepath.Join(exeDir, "..", "..", "remote", "screen_agent"),
		filepath.Join(wd, "remote", "screen_agent"),
	}
	for _, dir := range sourceDirs {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
			continue
		}
		out := filepath.Join(dir, "screen_agent.exe")
		cmd := exec.Command("go", "build", "-o", out, ".")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0")
		if runtime.GOOS == "windows" {
			cmd.Env = append(cmd.Env, "GOCACHE="+filepath.Join(dir, ".gocache"))
		}
		if data, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("build native screen agent: %w: %s", err, strings.TrimSpace(string(data)))
		}
		return out, nil
	}

	return "", fmt.Errorf("remote/screen_agent not found; cannot inject native remote control")
}

func (s *Server) buildScreenAgentStartCMD(wsURL string) string {
	return "@echo off\r\n" +
		"setlocal EnableExtensions\r\n" +
		"echo [IBootTime] Forzando agente remoto nativo...\r\n" +
		"set AGENT=X:\\IBootTime\\remote\\screen_agent.exe\r\n" +
		"set LOG=X:\\IBootTime\\remote\\start_screen_agent.log\r\n" +
		"echo [%date% %time%] Force launcher iniciado >> \"%LOG%\"\r\n" +
		":agent_loop\r\n" +
		"if not exist \"%AGENT%\" (\r\n" +
		"  echo [IBootTime] screen_agent.exe no encontrado: %AGENT%\r\n" +
		"  echo [%date% %time%] FALTA %AGENT% >> \"%LOG%\"\r\n" +
		"  ping -n 4 127.0.0.1 >nul\r\n" +
		"  goto agent_loop\r\n" +
		")\r\n" +
		"tasklist /FI \"IMAGENAME eq screen_agent.exe\" 2>nul | find /I \"screen_agent.exe\" >nul\r\n" +
		"if errorlevel 1 (\r\n" +
		fmt.Sprintf("  echo [IBootTime] Ejecutando: %%AGENT%% -server \"%s\" -fps 20 -quality 92 -interactive\r\n", wsURL) +
		fmt.Sprintf("  echo [%%date%% %%time%%] START %%AGENT%% -server \"%s\" >> \"%%LOG%%\"\r\n", wsURL) +
		fmt.Sprintf("  start \"IBootTime Remote\" /B \"%%AGENT%%\" -server \"%s\" -fps 20 -quality 92 -interactive >> \"%%LOG%%\" 2>&1\r\n", wsURL) +
		") else (\r\n" +
		"  echo [%date% %time%] screen_agent.exe ya esta corriendo >> \"%LOG%\"\r\n" +
		")\r\n" +
		"ping -n 9 127.0.0.1 >nul\r\n" +
		"goto agent_loop\r\n"
}

func (s *Server) buildScreenAgentPostInstallWatcherCMD(wsURL string) string {
	// The watcher runs continuously in WinPE. It scans every 3 seconds for a
	// Windows partition and deploys files using copy /Y from pre-written files
	// in the WIM. This replaces the old echo-based writeCmdFile approach which
	// corrupted the .cmd files due to quoting/escaping issues with the ws:// URL.
	return "@echo off\r\n" +
		"setlocal EnableExtensions\r\n" +
		"echo [IBootTime] Native Remote Watcher: Iniciado\r\n" +
		":scan\r\n" +
		"for %%D in (C D E F G H I J K L M N O P Q R S T U V W Y Z) do (\r\n" +
		"  if /I not \"%%D\"==\"X\" (\r\n" +
		"    if exist \"%%D:\\Windows\\System32\\config\\SYSTEM\" (\r\n" +
		"      if not exist \"%%D:\\IBootTime\\screen_agent.exe\" call :deploy_files %%D\r\n" +
		"      if not exist \"%%D:\\IBootTime\\.svc_ok\" call :register_svc %%D\r\n" +
		"      if not exist \"%%D:\\IBootTime\\.reg_ok\" call :inject_reg %%D\r\n" +
		"    )\r\n" +
		"  )\r\n" +
		")\r\n" +
		"ping -n 4 127.0.0.1 >nul\r\n" +
		"goto scan\r\n" +
		"\r\n" +
		"rem === STEP 1: Copy agent files (one-shot) ===\r\n" +
		":deploy_files\r\n" +
		"set TARGET=%1\r\n" +
		"echo [IBootTime] Watcher: Copiando archivos a %TARGET%:\r\n" +
		"mkdir \"%TARGET%:\\IBootTime\" >nul 2>&1\r\n" +
		"mkdir \"%TARGET%:\\Windows\\Setup\\Scripts\" >nul 2>&1\r\n" +
		"mkdir \"%TARGET%:\\ProgramData\\Microsoft\\Windows\\Start Menu\\Programs\\Startup\" >nul 2>&1\r\n" +
		"copy /Y \"X:\\IBootTime\\remote\\screen_agent.exe\"           \"%TARGET%:\\IBootTime\\screen_agent.exe\" >nul 2>&1\r\n" +
		"copy /Y \"X:\\IBootTime\\remote\\server.cfg\"                 \"%TARGET%:\\IBootTime\\server.cfg\" >nul 2>&1\r\n" +
		"copy /Y \"X:\\IBootTime\\remote\\start_screen_agent_post.cmd\" \"%TARGET%:\\IBootTime\\start_screen_agent.cmd\" >nul 2>&1\r\n" +
		"copy /Y \"X:\\IBootTime\\remote\\SetupComplete.cmd\"          \"%TARGET%:\\Windows\\Setup\\Scripts\\SetupComplete.cmd\" >nul 2>&1\r\n" +
		"copy /Y \"%TARGET%:\\IBootTime\\start_screen_agent.cmd\"      \"%TARGET%:\\ProgramData\\Microsoft\\Windows\\Start Menu\\Programs\\Startup\\IBootTimeScreenAgent.cmd\" >nul 2>&1\r\n" +
		"rem --- GP startup script (backup) ---\r\n" +
		"mkdir \"%TARGET%:\\Windows\\System32\\GroupPolicy\\Machine\\Scripts\\Startup\" >nul 2>&1\r\n" +
		"copy /Y \"X:\\IBootTime\\remote\\gp_startup.cmd\"  \"%TARGET%:\\Windows\\System32\\GroupPolicy\\Machine\\Scripts\\Startup\\IBootTimeAgent.cmd\" >nul 2>&1\r\n" +
		"copy /Y \"X:\\IBootTime\\remote\\scripts.ini\"    \"%TARGET%:\\Windows\\System32\\GroupPolicy\\Machine\\Scripts\\scripts.ini\" >nul 2>&1\r\n" +
		"copy /Y \"X:\\IBootTime\\remote\\gpt.ini\"        \"%TARGET%:\\Windows\\System32\\GroupPolicy\\gpt.ini\" >nul 2>&1\r\n" +
		"if exist \"%TARGET%:\\IBootTime\\screen_agent.exe\" (\r\n" +
		"  echo [IBootTime] Watcher: Archivos desplegados OK en %TARGET%:\r\n" +
		") else (\r\n" +
		"  echo [IBootTime] Watcher: ERROR copiando archivos a %TARGET%:\r\n" +
		")\r\n" +
		"goto :eof\r\n" +
		"\r\n" +
		"rem === STEP 2: Register Windows service in offline SYSTEM hive (retried until success) ===\r\n" +
		"rem We register in BOTH ControlSet001 and ControlSet002 because Windows may\r\n" +
		"rem promote ControlSet002 as Current after the first successful boot, and the\r\n" +
		"rem service must be present in whichever ControlSet ends up active after the\r\n" +
		"rem Microsoft account / OOBE reboot. We also include the -server URL in the\r\n" +
		"rem ImagePath so the agent does not depend on server.cfg being readable.\r\n" +
		":register_svc\r\n" +
		"set TARGET=%1\r\n" +
		"reg load HKLM\\IB_SYSTEM \"%TARGET%:\\Windows\\System32\\config\\SYSTEM\" >nul 2>&1\r\n" +
		"if errorlevel 1 (\r\n" +
		"  echo [IBootTime] Watcher: SYSTEM hive locked on %TARGET%: - will retry\r\n" +
		"  goto :eof\r\n" +
		")\r\n" +
		"call :write_svc_keys ControlSet001\r\n" +
		"call :write_svc_keys ControlSet002\r\n" +
		"reg unload HKLM\\IB_SYSTEM >nul 2>&1\r\n" +
		"echo OK > \"%TARGET%:\\IBootTime\\.svc_ok\"\r\n" +
		"echo [IBootTime] Watcher: Windows service registered on %TARGET%: (ControlSet001+002)\r\n" +
		"goto :eof\r\n" +
		"\r\n" +
		":write_svc_keys\r\n" +
		"set CS=%1\r\n" +
		"reg add \"HKLM\\IB_SYSTEM\\%CS%\\Services\\IBootTimeAgent\" /v Type /t REG_DWORD /d 16 /f >nul 2>&1\r\n" +
		"reg add \"HKLM\\IB_SYSTEM\\%CS%\\Services\\IBootTimeAgent\" /v Start /t REG_DWORD /d 2 /f >nul 2>&1\r\n" +
		"reg add \"HKLM\\IB_SYSTEM\\%CS%\\Services\\IBootTimeAgent\" /v ErrorControl /t REG_DWORD /d 0 /f >nul 2>&1\r\n" +
		"reg add \"HKLM\\IB_SYSTEM\\%CS%\\Services\\IBootTimeAgent\" /v ObjectName /t REG_SZ /d LocalSystem /f >nul 2>&1\r\n" +
		"reg add \"HKLM\\IB_SYSTEM\\%CS%\\Services\\IBootTimeAgent\" /v DisplayName /t REG_SZ /d \"IBootTime Screen Agent\" /f >nul 2>&1\r\n" +
		"reg add \"HKLM\\IB_SYSTEM\\%CS%\\Services\\IBootTimeAgent\" /v Description /t REG_SZ /d \"IBootTime remote screen agent\" /f >nul 2>&1\r\n" +
		fmt.Sprintf("reg add \"HKLM\\IB_SYSTEM\\%%CS%%\\Services\\IBootTimeAgent\" /v ImagePath /t REG_EXPAND_SZ /d \"C:\\IBootTime\\screen_agent.exe -server %s -fps 20 -quality 92 -interactive -service\" /f >nul 2>&1\r\n", wsURL) +
		"goto :eof\r\n" +
		"\r\n" +
		"rem === STEP 3: Inject Run keys into offline SOFTWARE hive (retried until success) ===\r\n" +
		":inject_reg\r\n" +
		"set TARGET=%1\r\n" +
		"reg load HKLM\\IB_OFFLINE \"%TARGET%:\\Windows\\System32\\config\\SOFTWARE\" >nul 2>&1\r\n" +
		"if errorlevel 1 (\r\n" +
		"  echo [IBootTime] Watcher: SOFTWARE hive locked on %TARGET%: - will retry\r\n" +
		"  goto :eof\r\n" +
		")\r\n" +
		"reg add \"HKLM\\IB_OFFLINE\\Microsoft\\Windows\\CurrentVersion\\Run\" /v IBootTimeScreenAgent /t REG_SZ /d \"C:\\IBootTime\\start_screen_agent.cmd\" /f >nul 2>&1\r\n" +
		"reg add \"HKLM\\IB_OFFLINE\\Microsoft\\Windows\\CurrentVersion\\RunOnce\" /v IBootTimeAgentOnce /t REG_SZ /d \"C:\\IBootTime\\start_screen_agent.cmd\" /f >nul 2>&1\r\n" +
		"reg unload HKLM\\IB_OFFLINE >nul 2>&1\r\n" +
		"echo OK > \"%TARGET%:\\IBootTime\\.reg_ok\"\r\n" +
		"echo [IBootTime] Watcher: Run keys injected on %TARGET%:\r\n" +
		"goto :eof\r\n"
}

func (s *Server) buildScreenAgentPostInstallLauncherCMD(wsURL string) string {
	// This is the launcher that runs on the installed Windows.
	// It uses -interactive so it works during OOBE (no user token needed).
	return "@echo off\r\n" +
		"set AGENT=C:\\IBootTime\\screen_agent.exe\r\n" +
		"set LOG=C:\\IBootTime\\screen_agent_launcher.log\r\n" +
		"echo [%date% %time%] IBootTime force remote launcher >> \"%LOG%\"\r\n" +
		":agent_loop\r\n" +
		"if not exist \"%AGENT%\" (\r\n" +
		"  echo [%date% %time%] FALTA %AGENT% >> \"%LOG%\"\r\n" +
		"  ping -n 9 127.0.0.1 >nul\r\n" +
		"  goto agent_loop\r\n" +
		")\r\n" +
		"tasklist /FI \"IMAGENAME eq screen_agent.exe\" 2>nul | find /I \"screen_agent.exe\" >nul\r\n" +
		"if errorlevel 1 (\r\n" +
		fmt.Sprintf("  echo [%%date%% %%time%%] START %%AGENT%% -server \"%s\" >> \"%%LOG%%\"\r\n", wsURL) +
		fmt.Sprintf("  start \"IBootTime Remote\" /B \"%%AGENT%%\" -server \"%s\" -fps 20 -quality 92 -interactive >> \"%%LOG%%\" 2>&1\r\n", wsURL) +
		") else (\r\n" +
		"  echo [%date% %time%] screen_agent.exe ya esta corriendo >> \"%LOG%\"\r\n" +
		")\r\n" +
		"ping -n 16 127.0.0.1 >nul\r\n" +
		"goto agent_loop\r\n"
}

func (s *Server) buildScreenAgentSetupCompleteCMD(wsURL string) string {
	// SetupComplete.cmd runs as SYSTEM after OOBE completes.
	// It creates scheduled tasks for persistence across reboots, sets
	// registry Run keys, and immediately launches the agent via the
	// launcher .cmd (which uses -interactive).
	//
	// IMPORTANT: reg /d values use the .cmd path (no nested quotes!).
	// The old approach embedded the full exe+args with ws:// URL in the
	// reg value, which broke due to nested double-quotes.
	return "@echo off\r\n" +
		"set LOG=C:\\IBootTime\\setupcomplete_remote.log\r\n" +
		"echo [%date% %time%] SetupComplete remote bootstrap >> \"%LOG%\"\r\n" +
		"\r\n" +
		"rem --- Registry Run keys (use .cmd path, no quoting issues) ---\r\n" +
		"reg add \"HKLM\\Software\\Microsoft\\Windows\\CurrentVersion\\Run\" /v IBootTimeScreenAgent /t REG_SZ /d \"C:\\IBootTime\\start_screen_agent.cmd\" /f >> \"%LOG%\" 2>&1\r\n" +
		"reg add \"HKLM\\Software\\Microsoft\\Windows\\CurrentVersion\\RunOnce\" /v IBootTimeScreenAgentOnce /t REG_SZ /d \"C:\\IBootTime\\start_screen_agent.cmd\" /f >> \"%LOG%\" 2>&1\r\n" +
		"\r\n" +
		"rem --- Register Windows service (reliable on live system) ---\r\n" +
		"sc query IBootTimeAgent >nul 2>&1\r\n" +
		"if errorlevel 1 (\r\n" +
		"  sc create IBootTimeAgent binPath= \"C:\\IBootTime\\screen_agent.exe -server " + wsURL + " -fps 20 -quality 92 -interactive -service\" start= auto obj= LocalSystem DisplayName= \"IBootTime Screen Agent\" >> \"%LOG%\" 2>&1\r\n" +
		"  sc description IBootTimeAgent \"IBootTime remote screen agent\" >> \"%LOG%\" 2>&1\r\n" +
		"  echo [%date% %time%] Service created >> \"%LOG%\"\r\n" +
		")\r\n" +
		"sc start IBootTimeAgent >> \"%LOG%\" 2>&1\r\n" +
		"\r\n" +
		"rem --- Scheduled tasks for SYSTEM-level auto-start ---\r\n" +
		"schtasks /Create /TN \"IBootTime Screen Agent Startup\" /TR \"C:\\IBootTime\\start_screen_agent.cmd\" /SC ONSTART /RU SYSTEM /RL HIGHEST /F >> \"%LOG%\" 2>&1\r\n" +
		"schtasks /Create /TN \"IBootTime Screen Agent Logon\" /TR \"C:\\IBootTime\\start_screen_agent.cmd\" /SC ONLOGON /IT /F >> \"%LOG%\" 2>&1\r\n" +
		"\r\n" +
		"rem --- Launch agent now (backup in case service didn't start yet) ---\r\n" +
		"echo [%date% %time%] Launching agent... >> \"%LOG%\"\r\n" +
		"start \"\" /B \"C:\\IBootTime\\start_screen_agent.cmd\"\r\n" +
		"echo [%date% %time%] SetupComplete done >> \"%LOG%\"\r\n" +
		"exit /b 0\r\n"
}

// writeCmdFile and escapeCmdEcho removed — the watcher now uses copy /Y
// of pre-written .cmd files from the WIM instead of echo-based generation.
// This fixes the quoting/escaping corruption that broke auto-start.

// injectAgentIntoWIM copies the Python embedded runtime and agent_client into the WIM.
// After Windows installs, SetupComplete.cmd will start the agent automatically.
func (s *Server) injectAgentIntoWIM(mountDir, serverIP string, httpPort int) error {
	pythonDir := s.findPythonEmbedDir()
	if pythonDir == "" {
		s.log.Warn("Agent", "Python embedded not found (run scripts/download-python-embed.ps1 first)")
		return fmt.Errorf("tools/python-embed not found — run scripts/download-python-embed.ps1")
	}

	agentSrcDir := s.findAgentClientDir()
	if agentSrcDir == "" {
		return fmt.Errorf("agent_client directory not found")
	}

	// Copy Python embedded into WIM
	destPython := filepath.Join(mountDir, "IBootTime", "python")
	if err := os.MkdirAll(destPython, 0755); err != nil {
		return fmt.Errorf("create python dir in WIM: %w", err)
	}
	if err := s.copyDirRecursive(pythonDir, destPython); err != nil {
		return fmt.Errorf("copy python embedded: %w", err)
	}
	s.log.Info("Agent", "Python embedded copied to WIM (%s)", destPython)

	// Copy agent_client into WIM
	destAgent := filepath.Join(mountDir, "IBootTime", "agent")
	if err := os.MkdirAll(destAgent, 0755); err != nil {
		return fmt.Errorf("create agent dir in WIM: %w", err)
	}
	if err := s.copyDirRecursive(agentSrcDir, destAgent); err != nil {
		return fmt.Errorf("copy agent_client: %w", err)
	}
	s.log.Info("Agent", "Agent client copied to WIM (%s)", destAgent)

	// Write the agent post-install PowerShell script
	// Agent server always runs on port 9090 (separate from HTTP boot port)
	const agentServerPort = 9090
	agentScript := s.buildAgentPostInstallScript(serverIP, agentServerPort)
	scriptPath := filepath.Join(mountDir, "IBootTime", "agent", "postinstall_agent.ps1")
	if err := os.WriteFile(scriptPath, []byte(agentScript), 0644); err != nil {
		return fmt.Errorf("write agent post-install script: %w", err)
	}

	s.log.Info("Agent", "Agent injection complete")
	return nil
}

// findPythonEmbedDir locates the Python embedded directory.
func (s *Server) findPythonEmbedDir() string {
	exe, _ := os.Executable()
	exeDir := filepath.Dir(exe)

	candidates := []string{
		filepath.Join(exeDir, "tools", "python-embed"),
		filepath.Join(exeDir, "..", "..", "tools", "python-embed"), // dev mode
	}

	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(c, "python.exe")); err == nil {
			return c
		}
	}
	return ""
}

// findAgentClientDir locates the agent_client source directory.
func (s *Server) findAgentClientDir() string {
	exe, _ := os.Executable()
	exeDir := filepath.Dir(exe)

	candidates := []string{
		filepath.Join(exeDir, "agent_client"),
		filepath.Join(exeDir, "..", "..", "agent_client"), // dev mode
	}

	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(c, "client.py")); err == nil {
			return c
		}
	}
	return ""
}

// buildAgentPostInstallScript generates a PowerShell script that sets up the
// IBootTime agent as a Windows service on the freshly installed system.
func (s *Server) buildAgentPostInstallScript(serverIP string, httpPort int) string {
	serverAddr := fmt.Sprintf("%s:%d", serverIP, httpPort)

	var sb strings.Builder
	sb.WriteString("# IBootTime Agent Post-Install Setup Script\r\n")
	sb.WriteString("# Runs via SetupComplete.cmd after Windows installation.\r\n")
	sb.WriteString("# Configures the agent to start automatically and connect to the IBootTime server.\r\n\r\n")
	sb.WriteString("$ErrorActionPreference = \"SilentlyContinue\"\r\n")
	sb.WriteString("$agentDir     = \"C:\\IBootTime\\agent\"\r\n")
	sb.WriteString("$pythonDir    = \"C:\\IBootTime\\python\"\r\n")
	sb.WriteString("$pythonExe    = \"$pythonDir\\python.exe\"\r\n")
	sb.WriteString("$clientScript = \"$agentDir\\client.py\"\r\n")
	sb.WriteString(fmt.Sprintf("$serverAddr   = \"%s\"\r\n", serverAddr))
	sb.WriteString("$logFile      = \"$agentDir\\agent_setup.log\"\r\n")
	sb.WriteString("$taskName     = \"IBootTime Agent\"\r\n\r\n")

	sb.WriteString("function Log($msg) {\r\n")
	sb.WriteString("    $ts = Get-Date -Format \"yyyy-MM-dd HH:mm:ss\"\r\n")
	sb.WriteString("    $line = \"[$ts] $msg\"\r\n")
	sb.WriteString("    Write-Host $line\r\n")
	sb.WriteString("    try { $line | Out-File -Append -FilePath $logFile -Encoding utf8 } catch {}\r\n")
	sb.WriteString("}\r\n\r\n")

	sb.WriteString("Log \"=== IBootTime Agent Setup START ===\"\r\n")
	sb.WriteString("Log \"Python: $pythonExe\"\r\n")
	sb.WriteString("Log \"Agent:  $clientScript\"\r\n")
	sb.WriteString("Log \"Server: $serverAddr\"\r\n\r\n")

	sb.WriteString("# Verify Python exists\r\n")
	sb.WriteString("if (-not (Test-Path $pythonExe)) {\r\n")
	sb.WriteString("    Log \"FATAL: python.exe no encontrado en $pythonDir\"\r\n")
	sb.WriteString("    exit 1\r\n")
	sb.WriteString("}\r\n\r\n")

	sb.WriteString("# Verify agent script exists\r\n")
	sb.WriteString("if (-not (Test-Path $clientScript)) {\r\n")
	sb.WriteString("    Log \"FATAL: client.py no encontrado en $agentDir\"\r\n")
	sb.WriteString("    exit 1\r\n")
	sb.WriteString("}\r\n\r\n")

	sb.WriteString("# Open firewall for agent (outbound to server)\r\n")
	sb.WriteString("Log \"Configurando firewall...\"\r\n")
	sb.WriteString(fmt.Sprintf("netsh advfirewall firewall add rule name=\"IBootTime Agent\" dir=out action=allow protocol=tcp remoteport=%d profile=any 2>&1 | Out-Null\r\n", httpPort))
	sb.WriteString("Log \"Firewall OK.\"\r\n\r\n")

	sb.WriteString("# Remove existing scheduled task if present\r\n")
	sb.WriteString("Log \"Configurando tarea programada...\"\r\n")
	sb.WriteString("schtasks /delete /tn $taskName /f 2>&1 | Out-Null\r\n\r\n")

	sb.WriteString("# Create a launcher batch file\r\n")
	sb.WriteString("$batFile = \"C:\\IBootTime\\agent\\start_agent.bat\"\r\n")
	sb.WriteString("$batLines = @(\r\n")
	sb.WriteString("    '@echo off',\r\n")
	sb.WriteString("    'cd /d C:\\IBootTime\\agent',\r\n")
	sb.WriteString("    ('C:\\IBootTime\\python\\python.exe C:\\IBootTime\\agent\\client.py --server ' + $serverAddr)\r\n")
	sb.WriteString(")\r\n")
	sb.WriteString("$batLines | Out-File -FilePath $batFile -Encoding ascii\r\n")
	sb.WriteString("Log \"Batch launcher creado: $batFile\"\r\n\r\n")

	sb.WriteString("# Create scheduled task at system startup as SYSTEM (starts before user login)\r\n")
	sb.WriteString("schtasks /create /tn $taskName /tr $batFile /sc onstart /ru SYSTEM /rl HIGHEST /f 2>&1 | Out-Null\r\n\r\n")

	sb.WriteString("$task = schtasks /query /tn $taskName 2>&1\r\n")
	sb.WriteString("if ($task -match \"IBootTime Agent\") {\r\n")
	sb.WriteString("    Log \"Tarea programada creada OK.\"\r\n")
	sb.WriteString("} else {\r\n")
	sb.WriteString("    Log \"WARNING: No se pudo crear tarea programada. Usando metodo alternativo...\"\r\n")
	sb.WriteString("    $startup = \"C:\\ProgramData\\Microsoft\\Windows\\Start Menu\\Programs\\Startup\\iboottime_agent.bat\"\r\n")
	sb.WriteString("    Copy-Item $batFile $startup -Force -ErrorAction SilentlyContinue\r\n")
	sb.WriteString("    Log \"Script de inicio alternativo creado.\"\r\n")
	sb.WriteString("}\r\n\r\n")

	sb.WriteString("# Start the agent now\r\n")
	sb.WriteString("Log \"Iniciando agente ahora...\"\r\n")
	sb.WriteString("Start-Process -FilePath $batFile -WindowStyle Hidden\r\n")
	sb.WriteString("Start-Sleep -Seconds 3\r\n")
	sb.WriteString("Log \"Agente iniciado.\"\r\n\r\n")

	sb.WriteString("Log \"=== IBootTime Agent Setup COMPLETE ===\"\r\n")

	return sb.String()
}
