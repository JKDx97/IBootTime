# IBootTime

A **Wails v2** (Go + React) PXE/Network Boot Server application - an iVentoy-style clone for network booting ISO images over the network without USB drives.

![Tech Stack](https://img.shields.io/badge/Go-1.26.1-blue) ![Wails](https://img.shields.io/badge/Wails-v2.12.0-purple) ![React](https://img.shields.io/badge/React-19-blue) ![Tailwind](https://img.shields.io/badge/TailwindCSS-v4-teal)

## Features

- **Proxy PXE Mode**: Works alongside your existing router/DHCP server - no IP conflicts or network disruption
- **Windows Installation Over Network**: Automated Windows 10/11 installation via SMB shares with injected network drivers
- **Linux Live Boot**: Boot Ubuntu, Debian, Fedora, and other Linux distros directly from ISO
- **Multi-Boot Support**: iPXE, GRUB, and Undionly boot protocols
- **Real-time Client Monitoring**: Track boot progress of all connected PXE clients
- **Automatic ISO Detection**: Detects Windows, Linux, WinPE, and utility ISOs automatically
- **Network Driver Injection**: Automatically injects NIC drivers into Windows PE for Win11 compatibility
- **Cross-Platform**: Windows (primary), Linux, and macOS support

---

## Project Structure

```
IBootTime/
├── app.go                          # Main Wails application logic
├── main.go                         # Entry point
├── wails.json                      # Wails project configuration
├── go.mod / go.sum                 # Go dependencies
│
├── build/                          # Build artifacts
│   ├── bin/                        # Compiled binaries
│   ├── darwin/                     # macOS-specific files
│   ├── windows/                    # Windows-specific files
│   └── appicon.png                 # Application icon
│
├── frontend/                       # React frontend
│   ├── src/
│   │   ├── App.jsx                 # Main app component
│   │   ├── main.jsx                # Entry point
│   │   ├── style.css               # TailwindCSS styles
│   │   ├── assets/                 # Static assets
│   │   └── components/             # React components
│   │       ├── Dashboard.jsx       # Server control & status
│   │       ├── NetworkConfig.jsx   # Network/interface settings
│   │       ├── IsoManager.jsx      # ISO catalog management
│   │       └── ClientMonitor.jsx   # Client boot monitoring
│   ├── wailsjs/                    # Auto-generated Wails bindings
│   ├── package.json                # Node dependencies
│   └── index.html                  # HTML template
│
└── internal/                       # Backend packages
    ├── config/                     # JSON configuration with mutex-safe access
    │   └── config.go
    ├── dhcpsrv/                    # DHCP server (proxy PXE mode)
    │   └── server.go
    ├── httpboot/                   # HTTP server for iPXE scripts & ISO serving
    │   ├── server.go
    │   ├── isohelper.go            # ISO streaming helpers
    │   └── assets/                 # iPXE boot assets
    ├── isomgr/                     # ISO scanner & OS detection
    │   ├── scanner.go
    │   └── types.go                # ISO metadata types
    ├── session/                    # Client session tracking
    ├── logger/                     # Centralized logging with frontend bridge
    ├── tftpsrv/                    # TFTP server (serves iPXE binaries)
    └── orchestrator/               # Service coordinator (start/stop all services)
```

---

### Architecture

### Pure Proxy PXE Mode

IBootTime operates exclusively in **proxy PXE mode** - it works alongside your existing router/DHCP server without interfering with normal network operations. Unlike traditional DHCP servers, IBootTime does NOT assign IP addresses - your router continues to handle that.

**How it works:**

1. **Stage 1 - PXE ROM Boot** (DHCP Port 67):
   - Client sends DHCP DISCOVER with PXE options
   - IBootTime responds with a proxy OFFER (yiaddr=0) containing:
     - Boot filename (`ipxe.efi` for UEFI, `undionly.kpxe` for BIOS)
     - TFTP server IP (siaddr)
     - PXE vendor options (option 43 PXEBS_SKIP)
   - PXE ROM downloads iPXE binary via TFTP and executes it

2. **Stage 2 - iPXE Network Boot** (HTTP Port 8080):
   - iPXE requests `boot.ipxe` script via HTTP
   - Script presents a boot menu with all available ISOs
   - User selects an ISO to boot

3. **Windows Installation** (HTTP + SMB):
   - For Windows ISOs: wimboot loads BCD + boot.sdi + boot.wim via HTTP
   - Modified boot.wim injects startnet.cmd that maps SMB share
   - Windows Setup runs from network share with full install.wim access

### Backend Services

| Service   | Port    | Protocol | Purpose                                      |
|-----------|---------|----------|----------------------------------------------|
| DHCP      | 67/4011 | UDP      | Proxy PXE boot responses (PXE info only)     |
| TFTP      | 69      | UDP      | Serve iPXE binaries (undionly.kpxe, ipxe.efi)|
| HTTP      | 8080    | TCP      | iPXE scripts, ISO streaming, wimboot         |
| SMB       | 445     | TCP      | Windows ISO shares for installation        |

---

## Installation & Usage

### Prerequisites

- **Go** 1.26.1 or later
- **Node.js** v24.13.0 or later
- **Wails CLI** v2.12.0

```bash
# Install Wails CLI
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

### Development

```bash
# Clone the repository
git clone https://github.com/JKDx97/IBootTime.git
cd IBootTime

# Install frontend dependencies
cd frontend
npm install
cd ..

# Run in live development mode (hot reload)
wails dev
```

### Building

```bash
# Build production binary
wails build

# Build with debug info
wails build -debug

# Build for specific platform
wails build -platform windows/amd64
```

The compiled binary will be in `build/bin/IBootTime.exe`.

### Running

```bash
# Run the built binary
./build/bin/IBootTime

# Or on Windows
.\build\bin\IBootTime.exe
```

### Configuration

On first run, a configuration file `iboottime.json` is created next to the executable:

```json
{
  "interfaceName": "Ethernet",
  "isoDirectory": "C:\\ISOs",
  "httpPort": 8080,
  "tftpPort": 69,
  "bootProtocol": "ipxe"
}
```

#### Configuration Options

| Option           | Description                                                                 | Default    |
|------------------|-----------------------------------------------------------------------------|------------|
| `interfaceName`  | Network interface to bind services (e.g., "Ethernet", "Wi-Fi")              | (empty)    |
| `isoDirectory`   | Path to folder containing your ISO files                                     | (empty)    |
| `httpPort`       | HTTP server port for iPXE scripts and ISO streaming                         | 8080       |
| `tftpPort`       | TFTP server port (requires admin/root on port 69)                           | 69         |
| `bootProtocol`   | Boot protocol: `ipxe`, `grub`, or `undionly`                                | `ipxe`     |

#### Boot Protocols

- **`ipxe`** (Recommended): Two-stage boot - PXE ROM loads iPXE, then iPXE loads ISO via HTTP. Best compatibility.
- **`grub`**: Direct GRUB bootloader - UEFI clients get `grubx64.efi`, BIOS clients get `grub2pxe`.
- **`undionly`**: Minimal iPXE with UNDI driver only - smaller binary but requires working UNDI driver.

---

## System Requirements

### Minimum Requirements

- **OS**: Windows 10/11 (64-bit), Windows Server 2016+, Linux, macOS
- **Network**: Wired Ethernet connection (Wi-Fi not recommended for PXE server)
- **Privileges**: Administrator/root access (required for ports 67, 69, 445)
- **Disk Space**: 500MB for application + space for ISO files
- **RAM**: 4GB minimum (8GB recommended for serving multiple clients)

### Network Requirements

- Client machines must be on the **same subnet** as the IBootTime server
- DHCP must be enabled on your router (IBootTime does not assign IPs)
- No other PXE servers should be running on the network
- Firewall must allow the required ports (auto-configured on Windows)

---

## First-Time Setup Guide

### 1. Prepare Your Environment

Create a folder for your ISO files:
```powershell
# Windows
mkdir C:\ISOs
```

### 2. Add Windows Network Drivers (Important for Win11)

Windows 11 and newer WinPE versions often lack network drivers. Place your NIC drivers in a `drivers` folder next to the executable:

```
IBootTime/
├── IBootTime.exe
├── drivers/
│   └── lenovo/
│       └── nic-driver/
│           ├── rt640x64.inf
│           ├── rt640x64.cat
│           └── rt640x64.sys
```

IBootTime will automatically inject these drivers into the Windows PE boot image using DISM.

### 3. Add ISO Files

Copy your ISO files to the ISO directory:
- **Windows ISOs**: Windows 10, 11, Server editions
- **Linux ISOs**: Ubuntu, Debian, Fedora, etc.
- **Utility ISOs**: Clonezilla, GParted, Hiren's BootCD

### 4. Launch IBootTime

Run as Administrator (Windows) or root (Linux/macOS):
```powershell
.\IBootTime.exe
```

### 5. Configure the Application

1. **Select Network Interface**: Choose your wired Ethernet interface
2. **Set ISO Directory**: Browse to your ISO folder
3. **Scan ISOs**: Click "Scan" to catalog available images
4. **Start Server**: Click "Start Server" to begin serving PXE clients

### 6. Boot Client Machines

On client computers:
1. Enter BIOS/UEFI settings
2. Enable **Network Boot** (PXE Boot / LAN Boot)
3. Set network boot as first priority
4. Save and exit

The client will:
1. PXE boot and receive iPXE binary via TFTP
2. Load iPXE boot menu via HTTP
3. Show available ISOs from your catalog
4. Boot selected OS

---

## Windows Installation Over Network

IBootTime provides full Windows installation capability without USB drives through a sophisticated boot process:

### How Windows Network Install Works

1. **Boot Process**:
   - Client PXE boots and loads iPXE
   - iPXE loads `boot.ipxe` menu script from HTTP server
   - User selects Windows ISO from menu
   - iPXE downloads and runs `wimboot` kernel
   - wimboot loads Windows PE (boot.wim) with injected startnet.cmd

2. **Automated Network Setup**:
   - Windows PE initializes network with injected drivers
   - startnet.cmd automatically:
     - Waits for network connectivity
     - Maps SMB share: `net use Z: \\server\share`
     - Launches `Z:\setup.exe`
   - Windows Setup runs with full access to install.wim

3. **SMB Share Credentials**:
   - Username: `Administrador`
   - Password: `P0s31d0n`

### Windows ISO Preparation

IBootTime automatically prepares Windows ISOs in the background:

1. **Mount ISO**: Uses Windows Mount-DiskImage
2. **Create SMB Share**: Exposes ISO contents as `\\server\IB_<isoname>`
3. **Modify boot.wim**: Uses DISM to inject drivers and startup script
4. **Cache Result**: Modified boot.wim is cached for faster subsequent boots

The first time a Windows ISO is used, preparation may take 2-5 minutes. Subsequent boots use cached files.

---

## Linux Live Boot

IBootTime supports multiple Linux boot methods for maximum compatibility:

### Ubuntu/Debian (Casper)

```ipxe
kernel ${http-root}/iso/ubuntu/file/casper/vmlinuz boot=casper netboot=url url=${http-root}/iso/ubuntu/raw ip=dhcp
initrd ${http-root}/iso/ubuntu/file/casper/initrd
boot
```

### Generic Linux (Live Boot)

For distros using live boot structure:
```ipxe
kernel ${http-root}/iso/linux/file/live/vmlinuz boot=live fetch=${http-root}/iso/linux/file/live/filesystem.squashfs ip=dhcp
initrd ${http-root}/iso/linux/file/live/initrd.img
boot
```

### Fallback: SAN Boot

If kernel/initrd method fails, IBootTime falls back to SAN boot (iSCSI-like direct ISO boot):
```ipxe
sanboot --no-describe ${http-root}/iso/linux/raw
```

---

## Client Monitoring

The Dashboard shows real-time status of all PXE clients:

| State       | Description                                   |
|-------------|-----------------------------------------------|
| discovery   | Client sent DHCP DISCOVER, waiting for response |
| tftp        | Downloading iPXE binary via TFTP              |
| menu        | At iPXE boot menu                             |
| loading     | Loading selected ISO                          |
| completed   | Boot completed successfully                   |
| error       | Boot failed                                     |

Each client shows:
- MAC address and IP
- Architecture (BIOS/UEFI-x64/etc)
- Selected ISO
- Transfer progress and speed
- Boot duration

---

## Firewall Configuration

IBootTime automatically configures Windows Firewall when starting (requires admin). Manual rules for other systems:

### Windows Firewall (netsh)
```powershell
# DHCP
netsh advfirewall firewall add rule name="IBootTime DHCP" dir=in action=allow protocol=udp localport=67,68,4011

# TFTP
netsh advfirewall firewall add rule name="IBootTime TFTP" dir=in action=allow protocol=udp localport=69

# HTTP
netsh advfirewall firewall add rule name="IBootTime HTTP" dir=in action=allow protocol=tcp localport=8080

# SMB
netsh advfirewall firewall add rule name="IBootTime SMB" dir=in action=allow protocol=tcp localport=445
```

### Linux (iptables)
```bash
# DHCP
iptables -A INPUT -p udp --dport 67:68 -j ACCEPT
iptables -A INPUT -p udp --dport 4011 -j ACCEPT

# TFTP
iptables -A INPUT -p udp --dport 69 -j ACCEPT

# HTTP
iptables -A INPUT -p tcp --dport 8080 -j ACCEPT

# SMB
iptables -A INPUT -p tcp --dport 445 -j ACCEPT
```

---

## Troubleshooting

### "No DHCP response" on clients

1. Verify IBootTime is running as Administrator
2. Check Windows Firewall allows ports 67, 68, 4011 (UDP)
3. Ensure no other DHCP/PXE servers on network
4. Try selecting a different network interface in settings

### TFTP timeout / slow transfers

1. Verify firewall allows UDP port 69
2. Check network cable connection
3. Try using TFTP port 6969 instead of 69 (change in settings)

### Windows PE has no network (ipconfig shows no IP)

1. Ensure you have placed NIC drivers in `drivers/` folder
2. Check driver compatibility with Windows PE (Win11 requires newer drivers)
3. Verify IBootTime injected drivers (check logs)

### "Access denied" on SMB share

1. Windows client may block guest access. Run on client:
   ```powershell
   Set-ItemProperty -Path "HKLM:\SYSTEM\CurrentControlSet\Services\LanmanWorkstation\Parameters" -Name "AllowInsecureGuestAuth" -Value 1
   ```
2. Verify Windows Firewall allows SMB (port 445)

### ISO not appearing in menu

1. Click "Scan ISOs" button to refresh catalog
2. Check ISO has `.iso` extension
3. Verify ISO is not corrupted

### UEFI clients won't boot

1. Some UEFI implementations have buggy PXE ROMs
2. Try BIOS/Legacy mode if available
3. Update client UEFI firmware
4. Use `ipxe` boot protocol (most compatible)

---

## The PXE Boot Process Explained

Understanding the boot flow helps troubleshoot issues:

### Stage 1: PXE ROM → iPXE

1. **Client Power-On**: Client NIC sends DHCP DISCOVER with PXE options
2. **IBootTime Responds**: Sends proxy OFFER with:
   - `yiaddr=0` (we don't assign IPs)
   - `siaddr=<server-ip>` (TFTP server)
   - `file=undionly.kpxe` (for BIOS) or `ipxe.efi` (for UEFI)
   - Option 43 with PXEBS_SKIP (skip port 4011 discovery)
3. **Client Downloads**: TFTP transfers the iPXE binary (~300KB-700KB)
4. **iPXE Executes**: Chainloads and gains full network stack with HTTP support

### Stage 2: iPXE → Operating System

1. **iPXE DHCP**: Gets IP from router (normal DHCP, no PXE options needed)
2. **Script Request**: iPXE requests `http://server:8080/boot.ipxe`
3. **Menu Display**: iPXE shows boot menu with available ISOs
4. **User Selects**: Menu handler boots the selected OS:
   - **Windows**: Loads `wimboot` kernel + BCD + boot.sdi + boot.wim
   - **Linux**: Loads `vmlinuz` kernel + `initrd` + network parameters
   - **Generic**: Uses `sanboot` for direct ISO streaming

### UEFI vs BIOS Differences

| Feature          | BIOS (Legacy)              | UEFI                        |
|------------------|----------------------------|-----------------------------|
| iPXE Binary      | `undionly.kpxe`            | `ipxe.efi` or `snp.efi`    |
| Boot File Path   | `/boot/bcd`                | `/efi/microsoft/boot/bcd` |
| Windows Boot     | wimboot only               | wimboot + bootx64.efi      |
| Compatibility    | Very high                  | Varies by firmware vendor   |

---

## Advanced Configuration

### Custom Boot Scripts

Advanced users can modify the boot process:

1. **TFTP Boot Script** (`internal/tftpsrv/server.go`):
   - Modify `serveIPXEScript()` for custom TFTP-only menus
   - Useful for clients without HTTP support (VirtualBox built-in iPXE)

2. **HTTP Boot Script** (`internal/httpboot/server.go`):
   - Modify `handleBootScript()` for custom HTTP menus
   - Add custom menu entries, timeout behaviors, branding

### SMB Share Security

Default credentials are hardcoded for convenience:
- Username: `Administrador`
- Password: `P0s31d0n`

To change credentials, modify these constants in `internal/httpboot/server.go`:
```go
const smbUser = "YourUser"
const smbPass = "YourPassword"
```

### Driver Injection

For Windows 11 compatibility, drivers are injected into boot.wim using DISM:

1. Place drivers in `drivers/` folder (subfolders scanned recursively)
2. IBootTime mounts boot.wim (all indexes)
3. DISM adds drivers with `/recurse /forceunsigned` flags
4. Modified WIM is cached in `.bootcache/<isoname>/`

**Supported Driver Types**: `.inf`, `.sys`, `.cat`, `.dll`

### Port Configuration

If default ports are in use:

```json
{
  "tftpPort": 6969,
  "httpPort": 8888
}
```

**Note**: TFTP clients must support alternate ports. Most PXE ROMs use hardcoded port 69.

---

## Tech Stack

| Component    | Technology                           | Purpose                              |
|--------------|--------------------------------------|--------------------------------------|
| Backend      | Go 1.26.1                            | Core services, DHCP, TFTP, HTTP      |
| Framework    | Wails v2.12.0                        | Desktop app framework (Go + React)   |
| Frontend     | React 19                             | UI components, real-time updates     |
| Styling      | TailwindCSS v4                       | Modern utility-first CSS             |
| Icons        | Lucide React                         | Icon library                         |
| TFTP Server  | github.com/pin/tftp/v3               | TFTP protocol implementation         |
| ISO Parser   | github.com/kdomanski/iso9660         | ISO filesystem reading               |
| Build Tool   | Vite                                 | Frontend bundling & dev server       |

---

## Contributing

Contributions are welcome! Areas for improvement:

- Additional OS type detection
- Support for more Linux boot methods
- UEFI Secure Boot compatibility
- macOS network boot support
- Additional i18n translations

### Development Setup

```bash
# Clone repo
git clone https://github.com/JKDx97/IBootTime.git
cd IBootTime

# Install Go dependencies
go mod download

# Install frontend dependencies
cd frontend && npm install && cd ..

# Run in dev mode (requires Wails CLI)
wails dev
```

---

## License

MIT License - See repository for details.

---

## Acknowledgments

- [iPXE](https://ipxe.org/) - The ultimate open source boot firmware
- [iVentoy](https://www.iventoy.com/) - Inspiration for proxy PXE mode
- [Wails](https://wails.io/) - Beautiful Go + React desktop apps
- [wimboot](https://ipxe.org/wimboot) - Windows Imaging Format bootloader
