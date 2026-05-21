"""
IBootTime Remote Agent Client
Runs on each remote PC after Windows installation.
Registers with the server, polls for tasks, and executes them.

Usage:
    python client.py --server 192.168.1.100:9090
"""

import argparse
import json
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


# ── Hardware & Diagnostics collectors ──────────────────────────────────────

def _ps(cmd: str) -> str:
    """Run a PowerShell command and return stdout."""
    try:
        r = subprocess.run(
            ["powershell", "-NoProfile", "-Command", cmd],
            capture_output=True, text=True, timeout=30,
        )
        return r.stdout.strip()
    except Exception:
        return ""


def _safe_json(raw: str):
    """Parse JSON or return None."""
    if not raw:
        return None
    try:
        return json.loads(raw)
    except (json.JSONDecodeError, ValueError):
        return None


def collect_hardware() -> dict:
    """Collect full hardware specs via WMI / PowerShell."""
    info: dict = {}

    # CPU
    cpu_raw = _ps(
        "Get-CimInstance Win32_Processor | Select-Object Name,NumberOfCores,"
        "NumberOfLogicalProcessors,MaxClockSpeed | ConvertTo-Json"
    )
    cpu = _safe_json(cpu_raw)
    if isinstance(cpu, list):
        cpu = cpu[0]
    info["cpu"] = cpu or {}

    # RAM
    ram_raw = _ps(
        "Get-CimInstance Win32_PhysicalMemory | Select-Object "
        "Capacity,Speed,Manufacturer,PartNumber | ConvertTo-Json"
    )
    ram_data = _safe_json(ram_raw)
    if isinstance(ram_data, dict):
        ram_data = [ram_data]
    if not ram_data:
        ram_data = []
    total_bytes = sum(int(m.get("Capacity", 0)) for m in ram_data)
    info["ram"] = {
        "total_gb": round(total_bytes / (1024 ** 3), 1),
        "modules": ram_data,
    }

    # Serial Number
    sn = _ps("(Get-CimInstance Win32_BIOS).SerialNumber")
    info["serial_number"] = sn.strip() if sn else "N/A"

    # System model & manufacturer
    model_raw = _ps(
        "Get-CimInstance Win32_ComputerSystem | "
        "Select-Object Manufacturer,Model,SystemFamily | ConvertTo-Json"
    )
    info["system"] = _safe_json(model_raw) or {}

    # Disk space
    disk_raw = _ps(
        'Get-CimInstance Win32_LogicalDisk -Filter "DriveType=3" | '
        "Select-Object DeviceID,Size,FreeSpace | ConvertTo-Json"
    )
    disk_data = _safe_json(disk_raw)
    if isinstance(disk_data, dict):
        disk_data = [disk_data]
    if not disk_data:
        disk_data = []
    info["disks"] = [
        {
            "drive": d.get("DeviceID", "?"),
            "total_gb": round(int(d.get("Size", 0)) / (1024 ** 3), 1),
            "free_gb": round(int(d.get("FreeSpace", 0)) / (1024 ** 3), 1),
        }
        for d in disk_data
    ]

    return info


def collect_diagnostics() -> dict:
    """Collect live diagnostics: disk SMART, battery, temperature."""
    diag: dict = {}

    # Disk SMART health
    smart_raw = _ps(
        "Get-CimInstance -Namespace root\\wmi "
        "-ClassName MSStorageDriver_FailurePredictStatus -ErrorAction SilentlyContinue | "
        "Select-Object InstanceName,PredictFailure,Reason | ConvertTo-Json"
    )
    smart = _safe_json(smart_raw)
    if isinstance(smart, dict):
        smart = [smart]
    if smart:
        diag["disk_smart"] = [
            {
                "disk": s.get("InstanceName", "?").split("\\")[-1][:40],
                "predict_failure": bool(s.get("PredictFailure", False)),
                "status": "Warning" if s.get("PredictFailure") else "OK",
            }
            for s in smart
        ]
    else:
        diag["disk_smart"] = []

    # Battery
    batt_raw = _ps(
        "Get-CimInstance Win32_Battery -ErrorAction SilentlyContinue | "
        "Select-Object EstimatedChargeRemaining,BatteryStatus,"
        "DesignCapacity,FullChargeCapacity,Status | ConvertTo-Json"
    )
    batt = _safe_json(batt_raw)
    if isinstance(batt, list):
        batt = batt[0] if batt else None
    if batt:
        design_cap = int(batt.get("DesignCapacity", 0) or 0)
        full_cap = int(batt.get("FullChargeCapacity", 0) or 0)
        health_pct = round((full_cap / design_cap) * 100, 1) if design_cap > 0 else 0
        status_map = {
            1: "Discharging", 2: "AC Power", 3: "Fully Charged",
            4: "Low", 5: "Critical", 6: "Charging",
            7: "Charging+High", 8: "Charging+Low", 9: "Charging+Critical",
            10: "Undefined", 11: "Partially Charged",
        }
        diag["battery"] = {
            "charge_percent": int(batt.get("EstimatedChargeRemaining", 0) or 0),
            "health_percent": health_pct,
            "status": status_map.get(int(batt.get("BatteryStatus", 0) or 0), "Unknown"),
            "design_capacity": design_cap,
            "full_charge_capacity": full_cap,
        }
    else:
        diag["battery"] = None  # desktop, no battery

    # Temperature (try MSAcpi first, then OpenHardwareMonitor)
    temp_raw = _ps(
        "try { "
        "  $t = Get-CimInstance -Namespace root/OpenHardwareMonitor "
        "    -ClassName Sensor -ErrorAction Stop | "
        "    Where-Object { $_.SensorType -eq 'Temperature' } | "
        "    Select-Object Name,Value | ConvertTo-Json; $t "
        "} catch { "
        "  try { "
        "    Get-CimInstance MSAcpi_ThermalZoneTemperature "
        "      -Namespace root/wmi -ErrorAction Stop | "
        "      Select-Object InstanceName,"
        "        @{N='TempC';E={[math]::Round(($_.CurrentTemperature - 2732) / 10, 1)}} | "
        "      ConvertTo-Json "
        "  } catch { '[]' } "
        "}"
    )
    temps = _safe_json(temp_raw)
    if isinstance(temps, dict):
        temps = [temps]
    if temps:
        diag["temperature"] = [
            {
                "sensor": t.get("Name") or t.get("InstanceName", "Unknown"),
                "value_c": round(float(t.get("Value") or t.get("TempC", 0)), 1),
            }
            for t in temps
        ]
    else:
        diag["temperature"] = []

    return diag


def execute_system_info(params: dict) -> tuple[bool, str]:
    """Collect full system specs + diagnostics and return as JSON."""
    try:
        data = {
            "hardware": collect_hardware(),
            "diagnostics": collect_diagnostics(),
        }
        return True, json.dumps(data, ensure_ascii=False)
    except Exception as e:
        return False, f"Error collecting system info: {e}"


EXECUTORS = {
    "ping": execute_ping,
    "create_test_file": execute_create_test_file,
    "open_notepad": execute_open_notepad,
    "system_info": execute_system_info,
}


# ── Main agent loop ───────────────────────────────────────────────────────

class AgentClient:
    def __init__(self, server_url: str):
        self.server_url = server_url.rstrip("/")
        self.client_id: str | None = None
        self.session = requests.Session()
        self.session.timeout = 10

    def register(self):
        """Register this machine with the agent server, including hardware specs."""
        print("[Agent] Collecting hardware info...")
        hw = {}
        diag = {}
        try:
            hw = collect_hardware()
            diag = collect_diagnostics()
        except Exception as e:
            print(f"[Agent] Hardware collection error: {e}")

        payload = {
            "hostname": socket.gethostname(),
            "ip": get_local_ip(),
            "os_version": f"{platform.system()} {platform.version()}",
            "mac": get_mac(),
            "hardware": hw,
            "diagnostics": diag,
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
            if resp.status_code in (404, 422):
                raise ConnectionError("Server lost our registration (got %d)" % resp.status_code)
            resp.raise_for_status()
            data = resp.json()
        except (requests.ConnectionError, ConnectionError) as e:
            raise  # propagate to main loop for re-registration
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

    def _do_register(self):
        """Keep trying to register until server is reachable."""
        while True:
            try:
                self.register()
                return
            except Exception as e:
                print(f"[Agent] Registration failed ({e}), retrying in 5s...")
                time.sleep(5)

    def run(self):
        """Main loop: register, then poll forever. Auto-reconnects on failure."""
        self._do_register()

        last_heartbeat = 0
        consecutive_errors = 0
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
                consecutive_errors = 0

            except KeyboardInterrupt:
                print("[Agent] Shutting down.")
                break
            except Exception as e:
                consecutive_errors += 1
                print(f"[Agent] Loop error ({consecutive_errors}): {e}")
                # Server probably restarted and lost our registration — re-register
                if consecutive_errors >= 3:
                    print("[Agent] Too many errors, re-registering...")
                    self._do_register()
                    consecutive_errors = 0
                    last_heartbeat = 0

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
