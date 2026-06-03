# Downloads Python embeddable (portable, no install needed) for bundling with the agent.
# Place the result in tools/python-embed/ — this gets injected into the WIM alongside agent_client.
# Run this ONCE before building/deploying ISOs with the agent.

param(
    [string]$Version = "3.13.3",
    [string]$OutputDir = "$PSScriptRoot\..\tools\python-embed"
)

$ErrorActionPreference = "Stop"

$arch = "amd64"
$zipName = "python-$Version-embed-$arch.zip"
$url = "https://www.python.org/ftp/python/$Version/$zipName"
$tempZip = "$env:TEMP\$zipName"

Write-Host "[IBootTime] Downloading Python $Version embeddable ($arch)..."
Write-Host "[IBootTime] URL: $url"

# Clean and create output dir
if (Test-Path $OutputDir) {
    Remove-Item -Recurse -Force $OutputDir
}
New-Item -ItemType Directory -Path $OutputDir -Force | Out-Null

# Download
Invoke-WebRequest -Uri $url -OutFile $tempZip -UseBasicParsing
Write-Host "[IBootTime] Downloaded: $tempZip ($('{0:N1}' -f ((Get-Item $tempZip).Length / 1MB)) MB)"

# Extract
Expand-Archive -Path $tempZip -DestinationPath $OutputDir -Force
Remove-Item $tempZip -Force

# Enable pip/site-packages by uncommenting import site in python*._pth
$pthFile = Get-ChildItem "$OutputDir\python*._pth" | Select-Object -First 1
if ($pthFile) {
    $content = Get-Content $pthFile.FullName
    $content = $content -replace "^#import site", "import site"
    Set-Content $pthFile.FullName $content
    Write-Host "[IBootTime] Enabled site-packages in $($pthFile.Name)"
}

# Install pip
Write-Host "[IBootTime] Installing pip..."
Invoke-WebRequest -Uri "https://bootstrap.pypa.io/get-pip.py" -OutFile "$OutputDir\get-pip.py" -UseBasicParsing
& "$OutputDir\python.exe" "$OutputDir\get-pip.py" --no-warn-script-location 2>&1 | Out-Null
Remove-Item "$OutputDir\get-pip.py" -Force -ErrorAction SilentlyContinue

# Install dependencies for both the post-install client and the local API server.
Write-Host "[IBootTime] Installing agent dependencies..."
& "$OutputDir\python.exe" -m pip install -r "$PSScriptRoot\..\agent_server\requirements.txt" --no-warn-script-location --quiet 2>&1 | Out-Null
& "$OutputDir\python.exe" -m pip install -r "$PSScriptRoot\..\agent_client\requirements.txt" --no-warn-script-location --quiet 2>&1 | Out-Null

# Cleanup pip cache
$pipCache = "$OutputDir\Lib\site-packages\pip"
# Keep pip but clean cache
& "$OutputDir\python.exe" -m pip cache purge 2>&1 | Out-Null

$totalSize = (Get-ChildItem $OutputDir -Recurse | Measure-Object -Property Length -Sum).Sum / 1MB
Write-Host "[IBootTime] Python embedded ready at: $OutputDir"
Write-Host "[IBootTime] Total size: $('{0:N1}' -f $totalSize) MB"
Write-Host ""
Write-Host "[IBootTime] DONE. Now run 'wails dev' or 'wails build' and the agent will be"
Write-Host "[IBootTime] automatically injected into ISOs when WinPE Remote is enabled."
