# UltraVNC Portable para WinPE Remote Control

Coloca aquí los archivos de UltraVNC portable necesarios para el control remoto durante WinPE.

## Archivos requeridos

- `winvnc.exe` — Servidor UltraVNC (obligatorio)
- Cualquier DLL adicional que necesite winvnc.exe

## Cómo obtener UltraVNC portable

1. Descarga UltraVNC desde https://uvnc.com/downloads/ultravnc.html
2. Instala o extrae los archivos
3. Copia `winvnc.exe` (y sus DLLs) a esta carpeta

## Nota

IBootTime generará automáticamente:
- `ultravnc.ini` con la contraseña VNC (generada por sesión)
- `start_vnc.cmd` script de inicio

Estos archivos se inyectan en el boot.wim durante la preparación del ISO.
