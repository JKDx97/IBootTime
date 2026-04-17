package httpboot

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// vncTriggerStore tracks per-client "please connect reverse VNC now" flags.
// The server UI sets the flag when the operator presses "Conectar"; the WinPE
// client polls /api/winpe/vnc-check and, when it sees the flag, dials out to
// the reverse VNC listener. The flag is cleared as soon as it is read so the
// client only connects once per press.
type vncTriggerStore struct {
	mu      sync.Mutex
	pending map[string]bool // clientIP -> pending
}

var vncTriggers = &vncTriggerStore{pending: make(map[string]bool)}

func (v *vncTriggerStore) Set(ip string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.pending[ip] = true
}

// TakeAndClear returns whether a trigger was pending and clears it.
func (v *vncTriggerStore) TakeAndClear(ip string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.pending[ip] {
		delete(v.pending, ip)
		return true
	}
	return false
}

// TriggerRemote marks the given client IP as "should dial in now". Called
// from the server-side UI (via Wails) when the operator presses "Conectar".
func (s *Server) TriggerRemote(ip string) error {
	if ip == "" {
		return fmt.Errorf("ip is required")
	}
	sess := s.sessions.GetByIP(ip)
	if sess == nil {
		return fmt.Errorf("no client session for IP %s", ip)
	}
	if !sess.RemoteAvailable {
		return fmt.Errorf("client %s has not reported VNC readiness yet", ip)
	}
	// If a reverse connection already exists (old auto-connect WIM or
	// previous trigger), keep it — handleVNCProxy will use it directly.
	// Only set the trigger flag so new-style polling clients also connect.
	vncTriggers.Set(ip)
	if s.reverseVNC != nil && s.reverseVNC.HasConn(ip) {
		s.log.Info("VNC", "Trigger set for %s — reverse connection already available", ip)
	} else {
		s.log.Info("VNC", "Trigger set for %s — waiting for client to dial in", ip)
	}
	return nil
}

// handleVNCCheck is polled by WinPE clients to learn when they should dial
// out to the reverse VNC listener. Returns JSON {"connect": bool}.
// GET /api/winpe/vnc-check?ip=<clientIP>
func (s *Server) handleVNCCheck(w http.ResponseWriter, r *http.Request) {
	ip := r.URL.Query().Get("ip")
	if ip == "" {
		ip = strings.Split(r.RemoteAddr, ":")[0]
	}
	should := vncTriggers.TakeAndClear(ip)
	if should {
		s.log.Info("VNC", "vnc-check: granting reverse connect to %s", ip)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"connect": should})
}

// handleRemoteTrigger allows HTTP clients (tests, CLI) to trigger a reverse
// connect. The primary path is Wails -> App.TriggerRemote, but exposing this
// endpoint keeps the server self-testable.
// POST /api/remote/trigger  Body: {"ip": "..."}
func (s *Server) handleRemoteTrigger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		IP string `json:"ip"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := s.TriggerRemote(body.IP); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
