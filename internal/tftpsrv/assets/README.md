# iPXE Boot Assets

Place the following binary files in this directory:

| Archivo         | Para qué sirve                             | Cómo obtenerlo |
|-----------------|---------------------------------------------|----------------|
| `undionly.kpxe` | Legacy BIOS chainloader                    | https://boot.ipxe.org/undionly.kpxe |
| `ipxe.efi`      | UEFI generic bootloader                    | https://boot.ipxe.org/ipxe.efi |
| `snp.efi`       | UEFI SNP driver bootloader                 | https://boot.ipxe.org/snp.efi |
| `shimx64.efi`   | **Secure Boot**: shim firmado por Microsoft| Ver abajo |

## shimx64.efi (Secure Boot)

Para habilitar arranque en red con Secure Boot activado, ejecuta el script de descarga:

```powershell
powershell -ExecutionPolicy Bypass -File scripts\download-shim.ps1
```

El script descarga el shim de Fedora 39, que está firmado por la clave
**Microsoft Corporation UEFI CA 2011** (ya incluida en todos los firmwares
modernos). Esto significa que funciona con Secure Boot **sin necesidad de
enrolar claves adicionales**.

Flujo de arranque con Secure Boot activo:
```
UEFI Firmware  →  shimx64.efi (firmado por MS)  →  ipxe.efi  →  boot.ipxe
```

Después de copiar shimx64.efi, recompila con `wails build` y activa
"Secure Boot" en la configuración de red de IBootTime.

These files are embedded into the final binary at compile time.
