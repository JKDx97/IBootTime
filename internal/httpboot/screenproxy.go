package httpboot

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type screenHub struct {
	mu            sync.RWMutex
	writeMu       sync.Mutex
	agent         *websocket.Conn
	agentIP       string
	agentLastSeen time.Time
	agentLastEmit time.Time
	viewers       map[*websocket.Conn]struct{}
}

type screenRegistry struct {
	mu   sync.RWMutex
	hubs map[string]*screenHub
}

var remoteScreens = &screenRegistry{
	hubs: make(map[string]*screenHub),
}

var screenUpgrader = websocket.Upgrader{
	ReadBufferSize:  256 * 1024,
	WriteBufferSize: 256 * 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (s *Server) handleScreenRemote(w http.ResponseWriter, r *http.Request) {
	conn, err := screenUpgrader.Upgrade(w, r, nil)
	if err != nil {
		s.log.Warn("Remote", "WebSocket upgrade failed: %v", err)
		return
	}
	conn.SetReadLimit(16 << 20)

	clientID := remoteClientID(r)
	hub := remoteScreens.get(clientID)

	if r.URL.Query().Get("role") == "viewer" {
		s.log.Info("Remote", "viewer connected from %s for client %s", r.RemoteAddr, clientID)
		hub.addViewer(conn)
		done := startWebSocketHeartbeat(conn, 20*time.Second, 60*time.Second)
		s.forwardViewerInput(hub, conn)
		close(done)
		hub.removeViewer(conn)
		conn.Close()
		s.log.Info("Remote", "viewer disconnected from %s for client %s", r.RemoteAddr, clientID)
		return
	}

	agentIP := remoteAddrIP(r.RemoteAddr)
	if clientID == "" {
		clientID = agentIP
		hub = remoteScreens.get(clientID)
	}
	s.log.Info("Remote", "screen agent connected from %s for client %s", r.RemoteAddr, clientID)
	if s.sessions != nil && agentIP != "" {
		s.sessions.SetRemoteReady(agentIP, 0, "", "")
	}
	hub.setAgent(conn, agentIP)
	done := startWebSocketHeartbeat(conn, 20*time.Second, 60*time.Second)
	s.forwardAgentFrames(hub, conn)
	close(done)
	hub.clearAgent(conn)
	conn.Close()
	s.log.Info("Remote", "screen agent disconnected from %s for client %s", r.RemoteAddr, clientID)
}

func (s *Server) forwardAgentFrames(hub *screenHub, conn *websocket.Conn) {
	for {
		mt, payload, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if mt != websocket.BinaryMessage {
			continue
		}
		if s.sessions != nil && hub.touchAgent() {
			if ip := hub.agentRemoteIP(); ip != "" {
				s.sessions.SetRemoteReady(ip, 0, "", "")
			}
		}
		hub.broadcast(payload)
	}
}

func (s *Server) forwardViewerInput(hub *screenHub, conn *websocket.Conn) {
	for {
		mt, payload, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if mt != websocket.BinaryMessage {
			continue
		}
		hub.sendToAgent(payload)
	}
}

func (r *screenRegistry) get(clientID string) *screenHub {
	if clientID == "" {
		clientID = "default"
	}
	r.mu.RLock()
	hub := r.hubs[clientID]
	r.mu.RUnlock()
	if hub != nil {
		return hub
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if hub = r.hubs[clientID]; hub != nil {
		return hub
	}
	hub = &screenHub{viewers: make(map[*websocket.Conn]struct{})}
	r.hubs[clientID] = hub
	return hub
}

func (h *screenHub) setAgent(conn *websocket.Conn, ip string) {
	h.mu.Lock()
	if h.agent != nil && h.agent != conn {
		h.agent.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "replaced"), time.Now().Add(time.Second))
		h.agent.Close()
	}
	h.agent = conn
	h.agentIP = ip
	h.agentLastSeen = time.Now()
	h.agentLastEmit = h.agentLastSeen
	h.mu.Unlock()
}

func (h *screenHub) clearAgent(conn *websocket.Conn) {
	h.mu.Lock()
	if h.agent == conn {
		h.agent = nil
		h.agentIP = ""
	}
	h.mu.Unlock()
}

func (h *screenHub) touchAgent() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.agent != nil {
		now := time.Now()
		h.agentLastSeen = now
		if now.Sub(h.agentLastEmit) >= 5*time.Second {
			h.agentLastEmit = now
			return true
		}
	}
	return false
}

func (h *screenHub) agentRemoteIP() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.agentIP
}

func (h *screenHub) addViewer(conn *websocket.Conn) {
	h.mu.Lock()
	h.viewers[conn] = struct{}{}
	h.mu.Unlock()
}

func startWebSocketHeartbeat(conn *websocket.Conn, interval, timeout time.Duration) chan struct{} {
	done := make(chan struct{})
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(timeout))
	})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				_ = conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(5*time.Second))
			}
		}
	}()
	return done
}

func remoteAddrIP(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err == nil {
		return host
	}
	return addr
}

func remoteClientID(r *http.Request) string {
	if id := strings.TrimSpace(r.URL.Query().Get("client_id")); id != "" {
		return id
	}
	prefix := "/ws/remote/"
	if strings.HasPrefix(r.URL.Path, prefix) {
		id := strings.Trim(strings.TrimPrefix(r.URL.Path, prefix), "/")
		if id != "" {
			return id
		}
	}
	if r.URL.Query().Get("role") != "viewer" {
		return remoteAddrIP(r.RemoteAddr)
	}
	return ""
}

func (h *screenHub) removeViewer(conn *websocket.Conn) {
	h.mu.Lock()
	delete(h.viewers, conn)
	h.mu.Unlock()
}

func (h *screenHub) broadcast(payload []byte) {
	h.mu.RLock()
	viewers := make([]*websocket.Conn, 0, len(h.viewers))
	for conn := range h.viewers {
		viewers = append(viewers, conn)
	}
	h.mu.RUnlock()

	for _, conn := range viewers {
		conn.SetWriteDeadline(time.Now().Add(250 * time.Millisecond))
		if err := conn.WriteMessage(websocket.BinaryMessage, payload); err != nil {
			h.removeViewer(conn)
			conn.Close()
		}
	}
}

func (h *screenHub) sendToAgent(payload []byte) {
	h.mu.RLock()
	agent := h.agent
	h.mu.RUnlock()
	if agent == nil {
		return
	}
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	agent.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err := agent.WriteMessage(websocket.BinaryMessage, payload); err != nil {
		h.clearAgent(agent)
		agent.Close()
	}
}
