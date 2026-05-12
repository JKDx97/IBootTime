package httpboot

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"IBootTime/internal/logger"
	"IBootTime/internal/session"
)

// Default TCP port where IBootTime listens for reverse VNC connections from
// WinPE clients (UltraVNC `-connect host:port`). 5500 is the de-facto standard.
const DefaultReverseVNCPort = 5500

// ReverseVNCListener accepts incoming VNC connections initiated by WinPE
// clients via `winvnc.exe -connect server:5500`. Each accepted connection is
// stored keyed by the client's IP so handleVNCProxy can bridge it to noVNC.
//
// Having the client dial out to us (instead of us dialing into the client)
// bypasses client-side firewalls/NAT — the common failure mode in WinPE.
type ReverseVNCListener struct {
	port     int
	log      *logger.Logger
	sessions *session.Manager

	listener net.Listener

	mu    sync.Mutex
	conns map[string]net.Conn // clientIP -> latest live conn
	// Per-IP signal channels so handleVNCProxy can wait for a conn to arrive.
	waiters map[string]chan struct{}
}

func NewReverseVNCListener(port int, log *logger.Logger, sessions *session.Manager) *ReverseVNCListener {
	return &ReverseVNCListener{
		port:     port,
		log:      log,
		sessions: sessions,
		conns:    make(map[string]net.Conn),
		waiters:  make(map[string]chan struct{}),
	}
}

// Start binds the listener and begins accepting connections in a goroutine.
// It returns immediately.
func (r *ReverseVNCListener) Start(ctx context.Context) error {
	addr := fmt.Sprintf(":%d", r.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	r.listener = ln
	r.log.Info("VNC", "Reverse VNC listener started on %s (clients dial in here)", addr)

	go func() {
		<-ctx.Done()
		r.Stop()
	}()

	go r.acceptLoop()
	return nil
}

// Stop closes the listener and any stored connections.
func (r *ReverseVNCListener) Stop() {
	if r.listener != nil {
		r.listener.Close()
		r.listener = nil
	}
	r.mu.Lock()
	for ip, c := range r.conns {
		c.Close()
		delete(r.conns, ip)
	}
	for ip, ch := range r.waiters {
		close(ch)
		delete(r.waiters, ip)
	}
	r.mu.Unlock()
}

func (r *ReverseVNCListener) acceptLoop() {
	for {
		conn, err := r.listener.Accept()
		if err != nil {
			// Listener was closed — exit cleanly.
			if strings.Contains(err.Error(), "use of closed") {
				return
			}
			// Transient error — log and keep looping (don't exit).
			r.log.Warn("VNC", "Reverse accept error (retrying): %v", err)
			continue
		}
		go r.handleNewConn(conn)
	}
}

func (r *ReverseVNCListener) handleNewConn(conn net.Conn) {
	ip := remoteIP(conn)
	r.log.Info("VNC", "Reverse VNC connection received from %s", ip)

	r.mu.Lock()
	// Close any previous conn for the same IP (stale auto-reconnect).
	if old, ok := r.conns[ip]; ok {
		old.Close()
	}
	r.conns[ip] = conn
	// Wake any waiter blocked in GetConn.
	if ch, ok := r.waiters[ip]; ok {
		close(ch)
		delete(r.waiters, ip)
	}
	r.mu.Unlock()

	// Mark this IP as remote-available in the session manager so the UI
	// shows a "Connect" button even if the HTTP beacon hasn't arrived yet.
	if r.sessions != nil {
		// Port 0 means "reverse only" — handleVNCProxy will ignore it and
		// use the stored conn instead of dialing.
		r.sessions.SetRemoteReady(ip, 0, "")
	}
}

// handleNewConn2 re-stores a connection that was taken out of the pool.
// Used by handleVNCProxy when it needs to put it back after waiting.
func (r *ReverseVNCListener) handleNewConn2(ip string, conn net.Conn) {
	r.mu.Lock()
	r.conns[ip] = conn
	r.mu.Unlock()
}

// TakeConn removes and returns the stored connection for the given IP, if any.
// Returns nil if no conn is currently available.
func (r *ReverseVNCListener) TakeConn(ip string) net.Conn {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.conns[ip]
	if !ok {
		return nil
	}
	delete(r.conns, ip)
	return c
}

// WaitForConn returns an existing conn or blocks up to `timeout` waiting for
// one to arrive. Returns nil if the timeout elapses.
func (r *ReverseVNCListener) WaitForConn(ip string, timeout time.Duration) net.Conn {
	// Fast path.
	if c := r.TakeConn(ip); c != nil {
		return c
	}

	// Install a waiter.
	r.mu.Lock()
	ch, ok := r.waiters[ip]
	if !ok {
		ch = make(chan struct{})
		r.waiters[ip] = ch
	}
	r.mu.Unlock()

	select {
	case <-ch:
		return r.TakeConn(ip)
	case <-time.After(timeout):
		r.mu.Lock()
		// Only remove the waiter if it's still ours (hasn't been closed).
		if cur, ok := r.waiters[ip]; ok && cur == ch {
			delete(r.waiters, ip)
		}
		r.mu.Unlock()
		return nil
	}
}

// DropConn closes and removes any stored connection for the IP.
// Used to flush stale connections before waiting for a fresh one.
func (r *ReverseVNCListener) DropConn(ip string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.conns[ip]; ok {
		c.Close()
		delete(r.conns, ip)
		r.log.Info("VNC", "Dropped stale reverse conn for %s", ip)
	}
}

// HasConn reports whether a reverse conn is currently stored for the IP.
func (r *ReverseVNCListener) HasConn(ip string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.conns[ip]
	return ok
}

func remoteIP(c net.Conn) string {
	addr := c.RemoteAddr().String()
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}
