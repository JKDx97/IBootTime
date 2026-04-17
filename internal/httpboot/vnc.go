package httpboot

import (
	"crypto/des"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// vncPasswordStore holds per-session VNC passwords (keyed by client IP).
type vncPasswordStore struct {
	mu    sync.RWMutex
	store map[string]string // ip -> plaintext password
}

var vncPasswords = &vncPasswordStore{store: make(map[string]string)}

func (v *vncPasswordStore) Set(ip, password string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.store[ip] = password
}

func (v *vncPasswordStore) Get(ip string) string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.store[ip]
}

// fixedVNCPassword is a shared constant password. UltraVNC refuses to start
// without a password, so we set one internally and auto-inject it into the
// noVNC viewer URL so the end user never has to type it. This makes the
// remote-control experience effectively password-less.
const fixedVNCPassword = "iboottime"

// generateVNCPassword returns the fixed shared password. The name is kept
// for backward compatibility with the rest of the module.
func generateVNCPassword() string {
	return fixedVNCPassword
}

// silence unused-import warnings when the random generator is not used.
var _ = rand.Reader
var _ = big.NewInt

// encryptVNCPassword encrypts a VNC password using the standard DES method.
// VNC uses a fixed key to encrypt the password for storage.
// The password is truncated/padded to 8 bytes, each byte's bits are reversed,
// then DES-encrypted with a null plaintext block.
func encryptVNCPassword(password string) (string, error) {
	// Pad or truncate to 8 bytes
	key := make([]byte, 8)
	copy(key, []byte(password))

	// Reverse bits of each byte (VNC DES quirk)
	for i := range key {
		key[i] = reverseBits(key[i])
	}

	block, err := des.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("DES cipher: %w", err)
	}

	// Encrypt 8 null bytes
	plaintext := make([]byte, 8)
	ciphertext := make([]byte, 8)
	block.Encrypt(ciphertext, plaintext)

	return strings.ToUpper(hex.EncodeToString(ciphertext)), nil
}

func reverseBits(b byte) byte {
	var result byte
	for i := 0; i < 8; i++ {
		result = (result << 1) | (b & 1)
		b >>= 1
	}
	return result
}

// buildUltraVNCIni generates a minimal ultravnc.ini with the given encrypted password.
func buildUltraVNCIni(encryptedHex string, port int) string {
	return fmt.Sprintf("[ultravnc]\r\n"+
		"passwd=%s\r\n"+
		"passwd2=%s\r\n"+
		"PortNumber=%d\r\n"+
		"HTTPConnect=0\r\n"+
		"AutoPortSelect=0\r\n"+
		"InputsEnabled=1\r\n"+
		"AllowLoopback=1\r\n"+
		"LoopbackOnly=0\r\n"+
		"ConnectPriority=0\r\n"+
		"DisableTrayIcon=1\r\n"+
		"MSLogonRequired=0\r\n"+
		"NewMSLogon=0\r\n"+
		"RemoveWallpaper=0\r\n"+
		"DebugMode=0\r\n"+
		"[admin]\r\n"+
		"UseRegistry=0\r\n"+
		// Disable the "Password required" policy so no auth dialog appears.
		"AuthRequired=0\r\n"+
		"AuthHosts=\r\n"+
		"QuerySetting=0\r\n"+
		"QueryAccept=2\r\n"+
		"QueryIfNoLogon=0\r\n"+
		"QueryTimeout=0\r\n"+
		"AllowShutdown=1\r\n"+
		"AllowProperties=1\r\n"+
		"AllowEditClients=1\r\n"+
		"FileTransferEnabled=1\r\n"+
		"RemoveWallpaper=0\r\n",
		encryptedHex, encryptedHex, port)
}

// --- WebSocket VNC Proxy ---

var wsUpgrader = websocket.Upgrader{
	CheckOrigin:  func(r *http.Request) bool { return true },
	Subprotocols: []string{"binary"},
}

// handleVNCProxy upgrades HTTP to WebSocket and proxies to a VNC server.
// URL: /ws/vnc?host=<clientIP>&port=<vncPort>&mode=<direct|reverse|auto>
//
// - reverse: use a connection previously dialed in by the client to our
//   reverse listener (port 5500). Preferred because it bypasses client firewall.
// - direct: dial the client on TCP host:port (legacy).
// - auto (default): try reverse first, fall back to direct if unavailable.
func (s *Server) handleVNCProxy(w http.ResponseWriter, r *http.Request) {
	clientHost := r.URL.Query().Get("host")
	clientPort := r.URL.Query().Get("port")
	mode := strings.ToLower(r.URL.Query().Get("mode"))
	if mode == "" {
		mode = "auto"
	}

	if clientHost == "" {
		http.Error(w, "missing host parameter", http.StatusBadRequest)
		return
	}

	// Validate that this client is known
	sess := s.sessions.GetByIP(clientHost)
	if sess == nil || !sess.RemoteAvailable {
		http.Error(w, "client not available for remote", http.StatusForbidden)
		return
	}

	var vncConn net.Conn
	var target string

	// --- Try reverse mode first ---
	if mode == "reverse" || mode == "auto" {
		if s.reverseVNC != nil {
			// If a conn is already waiting, take it immediately.
			// Otherwise wait briefly for UltraVNC -autoreconnect to dial in.
			waitFor := 2 * time.Second
			if mode == "reverse" {
				waitFor = 30 * time.Second
			}
			if c := s.reverseVNC.WaitForConn(clientHost, waitFor); c != nil {
				vncConn = c
				target = "reverse:" + clientHost
				s.log.Info("VNC", "Using reverse connection from %s", clientHost)
			}
		}
	}

	// --- Fall back to direct dial ---
	if vncConn == nil {
		if mode == "reverse" {
			http.Error(w, "no reverse VNC connection available from client", http.StatusBadGateway)
			return
		}
		if clientPort == "" {
			clientPort = "5900"
		}
		target = net.JoinHostPort(clientHost, clientPort)
		s.log.Info("VNC", "WebSocket proxy connecting to %s (direct)", target)

		c, err := net.DialTimeout("tcp", target, 10*time.Second)
		if err != nil {
			s.log.Error("VNC", "Cannot connect to %s: %v", target, err)
			http.Error(w, "cannot reach VNC server on client", http.StatusBadGateway)
			return
		}
		vncConn = c
	}

	// Upgrade to WebSocket
	ws, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		vncConn.Close()
		s.log.Error("VNC", "WebSocket upgrade failed: %v", err)
		return
	}

	s.log.Info("VNC", "Proxy established: WS <-> %s", target)

	// Bidirectional proxy
	var wg sync.WaitGroup
	wg.Add(2)

	// VNC -> WebSocket
	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := vncConn.Read(buf)
			if n > 0 {
				if wErr := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); wErr != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
		ws.Close()
	}()

	// WebSocket -> VNC
	go func() {
		defer wg.Done()
		for {
			_, msg, err := ws.ReadMessage()
			if err != nil {
				break
			}
			if _, wErr := vncConn.Write(msg); wErr != nil {
				break
			}
		}
		vncConn.Close()
	}()

	wg.Wait()
	s.log.Info("VNC", "Proxy closed for %s", target)
}

// handleRemoteBeacon is called by WinPE clients to report VNC readiness.
// POST /api/winpe/remote-ready  Body: {"ip": "...", "port": 5900, "password": "..."}
func (s *Server) handleRemoteBeacon(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		IP       string `json:"ip"`
		Port     int    `json:"port"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if body.IP == "" {
		// Fallback: use remote address
		body.IP = strings.Split(r.RemoteAddr, ":")[0]
	}
	if body.Port == 0 {
		body.Port = 5900
	}

	s.log.Info("VNC", "Remote beacon from %s (port=%d)", body.IP, body.Port)
	s.sessions.SetRemoteReady(body.IP, body.Port, body.Password)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleVNCConfig is called by WinPE to get its VNC password (per-session).
// GET /api/winpe/vnc-config  -> {"password": "abc123", "encryptedHex": "AABB...", "port": 5900}
func (s *Server) handleVNCConfig(w http.ResponseWriter, r *http.Request) {
	clientIP := strings.Split(r.RemoteAddr, ":")[0]
	vncPort := s.cfg.GetWinPEVncPort()

	// Check if password already generated for this IP
	pw := vncPasswords.Get(clientIP)
	if pw == "" {
		pw = generateVNCPassword()
		vncPasswords.Set(clientIP, pw)
	}

	encHex, err := encryptVNCPassword(pw)
	if err != nil {
		s.log.Error("VNC", "Password encryption failed: %v", err)
		http.Error(w, "encryption error", http.StatusInternalServerError)
		return
	}

	s.log.Info("VNC", "VNC config for %s: port=%d password=%s", clientIP, vncPort, pw)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"password":     pw,
		"encryptedHex": encHex,
		"port":         vncPort,
		"iniContent":   buildUltraVNCIni(encHex, vncPort),
	})
}

// handleVNCIni serves the raw ultravnc.ini content for curl-based WinPE clients.
// GET /api/winpe/vnc-ini -> raw INI text
func (s *Server) handleVNCIni(w http.ResponseWriter, r *http.Request) {
	clientIP := strings.Split(r.RemoteAddr, ":")[0]
	s.log.Info("VNC", "INI request from %s", clientIP)
	vncPort := s.cfg.GetWinPEVncPort()

	// Check if password already generated for this IP
	pw := vncPasswords.Get(clientIP)
	if pw == "" {
		pw = generateVNCPassword()
		vncPasswords.Set(clientIP, pw)
	}

	encHex, err := encryptVNCPassword(pw)
	if err != nil {
		s.log.Error("VNC", "Password encryption failed: %v", err)
		http.Error(w, "encryption error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, buildUltraVNCIni(encHex, vncPort))
}

// handleVNCPassword serves ONLY the plain-text VNC password for a client.
// GET /api/winpe/vnc-password -> raw password string (no JSON, no newlines)
func (s *Server) handleVNCPassword(w http.ResponseWriter, r *http.Request) {
	clientIP := strings.Split(r.RemoteAddr, ":")[0]
	s.log.Info("VNC", "Password request from %s", clientIP)

	pw := vncPasswords.Get(clientIP)
	if pw == "" {
		pw = generateVNCPassword()
		vncPasswords.Set(clientIP, pw)
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, pw)
}

// handleNoVNC serves the noVNC HTML viewer.
// GET /novnc?host=<clientIP>&port=<vncPort>&password=<pw>&mode=<reverse|direct|auto>
func (s *Server) handleNoVNC(w http.ResponseWriter, r *http.Request) {
	clientHost := r.URL.Query().Get("host")
	clientPort := r.URL.Query().Get("port")
	password := r.URL.Query().Get("password")
	mode := r.URL.Query().Get("mode")

	if clientHost == "" {
		http.Error(w, "missing host parameter", http.StatusBadRequest)
		return
	}
	if clientPort == "" {
		clientPort = "5900"
	}
	if mode == "" {
		mode = "reverse"
	}
	// Always auto-fill the shared password so the end user never has to type it.
	if password == "" {
		password = fixedVNCPassword
	}

	wsURL := fmt.Sprintf("ws://%s:%d/ws/vnc?host=%s&port=%s&mode=%s",
		s.serverIP, s.port, clientHost, clientPort, mode)

	html := buildNoVNCPage(wsURL, clientHost, password)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, html)
}

// buildNoVNCPage returns a self-contained HTML page that loads noVNC from CDN.
func buildNoVNCPage(wsURL, clientHost, password string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>IBootTime Remote - %s</title>
  <style>
    * { margin: 0; padding: 0; box-sizing: border-box; }
    body { background: #0f172a; color: #e2e8f0; font-family: system-ui, sans-serif; overflow: hidden; }
    #top-bar {
      display: flex; align-items: center; gap: 12px;
      padding: 8px 16px; background: #1e293b; border-bottom: 1px solid #334155;
    }
    #top-bar h1 { font-size: 14px; font-weight: 600; }
    #top-bar .info { font-size: 12px; color: #94a3b8; }
    #top-bar .status { font-size: 11px; padding: 2px 8px; border-radius: 9999px; }
    .connected { background: #065f4620; color: #34d399; border: 1px solid #34d39940; }
    .disconnected { background: #7f1d1d20; color: #f87171; border: 1px solid #f8717140; }
    .connecting { background: #78350f20; color: #fbbf24; border: 1px solid #fbbf2440; }
    #screen { width: 100vw; height: calc(100vh - 41px); }
    #noVNC_canvas { width: 100%%; height: 100%%; }
    #fallback { display: none; padding: 40px; text-align: center; }
    #fallback h2 { margin-bottom: 16px; }
    #fallback code { background: #1e293b; padding: 8px 16px; border-radius: 8px; display: inline-block; margin: 8px 0; }
  </style>
</head>
<body>
  <div id="top-bar">
    <h1>IBootTime Remote</h1>
    <span class="info">%s</span>
    <span id="status" class="status connecting">Connecting...</span>
    <span style="flex:1"></span>
  </div>
  <div id="screen">
    <div id="fallback">
      <h2>noVNC Loading...</h2>
      <p>If the viewer doesn't load, open this URL manually in your browser:</p>
      <code>%s</code>
      <p style="margin-top:20px;color:#94a3b8">Make sure you have internet access to load noVNC from CDN, or install noVNC locally.</p>
    </div>
  </div>
  <script type="module">
    const statusEl = document.getElementById('status');
    const screenEl = document.getElementById('screen');
    const fallbackEl = document.getElementById('fallback');

    function setStatus(text, cls) {
      statusEl.textContent = text;
      statusEl.className = 'status ' + cls;
    }

    // Timeout: si en 15s no carga la libreria, mostrar error
    const timeout = setTimeout(() => {
      fallbackEl.style.display = 'block';
      setStatus('Timeout cargando noVNC', 'disconnected');
    }, 15000);

    try {
      setStatus('Cargando noVNC...', 'connecting');
      const { default: RFB } = await import('/novnc-static/core/rfb.js');
      clearTimeout(timeout);

      const url = %q;
      const password = %q;
      const MAX_RETRIES = 3;
      let attempt = 0;

      function connectVNC() {
        attempt++;
        setStatus('Esperando conexion del cliente... (intento ' + attempt + '/' + MAX_RETRIES + ')', 'connecting');

        // Clear previous canvas content
        while (screenEl.firstChild && screenEl.firstChild !== fallbackEl) {
          screenEl.removeChild(screenEl.firstChild);
        }

        const rfb = new RFB(screenEl, url, {
          credentials: { password: password },
          scaleViewport: true,
          resizeSession: true,
        });

        rfb.addEventListener('connect', () => {
          attempt = 0;
          setStatus('Conectado', 'connected');
        });
        rfb.addEventListener('disconnect', (e) => {
          if (attempt < MAX_RETRIES) {
            setStatus('Reintentando en 5s... (' + attempt + '/' + MAX_RETRIES + ')', 'connecting');
            setTimeout(connectVNC, 5000);
          } else {
            setStatus('No se pudo conectar al cliente', 'disconnected');
          }
        });
        rfb.addEventListener('credentialsrequired', () => {
          rfb.sendCredentials({ password: password });
        });
      }

      connectVNC();
    } catch (e) {
      clearTimeout(timeout);
      fallbackEl.style.display = 'block';
      setStatus('Error: ' + e.message, 'disconnected');
      console.error('noVNC error:', e);
    }
  </script>
</body>
</html>`, clientHost, clientHost, wsURL, wsURL, password)
}
