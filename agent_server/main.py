"""
IBootTime Agent Server — FastAPI service that manages remote agent clients.
Runs alongside the Wails app on the server machine.
"""

import time
import uuid
from contextlib import asynccontextmanager
from typing import Optional

from fastapi import FastAPI, HTTPException
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel

# ── Models ──────────────────────────────────────────────────────────────────

class AgentRegistration(BaseModel):
    hostname: str
    ip: str
    os_version: str
    mac: Optional[str] = None


class AgentHeartbeat(BaseModel):
    client_id: str


class TaskResult(BaseModel):
    client_id: str
    task_id: str
    success: bool
    output: str


class RemoteClient(BaseModel):
    client_id: str
    hostname: str
    ip: str
    os_version: str
    mac: Optional[str] = None
    status: str = "online"            # online | offline
    registered_at: float = 0.0
    last_seen: float = 0.0


class Task(BaseModel):
    task_id: str
    task_type: str                    # ping | create_test_file | open_notepad
    params: dict = {}
    status: str = "pending"           # pending | delivered | completed | failed
    result_output: str = ""
    created_at: float = 0.0
    completed_at: float = 0.0


# ── In-memory stores ───────────────────────────────────────────────────────

clients: dict[str, RemoteClient] = {}
# task queue per client: client_id -> list[Task]
task_queues: dict[str, list[Task]] = {}

OFFLINE_TIMEOUT = 30  # seconds without heartbeat → offline


# ── Lifespan (stale cleanup) ──────────────────────────────────────────────

@asynccontextmanager
async def lifespan(app: FastAPI):
    # startup — nothing special needed
    yield
    # shutdown — cleanup
    clients.clear()
    task_queues.clear()


# ── App ────────────────────────────────────────────────────────────────────

app = FastAPI(title="IBootTime Agent Server", version="1.0.0", lifespan=lifespan)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_methods=["*"],
    allow_headers=["*"],
)


# ── Agent endpoints (called by the Python client on remote PCs) ────────────

@app.post("/agent/register")
def agent_register(reg: AgentRegistration):
    """Remote agent registers itself after Windows boots."""
    client_id = reg.ip  # use IP as natural key for simplicity
    now = time.time()
    clients[client_id] = RemoteClient(
        client_id=client_id,
        hostname=reg.hostname,
        ip=reg.ip,
        os_version=reg.os_version,
        mac=reg.mac or "",
        status="online",
        registered_at=now,
        last_seen=now,
    )
    if client_id not in task_queues:
        task_queues[client_id] = []
    return {"client_id": client_id, "message": "registered"}


@app.post("/agent/heartbeat")
def agent_heartbeat(hb: AgentHeartbeat):
    """Client pings every few seconds to stay alive."""
    c = clients.get(hb.client_id)
    if not c:
        raise HTTPException(404, "client not registered")
    c.last_seen = time.time()
    c.status = "online"
    return {"ok": True}


@app.get("/agent/tasks/{client_id}")
def agent_get_tasks(client_id: str):
    """Client polls for pending tasks."""
    _refresh_statuses()
    queue = task_queues.get(client_id, [])
    pending = [t for t in queue if t.status == "pending"]
    # mark delivered
    for t in pending:
        t.status = "delivered"
    return {"tasks": [t.model_dump() for t in pending]}


@app.post("/agent/task-result")
def agent_post_result(result: TaskResult):
    """Client reports task execution result."""
    queue = task_queues.get(result.client_id, [])
    for t in queue:
        if t.task_id == result.task_id:
            t.status = "completed" if result.success else "failed"
            t.result_output = result.output
            t.completed_at = time.time()
            return {"ok": True}
    raise HTTPException(404, "task not found")


# ── Frontend / Go proxy endpoints ──────────────────────────────────────────

@app.get("/api/clients")
def api_list_clients():
    """List all registered remote agents."""
    _refresh_statuses()
    return {"clients": [c.model_dump() for c in clients.values()]}


@app.get("/api/clients/{client_id}")
def api_get_client(client_id: str):
    c = clients.get(client_id)
    if not c:
        raise HTTPException(404, "not found")
    _refresh_statuses()
    return c.model_dump()


@app.post("/api/clients/{client_id}/ping")
def api_ping(client_id: str):
    """Queue a connectivity-check task."""
    return _enqueue(client_id, "ping")


@app.post("/api/clients/{client_id}/create-test-file")
def api_create_test_file(client_id: str):
    """Queue a 'create test file' task."""
    return _enqueue(client_id, "create_test_file", {
        "path": r"C:\Temp\vengo_desde_el_servidor.txt",
        "content": "vengo desde el servidor",
    })


@app.post("/api/clients/{client_id}/open-notepad")
def api_open_notepad(client_id: str):
    """Queue an 'open notepad' task."""
    return _enqueue(client_id, "open_notepad", {
        "path": r"C:\Temp\vengo_desde_el_servidor.txt",
    })


@app.get("/api/clients/{client_id}/tasks")
def api_client_tasks(client_id: str):
    """Get task history for a client."""
    queue = task_queues.get(client_id, [])
    return {"tasks": [t.model_dump() for t in queue]}


# ── Helpers ────────────────────────────────────────────────────────────────

def _enqueue(client_id: str, task_type: str, params: dict | None = None) -> dict:
    if client_id not in clients:
        raise HTTPException(404, "client not found")
    task = Task(
        task_id=str(uuid.uuid4())[:8],
        task_type=task_type,
        params=params or {},
        status="pending",
        created_at=time.time(),
    )
    task_queues.setdefault(client_id, []).append(task)
    return {"task_id": task.task_id, "status": "queued"}


def _refresh_statuses():
    now = time.time()
    for c in clients.values():
        if now - c.last_seen > OFFLINE_TIMEOUT:
            c.status = "offline"


# ── Entry point ────────────────────────────────────────────────────────────

if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=9090, log_level="info")
