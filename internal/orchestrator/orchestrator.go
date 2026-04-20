package orchestrator

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"

	"IBootTime/internal/config"
	"IBootTime/internal/dhcpsrv"
	"IBootTime/internal/hidecmd"
	"IBootTime/internal/httpboot"
	"IBootTime/internal/isomgr"
	"IBootTime/internal/logger"
	"IBootTime/internal/netinfo"
	"IBootTime/internal/session"
	"IBootTime/internal/tftpsrv"
)

type ServiceStatus struct {
	DHCP            bool   `json:"dhcp"`
	TFTP            bool   `json:"tftp"`
	HTTP            bool   `json:"http"`
	Running         bool   `json:"running"`
	IP              string `json:"ip"`
	HTTPPort        int    `json:"httpPort"`
	BootProtocol    string `json:"bootProtocol"`
	StartupPhase    string `json:"startupPhase"`
	StartupProgress int    `json:"startupProgress"`
	StartupDetail   string `json:"startupDetail"`
}

type Orchestrator struct {
	mu       sync.Mutex
	running  bool
	cancel   context.CancelFunc
	cfg      *config.Config
	log      *logger.Logger
	isoMgr   *isomgr.Manager
	sessions *session.Manager

	dhcp *dhcpsrv.Server
	tftp *tftpsrv.Server
	http *httpboot.Server

	status ServiceStatus

	onStatusChange func(ServiceStatus)
}

func New(
	cfg *config.Config,
	log *logger.Logger,
	isoMgr *isomgr.Manager,
	sessions *session.Manager,
	onStatusChange func(ServiceStatus),
) *Orchestrator {
	return &Orchestrator{
		cfg:            cfg,
		log:            log,
		isoMgr:         isoMgr,
		sessions:       sessions,
		onStatusChange: onStatusChange,
	}
}

func (o *Orchestrator) StartAll() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.running {
		return fmt.Errorf("server is already running")
	}

	ifaceName := o.cfg.GetInterfaceName()
	if ifaceName == "" {
		return fmt.Errorf("no network interface selected")
	}

	serverIP, err := netinfo.GetInterfaceIP(ifaceName)
	if err != nil {
		return fmt.Errorf("getting IP for interface %s: %w", ifaceName, err)
	}

	isoDir := o.cfg.GetISODirectory()
	if isoDir == "" {
		return fmt.Errorf("no ISO directory configured")
	}

	o.isoMgr.SetDirectory(isoDir)
	if _, err := o.isoMgr.Scan(); err != nil {
		o.log.Warn("Orchestrator", "ISO scan warning: %v", err)
	}

	bootProto := o.cfg.GetBootProtocol()

	ctx, cancel := context.WithCancel(context.Background())
	o.cancel = cancel

	o.status = ServiceStatus{
		IP:           serverIP,
		HTTPPort:     o.cfg.HTTPPort,
		BootProtocol: string(bootProto),
	}

	o.log.Info("Orchestrator", "Starting services: IP=%s protocol=%s (proxy PXE)", serverIP, bootProto)

	// Phase: Firewall + SMB
	o.status.StartupPhase = "firewall"
	o.status.StartupProgress = 5
	o.status.StartupDetail = "Configurando firewall y SMB..."
	o.emitStatus()
	o.ensureFirewallRules()
	o.ensureSMBGuestAccess()

	// Phase: TFTP
	o.status.StartupPhase = "services"
	o.status.StartupProgress = 15
	o.status.StartupDetail = "Iniciando TFTP..."
	o.emitStatus()
	o.tftp = tftpsrv.New(o.cfg.TFTPPort, serverIP, o.cfg.HTTPPort, o.log, o.sessions, o.isoMgr, o.cfg)
	if err := o.tftp.Start(ctx); err != nil {
		cancel()
		o.status = ServiceStatus{}
		o.emitStatus()
		return fmt.Errorf("starting TFTP: %w", err)
	}
	o.status.TFTP = true
	o.log.Info("Orchestrator", "TFTP server started on :%d", o.cfg.TFTPPort)

	// Phase: HTTP
	o.status.StartupProgress = 30
	o.status.StartupDetail = "Iniciando HTTP..."
	o.emitStatus()
	o.http = httpboot.New(o.cfg.HTTPPort, serverIP, o.isoMgr, o.log, o.sessions, o.cfg)
	if err := o.http.Start(ctx); err != nil {
		o.tftp.Stop()
		cancel()
		o.status = ServiceStatus{}
		o.emitStatus()
		return fmt.Errorf("starting HTTP: %w", err)
	}
	o.status.HTTP = true
	o.log.Info("Orchestrator", "HTTP boot server started on :%d", o.cfg.HTTPPort)

	// Phase: DHCP
	o.status.StartupProgress = 45
	o.status.StartupDetail = "Iniciando DHCP..."
	o.emitStatus()
	gatewayIP := netinfo.GetDefaultGateway(serverIP)
	o.log.Info("Orchestrator", "Detected gateway: %s", gatewayIP)
	o.dhcp = dhcpsrv.New(o.cfg, serverIP, gatewayIP, o.log, o.sessions)
	if err := o.dhcp.Start(ctx); err != nil {
		o.http.Stop()
		o.tftp.Stop()
		cancel()
		o.status = ServiceStatus{}
		o.emitStatus()
		return fmt.Errorf("starting DHCP: %w", err)
	}
	o.status.DHCP = true
	o.log.Info("Orchestrator", "Proxy PXE DHCP started")

	o.running = true
	o.status.Running = true
	o.status.StartupPhase = "ready"
	o.status.StartupProgress = 100
	o.status.StartupDetail = ""
	o.emitStatus()
	o.log.Info("Orchestrator", "Servidor LISTO en %s [proto=%s] — clientes pueden PXE boot", serverIP, bootProto)

	// ISO prep runs in background — show as secondary progress
	o.http.SetPrepProgressCallback(func(phase string, current, total int, detail string) {
		o.mu.Lock()
		defer o.mu.Unlock()
		if !o.running {
			return
		}
		pct := 0
		if total > 0 {
			pct = (current * 100) / total
		}
		o.status.StartupPhase = "preparing"
		o.status.StartupProgress = pct
		o.status.StartupDetail = detail
		o.emitStatus()
	})

	o.http.SetPrepDoneCallback(func() {
		o.mu.Lock()
		defer o.mu.Unlock()
		if !o.running {
			return
		}
		o.status.StartupPhase = "ready"
		o.status.StartupProgress = 100
		o.status.StartupDetail = ""
		o.emitStatus()
		o.log.Info("Orchestrator", "Todas las ISOs preparadas en %s", o.status.IP)
	})

	return nil
}

func (o *Orchestrator) StopAll() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if !o.running {
		return fmt.Errorf("server is not running")
	}

	if o.cancel != nil {
		o.cancel()
	}

	if o.dhcp != nil {
		o.dhcp.Stop()
	}
	if o.http != nil {
		o.http.Stop()
	}
	if o.tftp != nil {
		o.tftp.Stop()
	}

	o.running = false
	o.status = ServiceStatus{}

	o.emitStatus()
	o.log.Info("Orchestrator", "All services stopped")

	return nil
}

func (o *Orchestrator) IsRunning() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.running
}

func (o *Orchestrator) GetStatus() ServiceStatus {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.status
}

// TriggerRemote tells the HTTP server to set the reverse-VNC trigger flag
// for the given client IP. The WinPE client will pick it up on its next poll.
func (o *Orchestrator) TriggerRemote(ip string) error {
	o.mu.Lock()
	h := o.http
	o.mu.Unlock()
	if h == nil {
		return fmt.Errorf("HTTP server is not running")
	}
	return h.TriggerRemote(ip)
}

func (o *Orchestrator) emitStatus() {
	if o.onStatusChange != nil {
		o.onStatusChange(o.status)
	}
}

// ensureSMBGuestAccess ensures that the server allows guest/anonymous SMB access.
// Modern Windows 10 (1709+) and Windows 11 disable insecure guest auth by default,
// which causes WinPE "net use" to fail even when the share has Everyone FullAccess.
// This sets the required registry keys and restarts the LanmanWorkstation service.
func (o *Orchestrator) ensureSMBGuestAccess() {
	if runtime.GOOS != "windows" {
		return
	}

	// Enable insecure guest auth on the SERVER so WinPE clients can connect
	// without requiring a valid domain/local account.
	script := strings.Join([]string{
		// Server-side: allow guest fallback for SMB shares
		`Set-ItemProperty -Path 'HKLM:\SYSTEM\CurrentControlSet\Services\LanmanServer\Parameters' -Name 'RestrictNullSessAccess' -Value 0 -Type DWord -Force -ErrorAction SilentlyContinue`,
		// Client-side (for local testing): allow insecure guest auth
		`$p = 'HKLM:\SYSTEM\CurrentControlSet\Services\LanmanWorkstation\Parameters'`,
		`if (!(Test-Path $p)) { New-Item -Path $p -Force | Out-Null }`,
		`Set-ItemProperty -Path $p -Name 'AllowInsecureGuestAuth' -Value 1 -Type DWord -Force -ErrorAction SilentlyContinue`,
		// Ensure SMB server is enabled and started
		`Set-SmbServerConfiguration -EnableSMB2Protocol $true -Force -ErrorAction SilentlyContinue`,
		// Restart LanmanServer to pick up changes
		`Restart-Service LanmanServer -Force -ErrorAction SilentlyContinue`,
	}, "; ")

	cmd := hidecmd.Command("powershell", "-NoProfile", "-Command", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		o.log.Warn("Orchestrator", "SMB guest access config had issues: %v (%s)", err, strings.TrimSpace(string(out)))
	} else {
		o.log.Info("Orchestrator", "SMB guest access configured (insecure guest auth enabled)")
	}
}

func (o *Orchestrator) ensureFirewallRules() {
	if runtime.GOOS != "windows" {
		return
	}

	type rule struct {
		name  string
		dir   string
		proto string
		port  string
	}

	rules := []rule{
		{"IBootTime DHCP-In", "in", "udp", "67,68,4011"},
		{"IBootTime DHCP-Out", "out", "udp", "67,68,4011"},
		{"IBootTime TFTP-In", "in", "udp", "69"},
		{"IBootTime TFTP-Out", "out", "udp", "69"},
		{"IBootTime TFTP-Data-In", "in", "udp", "1024-65535"},
		{"IBootTime TFTP-Data-Out", "out", "udp", "1024-65535"},
		{"IBootTime HTTP", "in", "tcp", fmt.Sprintf("%d", o.cfg.HTTPPort)},
		{"IBootTime SMB-In", "in", "tcp", "445"},
		{"IBootTime SMB-Out", "out", "tcp", "445"},
		{"IBootTime VNC-In", "in", "tcp", fmt.Sprintf("%d", o.cfg.GetWinPEVncPort())},
		// Reverse VNC listener: WinPE clients dial in to us on 5500.
		{"IBootTime VNC-Reverse-In", "in", "tcp", "5500"},
	}

	// Build a single batch script that does ALL firewall operations at once
	// instead of spawning 20+ separate netsh processes.
	var sb strings.Builder
	for _, r := range rules {
		sb.WriteString(fmt.Sprintf(
			"netsh advfirewall firewall delete rule name=\"%s\" >nul 2>&1\n"+
				"netsh advfirewall firewall add rule name=\"%s\" dir=%s action=allow protocol=%s localport=%s profile=any >nul 2>&1\n",
			r.name, r.name, r.dir, r.proto, r.port))
	}

	// Program-based rules
	exePath, err := os.Executable()
	if err == nil {
		for _, dir := range []string{"in", "out"} {
			ruleName := "IBootTime Program-" + dir
			sb.WriteString(fmt.Sprintf(
				"netsh advfirewall firewall delete rule name=\"%s\" >nul 2>&1\n"+
					"netsh advfirewall firewall add rule name=\"%s\" dir=%s action=allow program=\"%s\" enable=yes profile=any >nul 2>&1\n",
				ruleName, ruleName, dir, exePath))
		}
	}

	// Execute everything in ONE hidden cmd.exe process
	cmd := hidecmd.Command("cmd", "/C", sb.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		o.log.Warn("Orchestrator", "Firewall batch failed: %v (%s)", err, strings.TrimSpace(string(out)))
	} else {
		o.log.Info("Orchestrator", "Firewall rules configured (%d rules)", len(rules)+2)
	}
}
