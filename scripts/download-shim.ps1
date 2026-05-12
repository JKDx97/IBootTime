# ============================================================
# download-shim.ps1 — Descarga shimx64.efi para IBootTime
# ============================================================
# El shim de Fedora esta firmado por Microsoft Corporation UEFI CA 2011,
# que ya esta en la base de datos DB de practicamente todos los firmwares
# modernos. Esto permite arranque en red con Secure Boot sin necesidad de
# enrolar claves adicionales.
#
# Uso:
#   powershell -ExecutionPolicy Bypass -File scripts\download-shim.ps1
# ============================================================

$assetsDir = Join-Path $PSScriptRoot "..\internal\tftpsrv\assets"
$shimDest  = Join-Path $assetsDir "shimx64.efi"

Write-Host "=== IBootTime: Descarga de shimx64.efi ===" -ForegroundColor Cyan

# URL del shim de Fedora 39 (firmado por Microsoft)
# Fuente: https://kojipkgs.fedoraproject.org/packages/shim/
$shimUrl = "https://kojipkgs.fedoraproject.org/packages/shim/15.8/3/x86_64/shim-x64-15.8-3.x86_64.rpm"
$rpmPath = Join-Path $env:TEMP "shim-x64.rpm"

Write-Host "Descargando RPM de Fedora..." -ForegroundColor Yellow
try {
    Invoke-WebRequest -Uri $shimUrl -OutFile $rpmPath -UseBasicParsing
    Write-Host "RPM descargado: $rpmPath" -ForegroundColor Green
} catch {
    Write-Host "ERROR descargando RPM: $_" -ForegroundColor Red
    Write-Host ""
    Write-Host "Alternativa manual:" -ForegroundColor Yellow
    Write-Host "  1. Descarga el RPM desde: https://kojipkgs.fedoraproject.org/packages/shim/"
    Write-Host "  2. Extrae shimx64.efi con 7-Zip (el RPM es un archivo comprimido)"
    Write-Host "  3. Copia shimx64.efi a: $assetsDir"
    exit 1
}

# Extraer shimx64.efi del RPM usando 7-Zip
$sevenZip = @(
    "C:\Program Files\7-Zip\7z.exe",
    "C:\Program Files (x86)\7-Zip\7z.exe"
) | Where-Object { Test-Path $_ } | Select-Object -First 1

if (-not $sevenZip) {
    Write-Host ""
    Write-Host "7-Zip no encontrado. Extraccion manual necesaria:" -ForegroundColor Yellow
    Write-Host "  1. Instala 7-Zip desde https://www.7-zip.org/"
    Write-Host "  2. Extrae el RPM y copia boot/efi/EFI/fedora/shimx64.efi a:"
    Write-Host "     $assetsDir"
    Write-Host ""
    Write-Host "O usa WSL/Linux para extraer:"
    Write-Host "  rpm2cpio shim-x64.rpm | cpio -idmv"
    Write-Host "  cp ./boot/efi/EFI/fedora/shimx64.efi $assetsDir"
    exit 1
}

Write-Host "Extrayendo RPM con 7-Zip..." -ForegroundColor Yellow
$extractDir = Join-Path $env:TEMP "shim-extract"
New-Item -ItemType Directory -Path $extractDir -Force | Out-Null

# RPM -> CPIO -> extraer
& $sevenZip e $rpmPath -o$extractDir -y 2>&1 | Out-Null
$cpioFile = Get-ChildItem $extractDir | Select-Object -First 1
if ($cpioFile) {
    & $sevenZip e $cpioFile.FullName -o$extractDir -y 2>&1 | Out-Null
}

# Buscar shimx64.efi en la extraccion
$found = Get-ChildItem $extractDir -Recurse -Filter "shimx64.efi" | Select-Object -First 1
if ($found) {
    Copy-Item $found.FullName $shimDest -Force
    Write-Host ""
    Write-Host "shimx64.efi copiado exitosamente a:" -ForegroundColor Green
    Write-Host "  $shimDest" -ForegroundColor Green
    $size = (Get-Item $shimDest).Length
    Write-Host "  Tamano: $([math]::Round($size/1KB, 1)) KB" -ForegroundColor Green
    Write-Host ""
    Write-Host "Siguiente paso:" -ForegroundColor Cyan
    Write-Host "  Recompila IBootTime con: wails build"
    Write-Host "  Luego activa 'Secure Boot' en la configuracion de red de IBootTime."
} else {
    Write-Host "ERROR: shimx64.efi no encontrado en el RPM extraido." -ForegroundColor Red
    Write-Host "Contenido extraido en: $extractDir"
}

# Limpiar temporales
Remove-Item $rpmPath -Force -ErrorAction SilentlyContinue
Remove-Item $extractDir -Recurse -Force -ErrorAction SilentlyContinue
