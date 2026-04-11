package session

import (
	"sync"
	"time"
)

type BootState string

const (
	StateDiscovery  BootState = "discovery"
	StateTFTP       BootState = "tftp"
	StateMenu       BootState = "menu"
	StateLoading    BootState = "loading"
	StateCompleted  BootState = "completed"
	StateError      BootState = "error"
)

type ClientSession struct {
	MAC              string    `json:"mac"`
	IP               string    `json:"ip"`
	Arch             string    `json:"arch"`
	State            BootState `json:"state"`
	ISOName          string    `json:"isoName"`
	BytesTransferred int64     `json:"bytesTransferred"`
	TotalBytes       int64     `json:"totalBytes"`
	Progress         float64   `json:"progress"`
	Speed            string    `json:"speed"`
	StartedAt        time.Time `json:"startedAt"`
	LastSeen         time.Time `json:"lastSeen"`
}

type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*ClientSession
	onUpdate func(ClientSession)
}

func NewManager(onUpdate func(ClientSession)) *Manager {
	return &Manager{
		sessions: make(map[string]*ClientSession),
		onUpdate: onUpdate,
	}
}

func (m *Manager) Register(mac, ip, arch string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	s, exists := m.sessions[mac]
	if !exists {
		s = &ClientSession{
			MAC:       mac,
			IP:        ip,
			Arch:      arch,
			State:     StateDiscovery,
			StartedAt: now,
			LastSeen:  now,
		}
		m.sessions[mac] = s
	} else {
		s.IP = ip
		s.Arch = arch
		s.LastSeen = now
	}

	if m.onUpdate != nil {
		m.onUpdate(*s)
	}
}

func (m *Manager) UpdateState(mac string, state BootState) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[mac]
	if !ok {
		return
	}
	s.State = state
	s.LastSeen = time.Now()

	if m.onUpdate != nil {
		m.onUpdate(*s)
	}
}

func (m *Manager) SetISO(mac, isoName string, totalBytes int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[mac]
	if !ok {
		return
	}
	s.ISOName = isoName
	s.TotalBytes = totalBytes
	s.BytesTransferred = 0
	s.Progress = 0
	s.State = StateLoading
	s.LastSeen = time.Now()

	if m.onUpdate != nil {
		m.onUpdate(*s)
	}
}

func (m *Manager) UpdateProgress(mac string, bytesTransferred int64, speed string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[mac]
	if !ok {
		return
	}
	s.BytesTransferred = bytesTransferred
	if s.TotalBytes > 0 {
		s.Progress = float64(bytesTransferred) / float64(s.TotalBytes) * 100
	}
	s.Speed = speed
	s.LastSeen = time.Now()

	if m.onUpdate != nil {
		m.onUpdate(*s)
	}
}

func (m *Manager) Remove(mac string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, mac)
}

func (m *Manager) List() []ClientSession {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]ClientSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		result = append(result, *s)
	}
	return result
}

func (m *Manager) CleanStale(timeout time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for mac, s := range m.sessions {
		if now.Sub(s.LastSeen) > timeout {
			delete(m.sessions, mac)
		}
	}
}
