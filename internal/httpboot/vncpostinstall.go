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
# This script is downloaded and executed by the unattend.xml specialize pass.
# It runs as SYSTEM during Windows setup, before the OOBE screens appear.

$ErrorActionPreference = "SilentlyContinue"
$serverIP   = "%s"
$httpPort   = %d
$server     = "http://${serverIP}:${httpPort}"
$vncDir     = "C:\IBootTime\vnc"
$vncPort    = %d
$reversePort = %d

# --- Create VNC directory ---
New-Item -ItemType Directory -Path $vncDir -Force | Out-Null

# --- Wait for network ---
Write-Host "[IBootTime] VNC PostInstall: Esperando red..."
$timeout = 120
$elapsed = 0
while (-not (Test-Connection $serverIP -Count 1 -Quiet) -and $elapsed -lt $timeout) {
    Start-Sleep -Seconds 2
    $elapsed += 2
}
if ($elapsed -ge $timeout) {
    Write-Host "[IBootTime] VNC PostInstall: No se pudo contactar al servidor. Abortando."
    exit 1
}
Write-Host "[IBootTime] VNC PostInstall: Servidor alcanzado."

# --- Download essential VNC files ---
$files = @(%s)
foreach ($f in $files) {
    Write-Host "[IBootTime] VNC PostInstall: Descargando $f..."
    try {
        Invoke-WebRequest -Uri "$server/api/vnc/files/$f" -OutFile "$vncDir\$f" -UseBasicParsing -TimeoutSec 30
    } catch {
        Write-Host "[IBootTime] VNC PostInstall: Error descargando ${f}: $_"
    }
}

# Verify winvnc.exe exists
if (-not (Test-Path "$vncDir\winvnc.exe")) {
    Write-Host "[IBootTime] VNC PostInstall: winvnc.exe no descargado. Abortando."
    exit 1
}

# --- Download ultravnc.ini ---
Write-Host "[IBootTime] VNC PostInstall: Descargando ultravnc.ini..."
try {
    Invoke-WebRequest -Uri "$server/api/winpe/vnc-ini" -OutFile "$vncDir\ultravnc.ini" -UseBasicParsing -TimeoutSec 15
} catch {
    Write-Host "[IBootTime] VNC PostInstall: Error descargando INI: $_"
}

# --- Download password ---
$pw = ""
try {
    $pw = (Invoke-WebRequest -Uri "$server/api/winpe/vnc-password" -UseBasicParsing -TimeoutSec 15).Content.Trim()
} catch {
    Write-Host "[IBootTime] VNC PostInstall: Error obteniendo password: $_"
}

# --- Open firewall ---
Write-Host "[IBootTime] VNC PostInstall: Configurando firewall..."
netsh advfirewall set allprofiles state off 2>&1 | Out-Null
netsh advfirewall firewall add rule name="IBootTime VNC" dir=in action=allow protocol=tcp localport=$vncPort profile=any 2>&1 | Out-Null

# --- Start VNC server ---
Write-Host "[IBootTime] VNC PostInstall: Iniciando winvnc.exe..."
Set-Location $vncDir
Start-Process -FilePath "$vncDir\winvnc.exe" -ArgumentList "-run" -WorkingDirectory $vncDir -WindowStyle Hidden
Start-Sleep -Seconds 5

# --- Get our IP ---
$myIP = $null
$adapters = Get-NetIPAddress -AddressFamily IPv4 | Where-Object {
    $_.IPAddress -ne "127.0.0.1" -and $_.PrefixOrigin -ne "WellKnown"
}
if ($adapters) {
    $myIP = ($adapters | Select-Object -First 1).IPAddress
}
if (-not $myIP) {
    # Fallback: parse ipconfig
    $ipconfig = ipconfig | Select-String "IPv4" | ForEach-Object { ($_ -split ":\s*")[1].Trim() } | Where-Object { $_ -ne "127.0.0.1" } | Select-Object -First 1
    $myIP = $ipconfig
}
if (-not $myIP) {
    Write-Host "[IBootTime] VNC PostInstall: No se pudo obtener IP. Abortando."
    exit 1
}
Write-Host "[IBootTime] VNC PostInstall: IP del cliente: $myIP"

# --- Send beacon ---
Write-Host "[IBootTime] VNC PostInstall: Enviando beacon..."
$body = @{ ip = $myIP; port = $vncPort; password = $pw } | ConvertTo-Json
try {
    Invoke-WebRequest -Uri "$server/api/winpe/remote-ready" -Method Post -Body $body -ContentType "application/json" -UseBasicParsing -TimeoutSec 15 | Out-Null
    Write-Host "[IBootTime] VNC PostInstall: Beacon enviado OK."
} catch {
    Write-Host "[IBootTime] VNC PostInstall: Beacon fallo: $_"
}

# --- Poll loop ---
Write-Host "[IBootTime] VNC PostInstall: Listo en ${myIP}:${vncPort} - esperando orden del servidor..."
while ($true) {
    try {
        $response = (Invoke-WebRequest -Uri "$server/api/winpe/vnc-check?ip=$myIP" -UseBasicParsing -TimeoutSec 10).Content
        if ($response -match '"connect"\s*:\s*true') {
            Write-Host "[IBootTime] VNC PostInstall: Servidor solicito conexion! Conectando..."
            Start-Process -FilePath "$vncDir\winvnc.exe" -ArgumentList "-connect ${serverIP}:${reversePort}" -WorkingDirectory $vncDir -WindowStyle Hidden
            Write-Host "[IBootTime] VNC PostInstall: Conexion reversa iniciada."
            Start-Sleep -Seconds 3
        }
    } catch {
        # Server not reachable, will retry
    }
    Start-Sleep -Seconds 5
}
`, s.serverIP, s.port, vncPort, DefaultReverseVNCPort, fileList)
}
