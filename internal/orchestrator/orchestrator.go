package orchestrator

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"

	"IBootTime/internal/config"
	"IBootTime/internal/dhcpsrv"
	"IBootTime/internal/httpboot"
	"IBootTime/internal/isomgr"
	"IBootTime/internal/logger"
	"IBootTime/internal/netinfo"
	"IBootTime/internal/session"
	"IBootTime/internal/tftpsrv"
)

type ServiceStatus struct {
	DHCP         bool   `json:"dhcp"`
	TFTP         bool   `json:"tftp"`
	HTTP         bool   `json:"http"`
	Running      bool   `json:"running"`
	IP           string `json:"ip"`
	BootProtocol string `json:"bootProtocol"`
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
		BootProtocol: string(bootProto),
	}

	o.log.Info("Orchestrator", "Starting services: IP=%s protocol=%s (proxy PXE)", serverIP, bootProto)

	// Auto-configure Windows Firewall for PXE boot ports
	o.ensureFirewallRules()

	// Start TFTP (now receives config for boot protocol and network mode)
	o.tftp = tftpsrv.New(o.cfg.TFTPPort, serverIP, o.cfg.HTTPPort, o.log, o.sessions, o.isoMgr, o.cfg)
	if err := o.tftp.Start(ctx); err != nil {
		cancel()
		return fmt.Errorf("starting TFTP: %w", err)
	}
	o.status.TFTP = true
	o.log.Info("Orchestrator", "TFTP server started on :%d", o.cfg.TFTPPort)

	// Start HTTP (now receives config for network mode)
	o.http = httpboot.New(o.cfg.HTTPPort, serverIP, o.isoMgr, o.log, o.sessions, o.cfg)
	if err := o.http.Start(ctx); err != nil {
		o.tftp.Stop()
		cancel()
		return fmt.Errorf("starting HTTP: %w", err)
	}
	o.status.HTTP = true
	o.log.Info("Orchestrator", "HTTP boot server started on :%d", o.cfg.HTTPPort)

	// Start DHCP (already has config reference)
	o.dhcp = dhcpsrv.New(o.cfg, serverIP, o.log, o.sessions)
	if err := o.dhcp.Start(ctx); err != nil {
		o.http.Stop()
		o.tftp.Stop()
		cancel()
		return fmt.Errorf("starting DHCP: %w", err)
	}
	o.status.DHCP = true
	o.log.Info("Orchestrator", "Proxy PXE DHCP started")

	o.running = true
	o.status.Running = true

	o.emitStatus()
	o.log.Info("Orchestrator", "All services started on %s [proto=%s, mode=proxy PXE]", serverIP, bootProto)

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

func (o *Orchestrator) emitStatus() {
	if o.onStatusChange != nil {
		o.onStatusChange(o.status)
	}
}

func (o *Orchestrator) ensureFirewallRules() {
	if runtime.GOOS != "windows" {
		return
	}

	rules := []struct {
		name  string
		dir   string
		proto string
		port  string
	}{
		{"IBootTime DHCP-In", "in", "udp", "67,68,4011"},
		{"IBootTime DHCP-Out", "out", "udp", "67,68,4011"},
		{"IBootTime TFTP-In", "in", "udp", "69"},
		{"IBootTime TFTP-Out", "out", "udp", "69"},
		{"IBootTime TFTP-Data-In", "in", "udp", "1024-65535"},
		{"IBootTime TFTP-Data-Out", "out", "udp", "1024-65535"},
		{"IBootTime HTTP", "in", "tcp", fmt.Sprintf("%d", o.cfg.HTTPPort)},
		{"IBootTime SMB-In", "in", "tcp", "445"},
		{"IBootTime SMB-Out", "out", "tcp", "445"},
	}

	for _, r := range rules {
		// Delete existing rule first (idempotent)
		exec.Command("netsh", "advfirewall", "firewall", "delete", "rule", "name="+r.name).Run()

		// Add rule
		err := exec.Command("netsh", "advfirewall", "firewall", "add", "rule",
			"name="+r.name, "dir="+r.dir, "action=allow",
			"protocol="+r.proto, "localport="+r.port, "profile=any",
		).Run()
		if err != nil {
			o.log.Warn("Orchestrator", "Firewall rule '%s' failed: %v (run as admin?)", r.name, err)
		} else {
			o.log.Info("Orchestrator", "Firewall rule added: %s (%s %s/%s)", r.name, r.dir, r.proto, r.port)
		}
	}

	// Also add PROGRAM-based rules — allows ALL traffic from/to IBootTime.exe.
	// This catches edge cases where port rules don't cover broadcast/multicast.
	exePath, err := os.Executable()
	if err == nil {
		for _, dir := range []string{"in", "out"} {
			ruleName := "IBootTime Program-" + dir
			exec.Command("netsh", "advfirewall", "firewall", "delete", "rule", "name="+ruleName).Run()
			err := exec.Command("netsh", "advfirewall", "firewall", "add", "rule",
				"name="+ruleName, "dir="+dir, "action=allow",
				"program="+exePath, "enable=yes", "profile=any",
			).Run()
			if err != nil {
				o.log.Warn("Orchestrator", "Program firewall rule '%s' failed: %v", ruleName, err)
			} else {
				o.log.Info("Orchestrator", "Program firewall rule added: %s (%s)", ruleName, exePath)
			}
		}
	}
}
