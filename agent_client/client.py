"""
IBootTime Remote Agent Client
Runs on each remote PC after Windows installation.
Registers with the server, polls for tasks, and executes them.

Usage:
    python client.py --server 192.168.1.100:9090
"""

import argparse
import os
import platform
import socket
import subprocess
import sys
import time
import uuid
from pathlib import Path

import requests

# ── Configuration ──────────────────────────────────────────────────────────

POLL_INTERVAL = 5  # seconds between polls
HEARTBEAT_INTERVAL = 10


def get_local_ip() -> str:
    """Best-effort local IP detection."""
    try:
        s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        s.connect(("8.8.8.8", 80))
        ip = s.getsockname()[0]
        s.close()
        return ip
    except Exception:
        return "127.0.0.1"


def get_mac() -> str:
    """Get a MAC address string."""
    mac_int = uuid.getnode()
    return ":".join(f"{(mac_int >> (8 * i)) & 0xFF:02x}" for i in reversed(range(6)))


# ── Task executors ─────────────────────────────────────────────────────────

def execute_ping(params: dict) -> tuple[bool, str]:
    """Simple connectivity check — return system info."""
    info = (
        f"hostname={socket.gethostname()}, "
        f"ip={get_local_ip()}, "
        f"os={platform.system()} {platform.release()}, "
        f"time={time.strftime('%Y-%m-%d %H:%M:%S')}"
    )
    return True, info


def execute_create_test_file(params: dict) -> tuple[bool, str]:
    """Create a text file at the given path with given content."""
    file_path = params.get("path", r"C:\Temp\vengo_desde_el_servidor.txt")
    content = params.get("content", "vengo desde el servidor")
    try:
        directory = Path(file_path).parent
        directory.mkdir(parents=True, exist_ok=True)
        Path(file_path).write_text(content, encoding="utf-8")
        return True, f"File created: {file_path}"
    except Exception as e:
        return False, f"Error creating file: {e}"


def execute_open_notepad(params: dict) -> tuple[bool, str]:
    """Open notepad.exe with the specified file."""
    file_path = params.get("path", r"C:\Temp\vengo_desde_el_servidor.txt")
    try:
        # Ensure file exists first
        if not Path(file_path).exists():
            Path(file_path).parent.mkdir(parents=True, exist_ok=True)
            Path(file_path).write_text("vengo desde el servidor", encoding="utf-8")
        subprocess.Popen(["notepad.exe", file_path])
        return True, f"Notepad opened with {file_path}"
    except Exception as e:
        return False, f"Error opening notepad: {e}"


EXECUTORS = {
    "ping": execute_ping,
    "create_test_file": execute_create_test_file,
    "open_notepad": execute_open_notepad,
}


# ── Main agent loop ───────────────────────────────────────────────────────

class AgentClient:
    def __init__(self, server_url: str):
        self.server_url = server_url.rstrip("/")
        self.client_id: str | None = None
        self.session = requests.Session()
        self.session.timeout = 10

    def register(self):
        """Register this machine with the agent server."""
        payload = {
            "hostname": socket.gethostname(),
            "ip": get_local_ip(),
            "os_version": f"{platform.system()} {platform.version()}",
            "mac": get_mac(),
        }
        print(f"[Agent] Registering with {self.server_url} ...")
        print(f"[Agent]   hostname={payload['hostname']}")
        print(f"[Agent]   ip={payload['ip']}")
        print(f"[Agent]   mac={payload['mac']}")

        resp = self.session.post(f"{self.server_url}/agent/register", json=payload)
        resp.raise_for_status()
        data = resp.json()
        self.client_id = data["client_id"]
        print(f"[Agent] Registered! client_id={self.client_id}")

    def heartbeat(self):
        """Send heartbeat to stay alive."""
        try:
            self.session.post(
                f"{self.server_url}/agent/heartbeat",
                json={"client_id": self.client_id},
            )
        except Exception:
            pass  # non-critical

    def poll_and_execute(self):
        """Check for pending tasks and execute them."""
        try:
            resp = self.session.get(f"{self.server_url}/agent/tasks/{self.client_id}")
            resp.raise_for_status()
            data = resp.json()
        except Exception as e:
            print(f"[Agent] Poll error: {e}")
            return

        tasks = data.get("tasks", [])
        for task in tasks:
            task_id = task["task_id"]
            task_type = task["task_type"]
            params = task.get("params", {})

            print(f"[Agent] Executing task {task_id}: {task_type}")
            executor = EXECUTORS.get(task_type)
            if executor:
                success, output = executor(params)
            else:
                success, output = False, f"Unknown task type: {task_type}"

            print(f"[Agent]   result: success={success}, output={output}")

            # Report result
            try:
                self.session.post(
                    f"{self.server_url}/agent/task-result",
                    json={
                        "client_id": self.client_id,
                        "task_id": task_id,
                        "success": success,
                        "output": output,
                    },
                )
            except Exception as e:
                print(f"[Agent] Failed to report result: {e}")

    def run(self):
        """Main loop: register, then poll forever."""
        # Keep trying to register until server is reachable
        while True:
            try:
                self.register()
                break
            except Exception as e:
                print(f"[Agent] Registration failed ({e}), retrying in 5s...")
                time.sleep(5)

        last_heartbeat = 0
        print(f"[Agent] Running. Polling every {POLL_INTERVAL}s ...")
        while True:
            try:
                now = time.time()

                # Heartbeat
                if now - last_heartbeat >= HEARTBEAT_INTERVAL:
                    self.heartbeat()
                    last_heartbeat = now

                # Poll for tasks
                self.poll_and_execute()

            except KeyboardInterrupt:
                print("[Agent] Shutting down.")
                break
            except Exception as e:
                print(f"[Agent] Loop error: {e}")

            time.sleep(POLL_INTERVAL)


# ── Entry point ────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(description="IBootTime Remote Agent Client")
    parser.add_argument(
        "--server",
        required=True,
        help="Server address, e.g. 192.168.1.100:9090",
    )
    args = parser.parse_args()

    server_url = args.server
    if not server_url.startswith("http"):
        server_url = f"http://{server_url}"

    agent = AgentClient(server_url)
    agent.run()


if __name__ == "__main__":
    main()
