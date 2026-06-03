package agentproxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"IBootTime/internal/hidecmd"
)

// Proxy forwards requests from Wails frontend to the Python agent server.

type Proxy struct {
	baseURL   string
	client    *http.Client
	cmd       *exec.Cmd
	mu        sync.Mutex
	scriptDir string
}

func New(pythonServerURL string) *Proxy {
	// Resolve agent_server/main.py relative to the executable
	exe, _ := os.Executable()
	exeDir := filepath.Dir(exe)
	wd, _ := os.Getwd()
	scriptDir := filepath.Join(exeDir, "agent_server")

	// Dev mode: exe is in build/bin/, project root is two levels up
	if _, err := os.Stat(filepath.Join(scriptDir, "main.py")); err != nil {
		projectRoot := filepath.Join(exeDir, "..", "..")
		if _, err2 := os.Stat(filepath.Join(projectRoot, "wails.json")); err2 == nil {
			scriptDir = filepath.Join(projectRoot, "agent_server")
		} else if _, err3 := os.Stat(filepath.Join(wd, "agent_server", "main.py")); err3 == nil {
			scriptDir = filepath.Join(wd, "agent_server")
		}
	}

	return &Proxy{
		baseURL:   pythonServerURL,
		scriptDir: scriptDir,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Start launches the Python agent server as a child process.
func (p *Proxy) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cmd != nil {
		return nil // already running
	}

	if p.isHTTPReady() {
		return nil // an agent server is already listening
	}

	script := filepath.Join(p.scriptDir, "main.py")
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("agent_server/main.py not found at %s", script)
	}

	pythonBin := p.resolvePython()

	p.cmd = hidecmd.Command(pythonBin, script)
	p.cmd.Dir = p.scriptDir
	p.cmd.Stdout = os.Stdout
	p.cmd.Stderr = os.Stderr

	if err := p.cmd.Start(); err != nil {
		p.cmd = nil
		return fmt.Errorf("failed to start python agent server: %w", err)
	}

	// Wait for the server to be ready (up to 10s)
	for i := 0; i < 20; i++ {
		time.Sleep(500 * time.Millisecond)
		if p.isHTTPReady() {
			return nil
		}
	}

	return fmt.Errorf("python agent server started but not responding on %s", p.baseURL)
}

func (p *Proxy) isHTTPReady() bool {
	resp, err := p.client.Get(p.baseURL + "/api/clients")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 500
}

func (p *Proxy) resolvePython() string {
	exe, _ := os.Executable()
	exeDir := filepath.Dir(exe)
	wd, _ := os.Getwd()

	candidates := []string{
		filepath.Join(exeDir, "tools", "python-embed", "python.exe"),
		filepath.Join(exeDir, "..", "..", "tools", "python-embed", "python.exe"),
		filepath.Join(wd, "tools", "python-embed", "python.exe"),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	if runtime.GOOS != "windows" {
		return "python3"
	}
	return "python"
}

// Stop kills the Python agent server process.
func (p *Proxy) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cmd == nil || p.cmd.Process == nil {
		return
	}

	if runtime.GOOS == "windows" {
		// On Windows, kill the process tree
		kill := hidecmd.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", p.cmd.Process.Pid))
		kill.Run()
	} else {
		p.cmd.Process.Signal(os.Interrupt)
		time.Sleep(2 * time.Second)
		p.cmd.Process.Kill()
	}

	p.cmd.Wait()
	p.cmd = nil
}

// IsRunning returns true if the Python server process is alive.
func (p *Proxy) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cmd != nil && p.cmd.Process != nil
}

// RemoteClient mirrors the Python model.
type RemoteClient struct {
	ClientID    string  `json:"client_id"`
	Hostname    string  `json:"hostname"`
	IP          string  `json:"ip"`
	OSVersion   string  `json:"os_version"`
	MAC         string  `json:"mac"`
	Status      string  `json:"status"`
	RegisterdAt float64 `json:"registered_at"`
	LastSeen    float64 `json:"last_seen"`
	Hardware    any     `json:"hardware"`
	Diagnostics any     `json:"diagnostics"`
}

type HardwareResponse struct {
	Hardware    any `json:"hardware"`
	Diagnostics any `json:"diagnostics"`
}

type RemoteTask struct {
	TaskID       string  `json:"task_id"`
	TaskType     string  `json:"task_type"`
	Params       any     `json:"params"`
	Status       string  `json:"status"`
	ResultOutput string  `json:"result_output"`
	CreatedAt    float64 `json:"created_at"`
	CompletedAt  float64 `json:"completed_at"`
}

type TaskResponse struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
}

// ListClients returns all registered remote agents.
func (p *Proxy) ListClients() ([]RemoteClient, error) {
	resp, err := p.client.Get(p.baseURL + "/api/clients")
	if err != nil {
		return nil, fmt.Errorf("agent server unreachable: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var result struct {
		Clients []RemoteClient `json:"clients"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result.Clients, nil
}

// PingClient queues a connectivity check.
func (p *Proxy) PingClient(clientID string) (*TaskResponse, error) {
	return p.postAction(clientID, "ping")
}

// CreateTestFile queues the "create test file" task.
func (p *Proxy) CreateTestFile(clientID string) (*TaskResponse, error) {
	return p.postAction(clientID, "create-test-file")
}

// OpenNotepad queues the "open notepad" task.
func (p *Proxy) OpenNotepad(clientID string) (*TaskResponse, error) {
	return p.postAction(clientID, "open-notepad")
}

// GetClientTasks returns task history for a client.
func (p *Proxy) GetClientTasks(clientID string) ([]RemoteTask, error) {
	resp, err := p.client.Get(fmt.Sprintf("%s/api/clients/%s/tasks", p.baseURL, clientID))
	if err != nil {
		return nil, fmt.Errorf("agent server unreachable: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var result struct {
		Tasks []RemoteTask `json:"tasks"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result.Tasks, nil
}

// GetSystemInfo queues a full hardware + diagnostics collection task.
func (p *Proxy) GetSystemInfo(clientID string) (*TaskResponse, error) {
	return p.postAction(clientID, "system-info")
}

// GetHardware returns stored hardware/diagnostics data for a client.
func (p *Proxy) GetHardware(clientID string) (*HardwareResponse, error) {
	resp, err := p.client.Get(fmt.Sprintf("%s/api/clients/%s/hardware", p.baseURL, clientID))
	if err != nil {
		return nil, fmt.Errorf("agent server unreachable: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var result HardwareResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UpdateHardware tells the server to refresh stored hw data from the latest completed task.
func (p *Proxy) UpdateHardware(clientID string) error {
	resp, err := p.client.Post(fmt.Sprintf("%s/api/clients/%s/update-hardware", p.baseURL, clientID), "application/json", nil)
	if err != nil {
		return fmt.Errorf("agent server unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("error %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func (p *Proxy) postAction(clientID, action string) (*TaskResponse, error) {
	url := fmt.Sprintf("%s/api/clients/%s/%s", p.baseURL, clientID, action)
	resp, err := p.client.Post(url, "application/json", nil)
	if err != nil {
		return nil, fmt.Errorf("agent server unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("error %d: %s", resp.StatusCode, string(body))
	}

	body, _ := io.ReadAll(resp.Body)
	var tr TaskResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, err
	}
	return &tr, nil
}
