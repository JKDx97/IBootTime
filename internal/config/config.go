package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type BootProtocol string

const (
	BootProtocolIPXE    BootProtocol = "ipxe"
	BootProtocolGRUB    BootProtocol = "grub"
	BootProtocolUndionly BootProtocol = "undionly"
)

type Config struct {
	mu            sync.RWMutex
	InterfaceName string       `json:"interfaceName"`
	ISODirectory  string       `json:"isoDirectory"`
	HTTPPort      int          `json:"httpPort"`
	TFTPPort      int          `json:"tftpPort"`
	BootProtocol  BootProtocol `json:"bootProtocol"`
	WinPERemote   bool         `json:"winpeRemote"`
	WinPEVncPort  int          `json:"winpeVncPort"`
	DisabledISOs  []string            `json:"disabledISOs,omitempty"`
	ISOUnattend   map[string]string   `json:"isoUnattend,omitempty"`
	configPath    string
}

func DefaultConfig() *Config {
	return &Config{
		ISODirectory: "",
		HTTPPort:     8080,
		TFTPPort:     69,
		BootProtocol: BootProtocolIPXE,
		WinPERemote:  true,
		WinPEVncPort: 5900,
	}
}

func Load(path string) (*Config, error) {
	cfg := DefaultConfig()
	cfg.configPath = path

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, cfg.Save()
		}
		return nil, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) Save() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	dir := filepath.Dir(c.configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.configPath, data, 0644)
}

func (c *Config) Update(fn func(c *Config)) error {
	c.mu.Lock()
	fn(c)
	c.mu.Unlock()
	return c.Save()
}

func (c *Config) GetISODirectory() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ISODirectory
}

func (c *Config) GetInterfaceName() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.InterfaceName
}

func (c *Config) GetBootProtocol() BootProtocol {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.BootProtocol == "" {
		return BootProtocolIPXE
	}
	return c.BootProtocol
}

func (c *Config) GetWinPERemote() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.WinPERemote
}

func (c *Config) GetWinPEVncPort() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.WinPEVncPort == 0 {
		return 5900
	}
	return c.WinPEVncPort
}

func (c *Config) GetDisabledISOs() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]string, len(c.DisabledISOs))
	copy(result, c.DisabledISOs)
	return result
}

func (c *Config) IsISODisabled(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, n := range c.DisabledISOs {
		if n == name {
			return true
		}
	}
	return false
}

func (c *Config) GetISOUnattend(name string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.ISOUnattend == nil {
		return ""
	}
	return c.ISOUnattend[name]
}

func (c *Config) GetAllISOUnattend() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[string]string)
	for k, v := range c.ISOUnattend {
		result[k] = v
	}
	return result
}

