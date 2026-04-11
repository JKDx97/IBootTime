package isomgr

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/kdomanski/iso9660"
)

type Manager struct {
	mu       sync.RWMutex
	isoDir   string
	isos     map[string]*ISOInfo
	enabled  map[string]bool
}

func NewManager(isoDir string) *Manager {
	return &Manager{
		isoDir:  isoDir,
		isos:    make(map[string]*ISOInfo),
		enabled: make(map[string]bool),
	}
}

func (m *Manager) SetDirectory(dir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isoDir = dir
}

func (m *Manager) Scan() ([]ISOInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isoDir == "" {
		return nil, fmt.Errorf("ISO directory not configured")
	}

	entries, err := os.ReadDir(m.isoDir)
	if err != nil {
		return nil, fmt.Errorf("reading ISO directory: %w", err)
	}

	newISOs := make(map[string]*ISOInfo)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".iso") {
			continue
		}

		fullPath := filepath.Join(m.isoDir, name)
		info, err := entry.Info()
		if err != nil {
			continue
		}

		iso := &ISOInfo{
			Name:   name,
			Path:   fullPath,
			Size:   info.Size(),
			SizeHR: humanizeBytes(info.Size()),
			Arch:   "x64",
		}

		iso.OSType = detectOSType(fullPath)

		if prev, ok := m.enabled[name]; ok {
			iso.Enabled = prev
		} else {
			iso.Enabled = true
		}

		newISOs[name] = iso
	}

	m.isos = newISOs
	for name, iso := range m.isos {
		m.enabled[name] = iso.Enabled
	}

	return m.listLocked(), nil
}

func (m *Manager) List() []ISOInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.listLocked()
}

func (m *Manager) listLocked() []ISOInfo {
	result := make([]ISOInfo, 0, len(m.isos))
	for _, iso := range m.isos {
		result = append(result, *iso)
	}
	return result
}

func (m *Manager) ListEnabled() []ISOInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]ISOInfo, 0)
	for _, iso := range m.isos {
		if iso.Enabled {
			result = append(result, *iso)
		}
	}
	return result
}

func (m *Manager) Toggle(name string, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	iso, ok := m.isos[name]
	if !ok {
		return fmt.Errorf("ISO %q not found", name)
	}
	iso.Enabled = enabled
	m.enabled[name] = enabled
	return nil
}

func (m *Manager) GetByName(name string) (*ISOInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	iso, ok := m.isos[name]
	if !ok {
		return nil, fmt.Errorf("ISO %q not found", name)
	}
	cp := *iso
	return &cp, nil
}

func detectOSType(isoPath string) OSType {
	f, err := os.Open(isoPath)
	if err != nil {
		return detectFromName(isoPath)
	}
	defer f.Close()

	img, err := iso9660.OpenImage(f)
	if err != nil {
		return detectFromName(isoPath)
	}

	root, err := img.RootDir()
	if err != nil {
		return detectFromName(isoPath)
	}

	hasAutorun := false
	hasSources := false
	hasCasper := false
	hasIsolinux := false

	children, err := root.GetChildren()
	if err != nil {
		return detectFromName(isoPath)
	}

	for _, child := range children {
		name := strings.ToLower(child.Name())
		switch {
		case name == "autorun.inf":
			hasAutorun = true
		case name == "sources":
			hasSources = true
		case name == "casper":
			hasCasper = true
		case name == "isolinux" || name == "syslinux":
			hasIsolinux = true
		}
	}

	nameLower := strings.ToLower(filepath.Base(isoPath))

	if hasSources && hasAutorun {
		if strings.Contains(nameLower, "pe") || strings.Contains(nameLower, "winpe") ||
			strings.Contains(nameLower, "xpe") || strings.Contains(nameLower, "winbuilder") {
			return OSTypeWinPE
		}
		return OSTypeWindows
	}

	if hasCasper || hasIsolinux {
		return OSTypeLinux
	}

	if strings.Contains(nameLower, "clonezilla") || strings.Contains(nameLower, "gparted") ||
		strings.Contains(nameLower, "hirens") || strings.Contains(nameLower, "memtest") {
		return OSTypeUtility
	}

	return detectFromName(isoPath)
}

func detectFromName(isoPath string) OSType {
	name := strings.ToLower(filepath.Base(isoPath))
	switch {
	case strings.Contains(name, "win"):
		if strings.Contains(name, "pe") || strings.Contains(name, "xpe") {
			return OSTypeWinPE
		}
		return OSTypeWindows
	case strings.Contains(name, "ubuntu") || strings.Contains(name, "debian") ||
		strings.Contains(name, "fedora") || strings.Contains(name, "centos") ||
		strings.Contains(name, "linux") || strings.Contains(name, "mint") ||
		strings.Contains(name, "arch") || strings.Contains(name, "manjaro"):
		return OSTypeLinux
	case strings.Contains(name, "clonezilla") || strings.Contains(name, "gparted") ||
		strings.Contains(name, "hirens") || strings.Contains(name, "memtest"):
		return OSTypeUtility
	default:
		return OSTypeUnknown
	}
}

func humanizeBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
