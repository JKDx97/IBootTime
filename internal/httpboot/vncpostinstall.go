package httpboot

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// essentialVNCFiles lists the minimum files needed for UltraVNC on full Windows.
var essentialVNCFiles = []string{
	"winvnc.exe",
	"vnchooks.dll",
	"ddengine64.dll",
	"logging.dll",
	"logmessages.dll",
	"authSSP.dll",
	"authadmin.dll",
}

// handleVNCFileDownload serves individual VNC binary files from the remote/winvnc directory.
// GET /api/vnc/files/<filename>
func (s *Server) handleVNCFileDownload(w http.ResponseWriter, r *http.Request) {
	// Extract filename from path: /api/vnc/files/winvnc.exe -> winvnc.exe
	prefix := "/api/vnc/files/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	filename := strings.TrimPrefix(r.URL.Path, prefix)
	if filename == "" || strings.Contains(filename, "..") || strings.Contains(filename, "/") || strings.Contains(filename, "\\") {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}

	vncDir := s.findVNCDir()
	if vncDir == "" {
		http.Error(w, "VNC directory not found on server", http.StatusInternalServerError)
		return
	}

	filePath := filepath.Join(vncDir, filename)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		http.Error(w, "file not found: "+filename, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)
	http.ServeFile(w, r, filePath)
}

// handleVNCSetupScript serves a PowerShell script that sets up VNC on a freshly
// installed Windows (post-reboot, during specialize/OOBE). The script:
// 1. Downloads essential VNC files from the IBootTime HTTP server
// 2. Downloads ultravnc.ini and password
// 3. Opens firewall
// 4. Starts winvnc.exe
// 5. Sends beacon to server
// 6. Polls for connect trigger
// GET /api/vnc/setup-script
func (s *Server) handleVNCSetupScript(w http.ResponseWriter, r *http.Request) {
	vncPort := s.cfg.GetWinPEVncPort()

	script := s.buildPostInstallVNCScript(vncPort)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, script)
}

// handleUnattend generates a minimal unattend.xml that starts VNC during the
// Windows specialize pass (before OOBE) and restarts it at first logon.
// GET /api/vnc/unattend
func (s *Server) handleUnattend(w http.ResponseWriter, r *http.Request) {
	xml := s.buildUnattendXML()

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	io.WriteString(w, xml)
}

// buildUnattendXML generates a minimal unattend.xml that only contains VNC setup.
// It uses RunSynchronous in the specialize pass to download and start VNC before
// OOBE screens appear, so the operator has remote visibility during the entire
// out-of-box experience.
//
// IMPORTANT: Commands must be simple to avoid XML escaping issues. We use
// cmd /c start /b to background PowerShell, and irm (Invoke-RestMethod) to
// download+execute the setup script in one line.
func (s *Server) buildUnattendXML() string {
	// specialize: runs after first reboot, before OOBE.
	// "cmd /c start /b" launches PowerShell in background so it doesn't block setup.
	// "irm" is a PowerShell alias for Invoke-RestMethod (available since PS 3.0).
	specializeCmd := fmt.Sprintf(
		`cmd /c start /b powershell -ExecutionPolicy Bypass -WindowStyle Hidden -Command "irm 'http://%s:%d/api/vnc/setup-script' | Out-File C:\ibt_vnc.ps1; . C:\ibt_vnc.ps1"`,
		s.serverIP, s.port,
	)

	// oobeSystem/FirstLogonCommands: restarts VNC if it died during desktop transition.
	firstLogonCmd := fmt.Sprintf(
		`cmd /c start /b powershell -ExecutionPolicy Bypass -WindowStyle Hidden -Command "irm 'http://%s:%d/api/vnc/setup-script' | Out-File C:\ibt_vnc.ps1; . C:\ibt_vnc.ps1"`,
		s.serverIP, s.port,
	)

	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<unattend xmlns="urn:schemas-microsoft-com:unattend">
  <settings pass="specialize">
    <component name="Microsoft-Windows-Deployment" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
      <RunSynchronous>
        <RunSynchronousCommand wcm:action="add">
          <Order>1</Order>
          <Path>%s</Path>
        </RunSynchronousCommand>
      </RunSynchronous>
    </component>
  </settings>
  <settings pass="oobeSystem">
    <component name="Microsoft-Windows-Shell-Setup" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
      <FirstLogonCommands>
        <SynchronousCommand wcm:action="add">
          <Order>1</Order>
          <CommandLine>%s</CommandLine>
        </SynchronousCommand>
      </FirstLogonCommands>
    </component>
  </settings>
</unattend>`, specializeCmd, firstLogonCmd)
}

// buildPostInstallVNCScript generates a PowerShell script that runs on the
// freshly installed Windows to set up and start VNC for remote management.
func (s *Server) buildPostInstallVNCScript(vncPort int) string {
	fileList := ""
	for i, f := range essentialVNCFiles {
		if i > 0 {
			fileList += ", "
		}
		fileList += fmt.Sprintf(`"%s"`, f)
	}

	return fmt.Sprintf(`# IBootTime Post-Install VNC Setup Script

$ErrorActionPreference = "Stop"
$serverIP    = "%s"
$httpPort    = %d
$server      = "http://${serverIP}:${httpPort}"
$vncDir      = "C:\IBootTime\vnc"
$vncPort     = %d
$reversePort = %d
$lockFile    = "$vncDir\.postinstall.lock"
$logFile     = "$vncDir\postinstall.log"

function Log($msg) {
    $ts = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    $line = "[$ts] $msg"
    Write-Host $line
    try { $line | Out-File -Append -FilePath $logFile -Encoding utf8 } catch {}
}

New-Item -ItemType Directory -Path $vncDir -Force | Out-Null
Log "=== IBootTime VNC PostInstall START (PID=$PID) ==="
Log "Server=$server  vncPort=$vncPort  reversePort=$reversePort"

# --- Instance lock ---
if (Test-Path $lockFile) {
    $lockPid = Get-Content $lockFile -ErrorAction SilentlyContinue
    if ($lockPid) {
        $existingProc = $null
        try { $existingProc = Get-Process -Id ([int]$lockPid) -ErrorAction Stop } catch {}
        if ($existingProc -and $existingProc.ProcessName -match "(?i)^(powershell|pwsh)$") {
            Log "Otra instancia corriendo (PID $lockPid). Saliendo."
            exit 0
        }
    }
    Log "Lock stale (PID=$lockPid ya no existe), limpiando..."
    Remove-Item $lockFile -Force -ErrorAction SilentlyContinue
}
$PID | Out-File $lockFile -Force
Log "Lock adquirido (PID=$PID)"

# --- Wait for server (HTTP, infinite retry) ---
Log "Esperando servidor HTTP en $server/health ..."
$attempts = 0
while ($true) {
    $attempts++
    try {
        $null = Invoke-WebRequest -Uri "$server/health" -UseBasicParsing -TimeoutSec 8
        Log "Servidor alcanzado en intento $attempts."
        break
    } catch {
        if ($attempts %% 10 -eq 0) { Log "Intento $attempts - aun esperando: $_" }
        Start-Sleep -Seconds 3
    }
}

# --- Download missing VNC files ---
$files = @(%s)
foreach ($f in $files) {
    if (Test-Path "$vncDir\$f") {
        Log "Archivo $f ya existe, skip."
        continue
    }
    Log "Descargando $f..."
    try {
        Invoke-WebRequest -Uri "$server/api/vnc/files/$f" -OutFile "$vncDir\$f" -UseBasicParsing -TimeoutSec 30
        Log "$f descargado OK."
    } catch {
        Log "ERROR descargando ${f}: $_"
    }
}

if (-not (Test-Path "$vncDir\winvnc.exe")) {
    Log "FATAL: winvnc.exe no existe. Abortando."
    Remove-Item $lockFile -Force -ErrorAction SilentlyContinue
    exit 1
}
Log "winvnc.exe presente OK."

# --- Always re-download INI ---
Log "Descargando ultravnc.ini..."
try {
    Invoke-WebRequest -Uri "$server/api/winpe/vnc-ini" -OutFile "$vncDir\ultravnc.ini" -UseBasicParsing -TimeoutSec 15
    Log "ultravnc.ini OK."
} catch {
    Log "ERROR descargando INI: $_"
}

$pw = ""
try {
    $pw = (Invoke-WebRequest -Uri "$server/api/winpe/vnc-password" -UseBasicParsing -TimeoutSec 15).Content.Trim()
    Log "Password obtenido OK (len=$($pw.Length))."
} catch {
    Log "ERROR obteniendo password: $_"
}

# --- Firewall ---
Log "Configurando firewall..."
$ErrorActionPreference = "SilentlyContinue"
netsh advfirewall set allprofiles state off 2>&1 | Out-Null
netsh advfirewall firewall add rule name="IBootTime VNC" dir=in action=allow protocol=tcp localport=$vncPort profile=any 2>&1 | Out-Null
netsh advfirewall firewall add rule name="IBootTime VNC Reverse" dir=out action=allow protocol=tcp remoteport=$reversePort profile=any 2>&1 | Out-Null
$ErrorActionPreference = "Stop"
Log "Firewall configurado."

# --- Helper: install winvnc as service (hides tray icon) and start with reverse connection ---
$global:svcInstalled = $false
function Install-VNC-Service {
    if ($global:svcInstalled) { return }
    Log "Instalando winvnc como servicio..."
    $p = Start-Process -FilePath "$vncDir\winvnc.exe" -ArgumentList "-install" -WorkingDirectory $vncDir -WindowStyle Hidden -Wait -PassThru
    Start-Sleep -Seconds 2
    # Verify service exists
    $svc = Get-Service -Name "uvnc_service" -ErrorAction SilentlyContinue
    if (-not $svc) {
        # Some UltraVNC versions register as "winvnc"
        $svc = Get-Service -Name "winvnc" -ErrorAction SilentlyContinue
    }
    if ($svc) {
        Log "Servicio VNC instalado OK: $($svc.Name)"
        $global:svcInstalled = $true
    } else {
        Log "WARNING: No se detecto servicio VNC tras -install. Usando modo app como fallback."
    }
}

function Start-VNC-Reverse {
    Log "Deteniendo winvnc existente..."
    Get-Process winvnc -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    Start-Sleep -Seconds 2

    Install-VNC-Service

    if ($global:svcInstalled) {
        # Start via service — DisableTrayIcon=1 is honored, no tray icon
        $svcName = if (Get-Service -Name "uvnc_service" -ErrorAction SilentlyContinue) { "uvnc_service" } else { "winvnc" }
        Log "Iniciando servicio $svcName..."
        Start-Service -Name $svcName -ErrorAction SilentlyContinue
        Start-Sleep -Seconds 3
        # Trigger reverse connection
        Log "Disparando conexion reversa a ${serverIP}:${reversePort}..."
        Start-Process -FilePath "$vncDir\winvnc.exe" -ArgumentList "-autoreconnect -connect ${serverIP}:${reversePort}" -WorkingDirectory $vncDir -WindowStyle Hidden
    } else {
        # Fallback: run as app (tray icon may appear)
        Log "Iniciando winvnc.exe -autoreconnect -connect ${serverIP}:${reversePort} -run (fallback)..."
        Start-Process -FilePath "$vncDir\winvnc.exe" -ArgumentList "-autoreconnect -connect ${serverIP}:${reversePort} -run" -WorkingDirectory $vncDir -WindowStyle Hidden
    }
    Start-Sleep -Seconds 5
    $proc = Get-Process winvnc -ErrorAction SilentlyContinue
    if ($proc) {
        Log "winvnc.exe corriendo (PID=$($proc.Id))."
    } else {
        Log "WARNING: winvnc.exe NO aparece en procesos!"
    }
}

Set-Location $vncDir

# --- Get our IP ---
$myIP = $null
$adapters = Get-NetIPAddress -AddressFamily IPv4 -ErrorAction SilentlyContinue | Where-Object {
    $_.IPAddress -ne "127.0.0.1" -and $_.PrefixOrigin -ne "WellKnown"
}
if ($adapters) {
    $myIP = ($adapters | Select-Object -First 1).IPAddress
}
if (-not $myIP) {
    $ipconfig = ipconfig | Select-String "IPv4" | ForEach-Object { ($_ -split ":\s*")[1].Trim() } | Where-Object { $_ -ne "127.0.0.1" } | Select-Object -First 1
    $myIP = $ipconfig
}
if (-not $myIP) {
    Log "FATAL: No se pudo obtener IP. Abortando."
    Remove-Item $lockFile -Force -ErrorAction SilentlyContinue
    exit 1
}
Log "IP del cliente: $myIP"

# --- Beacon helper ---
function Send-Beacon {
    $body = @{ ip = $myIP; port = $vncPort; password = $pw } | ConvertTo-Json
    try {
        Invoke-WebRequest -Uri "$server/api/winpe/remote-ready" -Method Post -Body $body -ContentType "application/json" -UseBasicParsing -TimeoutSec 15 | Out-Null
        Log "Beacon enviado OK."
    } catch {
        Log "Beacon FALLO: $_"
    }
}

# --- Start winvnc with reverse connect (single command, no IPC issue) ---
Start-VNC-Reverse

# --- Initial beacon ---
Send-Beacon

# --- Main polling loop ---
Log "Entrando en loop de polling..."
$lastBeacon = Get-Date
$ErrorActionPreference = "SilentlyContinue"
while ($true) {
    if (((Get-Date) - $lastBeacon).TotalSeconds -ge 60) {
        Send-Beacon
        $lastBeacon = Get-Date
        if (-not (Get-Process winvnc -ErrorAction SilentlyContinue)) {
            Log "winvnc.exe murio, reiniciando con -connect..."
            Start-VNC-Reverse
        }
    }
    try {
        $response = (Invoke-WebRequest -Uri "$server/api/winpe/vnc-check?ip=$myIP" -UseBasicParsing -TimeoutSec 10).Content
        if ($response -match '"connect"\s*:\s*true') {
            Log "Servidor solicito reconexion! Reiniciando winvnc..."
            Start-VNC-Reverse
            Log "Reconexion completada."
        }
    } catch {}
    Start-Sleep -Seconds 5
}
`, s.serverIP, s.port, vncPort, DefaultReverseVNCPort, fileList)
}

func (s *Server) buildSetupCompleteCMD() string {
	return "@echo off\r\n" +
		"reg add \"HKLM\\SOFTWARE\\Microsoft\\Windows\\CurrentVersion\\Run\" /v \"IBootTime VNC\" /t REG_SZ /d \"powershell -ExecutionPolicy Bypass -WindowStyle Hidden -File C:\\IBootTime\\vnc\\postinstall_vnc.ps1\" /f >nul 2>&1\r\n" +
		"start /b powershell -ExecutionPolicy Bypass -WindowStyle Hidden -File C:\\IBootTime\\vnc\\postinstall_vnc.ps1\r\n" +
		"exit /b 0\r\n"
}

func (s *Server) buildPostInstallBootstrapCMD() string {
	// This script runs in background in WinPE while Windows Setup installs.
	// It continuously scans for the target Windows partition and deploys
	// SetupComplete.cmd + VNC files. Key design decisions:
	// - NEVER exits: keeps polling until WinPE reboots (handles format+reinstall)
	// - Polls every 3 seconds for fast detection
	// - Nested if blocks for reliable batch parsing
	// - Verifies deployment succeeded before moving on
	return "@echo off\r\n" +
		"setlocal EnableExtensions\r\n" +
		"echo [IBootTime] PostInstall Watcher: Iniciado\r\n" +
		"echo [IBootTime] PostInstall Watcher: Esperando particion Windows...\r\n" +
		":scan\r\n" +
		"for %%D in (C D E F G H I J K L M N O P Q R S T U V W Y Z) do (\r\n" +
		"  if /I not \"%%D\"==\"X\" (\r\n" +
		"    if exist \"%%D:\\Windows\\System32\\config\\SYSTEM\" (\r\n" +
		"      if not exist \"%%D:\\Windows\\Setup\\Scripts\\SetupComplete.cmd\" (\r\n" +
		"        echo [IBootTime] PostInstall Watcher: Particion detectada en %%D:\r\n" +
		"        call :deploy %%D\r\n" +
		"      )\r\n" +
		"    )\r\n" +
		"  )\r\n" +
		")\r\n" +
		"ping -n 3 127.0.0.1 >nul\r\n" +
		"goto scan\r\n" +
		":deploy\r\n" +
		"set TARGET=%1\r\n" +
		"echo [IBootTime] PostInstall Watcher: Desplegando en %TARGET%: ...\r\n" +
		"mkdir \"%TARGET%:\\IBootTime\\vnc\" >nul 2>&1\r\n" +
		"mkdir \"%TARGET%:\\Windows\\Setup\\Scripts\" >nul 2>&1\r\n" +
		"xcopy \"X:\\IBootTime\\vnc\\*\" \"%TARGET%:\\IBootTime\\vnc\\\" /E /I /Y /Q >nul 2>&1\r\n" +
		"(\r\n" +
		"echo @echo off\r\n" +
		"echo reg add \"HKLM\\SOFTWARE\\Microsoft\\Windows\\CurrentVersion\\Run\" /v \"IBootTime VNC\" /t REG_SZ /d \"powershell -ExecutionPolicy Bypass -WindowStyle Hidden -File C:\\IBootTime\\vnc\\postinstall_vnc.ps1\" /f ^>nul 2^>^&1\r\n" +
		"echo start /b powershell -ExecutionPolicy Bypass -WindowStyle Hidden -File C:\\IBootTime\\vnc\\postinstall_vnc.ps1\r\n" +
		"echo exit /b 0\r\n" +
		") > \"%TARGET%:\\Windows\\Setup\\Scripts\\SetupComplete.cmd\"\r\n" +
		"if exist \"%TARGET%:\\Windows\\Setup\\Scripts\\SetupComplete.cmd\" (\r\n" +
		"  echo [IBootTime] PostInstall Watcher: SetupComplete.cmd creado en %TARGET%: OK\r\n" +
		") else (\r\n" +
		"  echo [IBootTime] PostInstall Watcher: FALLO creando SetupComplete.cmd en %TARGET%:\r\n" +
		")\r\n" +
		"goto :eof\r\n"
}
