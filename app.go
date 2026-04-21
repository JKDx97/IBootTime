package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"IBootTime/internal/agentproxy"
	"IBootTime/internal/config"
	"IBootTime/internal/isomgr"
	"IBootTime/internal/logger"
	"IBootTime/internal/netinfo"
	"IBootTime/internal/orchestrator"
	"IBootTime/internal/session"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx          context.Context
	cfg          *config.Config
	log          *logger.Logger
	isoMgr       *isomgr.Manager
	sessions     *session.Manager
	orchestrator *orchestrator.Orchestrator
	agentProxy   *agentproxy.Proxy
}

func NewApp() *App {
	// Determine config path next to executable
	exe, _ := os.Executable()
	configDir := filepath.Dir(exe)
	configPath := filepath.Join(configDir, "iboottime.json")

	cfg, err := config.Load(configPath)
	if err != nil {
		cfg = config.DefaultConfig()
	}

	a := &App{
		cfg: cfg,
	}

	return a
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// Initialize logger with event bridge to frontend
	a.log = logger.New(1000, func(entry logger.LogEntry) {
		wailsRuntime.EventsEmit(a.ctx, "server:log", entry)
	})

	// Initialize session manager with event bridge
	a.sessions = session.NewManager(func(s session.ClientSession) {
		wailsRuntime.EventsEmit(a.ctx, "client:updated", s)
	})

	// Initialize ISO manager with persisted disabled list and unattend paths
	a.isoMgr = isomgr.NewManager(a.cfg.GetISODirectory())
	a.isoMgr.SetDisabledNames(a.cfg.GetDisabledISOs())
	a.isoMgr.SetUnattendPaths(a.cfg.GetAllISOUnattend())

	// Initialize orchestrator
	a.orchestrator = orchestrator.New(
		a.cfg,
		a.log,
		a.isoMgr,
		a.sessions,
		func(status orchestrator.ServiceStatus) {
			wailsRuntime.EventsEmit(a.ctx, "server:status-changed", status)
		},
	)

	// Start stale session cleanup goroutine
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.sessions.CleanStale(5 * time.Minute)
			}
		}
	}()

	// Initialize agent proxy to Python server
	a.agentProxy = agentproxy.New("http://127.0.0.1:9090")

	a.log.Info("App", "IBootTime initialized")
}

func (a *App) shutdown(ctx context.Context) {
	if a.agentProxy != nil {
		a.agentProxy.Stop()
	}
	if a.orchestrator != nil && a.orchestrator.IsRunning() {
		a.orchestrator.StopAll()
	}
}

// ==================== Server Control ====================

func (a *App) StartServer() error {
	a.log.Info("App", "Starting all services...")

	// Start Python agent server first
	if err := a.agentProxy.Start(); err != nil {
		a.log.Warn("App", "Agent server failed to start: %v (continuing without it)", err)
	} else {
		a.log.Info("App", "Python agent server started on :9090")
	}

	return a.orchestrator.StartAll()
}

func (a *App) StopServer() error {
	a.log.Info("App", "Stopping all services...")

	// Stop Python agent server
	a.agentProxy.Stop()
	a.log.Info("App", "Python agent server stopped")

	return a.orchestrator.StopAll()
}

func (a *App) GetServerStatus() orchestrator.ServiceStatus {
	return a.orchestrator.GetStatus()
}

func (a *App) IsServerRunning() bool {
	return a.orchestrator.IsRunning()
}

// ==================== Network ====================

func (a *App) GetNetworkInterfaces() ([]netinfo.NetInterface, error) {
	return netinfo.ListInterfaces()
}

func (a *App) SetNetworkInterface(name string) error {
	return a.cfg.Update(func(c *config.Config) {
		c.InterfaceName = name
	})
}

func (a *App) GetSelectedInterface() string {
	return a.cfg.GetInterfaceName()
}

// ==================== ISO Management ====================

func (a *App) GetISODirectory() string {
	return a.cfg.GetISODirectory()
}

func (a *App) SetISODirectory(dir string) error {
	a.isoMgr.SetDirectory(dir)
	return a.cfg.Update(func(c *config.Config) {
		c.ISODirectory = dir
	})
}

func (a *App) BrowseISODirectory() (string, error) {
	dir, err := wailsRuntime.OpenDirectoryDialog(a.ctx, wailsRuntime.OpenDialogOptions{
		Title: "Select ISO Directory",
	})
	if err != nil {
		return "", err
	}
	if dir != "" {
		a.isoMgr.SetDirectory(dir)
		a.cfg.Update(func(c *config.Config) {
			c.ISODirectory = dir
		})
	}
	return dir, nil
}

func (a *App) ScanISOs() ([]isomgr.ISOInfo, error) {
	isos, err := a.isoMgr.Scan()
	if err != nil {
		a.log.Error("App", "ISO scan failed: %v", err)
		return nil, err
	}
	a.log.Info("App", "Found %d ISO files", len(isos))
	wailsRuntime.EventsEmit(a.ctx, "iso:list-changed", isos)
	return isos, nil
}

func (a *App) GetISOList() []isomgr.ISOInfo {
	return a.isoMgr.List()
}

func (a *App) ToggleISO(name string, enabled bool) error {
	err := a.isoMgr.Toggle(name, enabled)
	if err != nil {
		return err
	}
	// Persist disabled ISOs to config
	disabled := a.isoMgr.ListDisabledNames()
	a.cfg.Update(func(c *config.Config) {
		c.DisabledISOs = disabled
	})
	wailsRuntime.EventsEmit(a.ctx, "iso:list-changed", a.isoMgr.List())
	return nil
}

// ==================== Clients ====================

func (a *App) GetConnectedClients() []session.ClientSession {
	return a.sessions.List()
}

func (a *App) AssignISO(mac, isoName string) bool {
	a.log.Info("App", "Assigning ISO '%s' to client %s", isoName, mac)
	return a.sessions.AssignISO(mac, isoName)
}

// ==================== Logs ====================

func (a *App) GetRecentLogs(count int) []logger.LogEntry {
	return a.log.GetEntries(count)
}

// ==================== WinPE Remote Control ====================

func (a *App) GetWinPERemote() bool {
	return a.cfg.GetWinPERemote()
}

func (a *App) SetWinPERemote(enabled bool) error {
	return a.cfg.Update(func(c *config.Config) {
		c.WinPERemote = enabled
	})
}

// TriggerRemote tells the given WinPE client to initiate a reverse VNC
// connection. The client polls /api/winpe/vnc-check and connects only when
// this trigger is set.
func (a *App) TriggerRemote(ip string) error {
	return a.orchestrator.TriggerRemote(ip)
}

// ==================== Boot Protocol ====================

func (a *App) GetBootProtocol() string {
	return string(a.cfg.GetBootProtocol())
}

func (a *App) SetBootProtocol(protocol string) error {
	p := config.BootProtocol(protocol)
	switch p {
	case config.BootProtocolIPXE, config.BootProtocolGRUB, config.BootProtocolUndionly:
		// valid
	default:
		return fmt.Errorf("invalid boot protocol: %s", protocol)
	}
	return a.cfg.Update(func(c *config.Config) {
		c.BootProtocol = p
	})
}

// ==================== Autounattend ====================

func (a *App) SetISOUnattend(isoName, filePath string) error {
	if _, err := os.Stat(filePath); err != nil {
		return fmt.Errorf("file not found: %s", filePath)
	}
	a.isoMgr.SetUnattend(isoName, filePath)
	a.cfg.Update(func(c *config.Config) {
		if c.ISOUnattend == nil {
			c.ISOUnattend = make(map[string]string)
		}
		c.ISOUnattend[isoName] = filePath
	})
	a.log.Info("App", "Set autounattend.xml for %s: %s", isoName, filePath)
	wailsRuntime.EventsEmit(a.ctx, "iso:list-changed", a.isoMgr.List())
	return nil
}

func (a *App) ClearISOUnattend(isoName string) error {
	a.isoMgr.SetUnattend(isoName, "")
	a.cfg.Update(func(c *config.Config) {
		if c.ISOUnattend != nil {
			delete(c.ISOUnattend, isoName)
		}
	})
	a.log.Info("App", "Cleared autounattend.xml for %s", isoName)
	wailsRuntime.EventsEmit(a.ctx, "iso:list-changed", a.isoMgr.List())
	return nil
}

func (a *App) BrowseISOUnattend(isoName string) (string, error) {
	filePath, err := wailsRuntime.OpenFileDialog(a.ctx, wailsRuntime.OpenDialogOptions{
		Title: "Select autounattend.xml for " + isoName,
		Filters: []wailsRuntime.FileFilter{
			{DisplayName: "XML Files (*.xml)", Pattern: "*.xml"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		},
	})
	if err != nil {
		return "", err
	}
	if filePath != "" {
		if err := a.SetISOUnattend(isoName, filePath); err != nil {
			return "", err
		}
	}
	return filePath, nil
}

// ==================== Remote Agent (Python proxy) ====================

func (a *App) AgentListClients() ([]agentproxy.RemoteClient, error) {
	return a.agentProxy.ListClients()
}

func (a *App) AgentPing(clientID string) (*agentproxy.TaskResponse, error) {
	return a.agentProxy.PingClient(clientID)
}

func (a *App) AgentCreateTestFile(clientID string) (*agentproxy.TaskResponse, error) {
	return a.agentProxy.CreateTestFile(clientID)
}

func (a *App) AgentOpenNotepad(clientID string) (*agentproxy.TaskResponse, error) {
	return a.agentProxy.OpenNotepad(clientID)
}

func (a *App) AgentGetTasks(clientID string) ([]agentproxy.RemoteTask, error) {
	return a.agentProxy.GetClientTasks(clientID)
}
