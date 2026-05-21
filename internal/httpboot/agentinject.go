package httpboot

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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
