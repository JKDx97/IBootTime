package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

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

	// Initialize ISO manager
	a.isoMgr = isomgr.NewManager(a.cfg.GetISODirectory())

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

	a.log.Info("App", "IBootTime initialized")
}

func (a *App) shutdown(ctx context.Context) {
	if a.orchestrator != nil && a.orchestrator.IsRunning() {
		a.orchestrator.StopAll()
	}
}

// ==================== Server Control ====================

func (a *App) StartServer() error {
	a.log.Info("App", "Starting all services...")
	return a.orchestrator.StartAll()
}

func (a *App) StopServer() error {
	a.log.Info("App", "Stopping all services...")
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
	wailsRuntime.EventsEmit(a.ctx, "iso:list-changed", a.isoMgr.List())
	return nil
}

// ==================== Clients ====================

func (a *App) GetConnectedClients() []session.ClientSession {
	return a.sessions.List()
}

// ==================== Logs ====================

func (a *App) GetRecentLogs(count int) []logger.LogEntry {
	return a.log.GetEntries(count)
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


