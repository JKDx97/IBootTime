# BootTime

A **Wails v2** (Go + React) PXE/Network Boot Server application - an iVentoy-style clone for network booting ISO images.

![Tech Stack](https://img.shields.io/badge/Go-1.26.1-blue) ![Wails](https://img.shields.io/badge/Wails-v2.12.0-purple) ![React](https://img.shields.io/badge/React-19-blue) ![Tailwind](https://img.shields.io/badge/TailwindCSS-v4-teal)

## Features

- **Proxy PXE Mode**: Works alongside your router, no IP conflict
- **Multi-Protocol Support**: TFTP, HTTP, DHCP (proxy mode)
- **ISO Management**: Scan, catalog, and serve ISO images
- **Real-time Client Monitoring**: Track boot progress of connected clients
- **iPXE Integration**: Serve iPXE boot scripts and binaries
- **Cross-Platform**: Built with Go and React, runs on Windows/Linux/macOS

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

## Architecture

### Pure Proxy PXE Mode

BootTime operates exclusively in **proxy PXE mode** - it works alongside your existing router/DHCP server:

1. **DISCOVER → Proxy OFFER**: Responds with yiaddr=0, siaddr=our IP, filename, option 43 PXEBS_SKIP
2. **REQUEST → Piggyback ACK**: Acknowledges client's requested IP (from router) with boot info
3. **Port 4011 → Boot ACK**: Unicast reply with boot info for clients

### Backend Services

| Service   | Port  | Purpose                          |
|-----------|-------|----------------------------------|
| DHCP      | 67/4011 | Proxy PXE boot responses       |
| TFTP      | 69      | Serve iPXE binaries            |
| HTTP      | 8080    | iPXE scripts & ISO streaming   |

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

On first run, a configuration file `iboottime.json` is created:

```json
{
  "interfaceName": "Ethernet",
  "isoDirectory": "C:\\ISOs",
  "httpPort": 8080,
  "tftpPort": 69,
  "bootProtocol": "ipxe"
}
```

- **interfaceName**: Network interface to bind services to
- **isoDirectory**: Path to your ISO files
- **httpPort**: HTTP server port for iPXE scripts and ISO streaming
- **tftpPort**: TFTP server port (requires admin/root on port 69)
- **bootProtocol**: Boot protocol (`ipxe` for standard iPXE)

---

## Workflow

1. **Configure**: Select network interface and set ISO directory
2. **Scan ISOs**: App scans and catalogs available ISOs with OS detection
3. **Start Server**: Click "Start Server" to launch DHCP/TFTP/HTTP services
4. **Boot Clients**: PXE clients on the network will receive boot info and connect
5. **Monitor**: Watch real-time client progress in the Dashboard

---

## Tech Stack

| Component    | Technology                           |
|--------------|--------------------------------------|
| Backend      | Go 1.26.1                            |
| Framework    | Wails v2.12.0                        |
| Frontend     | React 19                             |
| Styling      | TailwindCSS v4                       |
| Icons        | Lucide React                         |
| TFTP Server  | github.com/pin/tftp/v3               |
| Build Tool   | Vite                                 |

---

## License

MIT License - See repository for details.
