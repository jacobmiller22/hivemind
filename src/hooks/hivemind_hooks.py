import os
import sys
import json
import socket
import asyncio
import datetime
import subprocess
from typing import Optional, Any, List, Dict
from google.antigravity import types
from google.antigravity.hooks import hooks

SOCKET_PATH = os.path.expanduser("~/.config/hivemind/hivemind.sock")
FALLBACK_SOCKET_PATH = "/tmp/hivemind.sock"

def log_debug(msg: str):
    try:
        with open("/tmp/hivemind_hooks.log", "a") as f:
            f.write(f"{datetime.datetime.utcnow().isoformat()}Z - {msg}\n")
    except Exception:
        pass

log_debug("hivemind_hooks module imported!")

class HivemindTelemetryClient:
    def __init__(self):
        log_debug("HivemindTelemetryClient initialized")
        self.session_id: Optional[str] = None
        self.tmux_pane_id = os.environ.get("TMUX_PANE", "")
        self.tmux_session = ""
        self.tmux_window = ""
        self.cwd = os.getcwd()
        self.git_branch = self._resolve_git_branch()
        self._resolve_tmux_coordinates()
        # Track active subagents spawned in this session to update their lifecycle status
        self.current_subagents: List[Dict[str, Any]] = []

    def _resolve_git_branch(self) -> Optional[str]:
        try:
            res = subprocess.run(["git", "branch", "--show-current"], capture_output=True, text=True, timeout=1)
            return res.stdout.strip() if res.returncode == 0 else None
        except Exception:
            return None

    def _resolve_tmux_coordinates(self):
        if not self.tmux_pane_id:
            return
        try:
            # Query tmux for current session and window index of this pane
            res = subprocess.run(
                ["tmux", "display-message", "-p", "-F", "#S #I", "-t", self.tmux_pane_id],
                capture_output=True, text=True, timeout=1
            )
            if res.returncode == 0:
                parts = res.stdout.strip().split()
                if len(parts) >= 2:
                    self.tmux_session = parts[0]
                    self.tmux_window = parts[1]
        except Exception:
            pass

    async def send_event(self, event_type: str, status: Optional[str] = None, payload: Optional[dict] = None):
        log_debug(f"send_event called with type: {event_type}, session: {self.session_id}")
        if not self.session_id:
            log_debug("No session_id, skipping send")
            return

        event = {
            "type": "event",
            "eventId": f"evt_{os.urandom(8).hex()}",
            "sessionId": self.session_id,
            "timestamp": datetime.datetime.utcnow().isoformat() + "Z",
            "eventType": event_type,
            "context": {
                "tmuxPaneId": self.tmux_pane_id,
                "tmuxSession": self.tmux_session,
                "tmuxWindow": self.tmux_window,
                "cwd": self.cwd,
                "gitBranch": self.git_branch
            },
            "payload": {
                "status": status,
                **(payload or {})
            }
        }

        # Safe UDS write - must NEVER crash parent agent
        writer = None
        for path in [SOCKET_PATH, FALLBACK_SOCKET_PATH]:
            try:
                resolved_path = os.path.expanduser(path)
                reader, writer = await asyncio.open_unix_connection(resolved_path)
                break
            except Exception as e:
                log_debug(f"Failed to connect to socket {path}: {e}")
                continue

        if writer is None:
            log_debug("All socket connection attempts failed.")
            return

        try:
            writer.write(json.dumps(event).encode('utf-8') + b'\n')
            await writer.drain()
            writer.close()
            await writer.wait_closed()
            log_debug(f"Successfully sent event: {event_type}")
        except Exception as e:
            log_debug(f"Exception during socket write: {e}")
            pass

# Singleton Client Instance
client = HivemindTelemetryClient()

@hooks.on_session_start
async def on_session_start(*args, **kwargs):
    log_debug(f"on_session_start hook fired! args: {args}, kwargs: {kwargs}")
    # Retrieve or generate Conversation ID
    session_id = None
    if args:
        session_id = getattr(args[0], "conversation_id", None)
    if not session_id and kwargs:
        session_id = kwargs.get("conversation_id")
        
    client.session_id = session_id or f"session_{os.urandom(8).hex()}"
    log_debug(f"Determined session ID: {client.session_id}")
    await client.send_event("session_started", status="idle")

@hooks.on_session_end
async def on_session_end(*args, **kwargs):
    await client.send_event("session_stopped", status="idle")

@hooks.pre_turn
async def pre_turn(prompt: str) -> types.HookResult:
    await client.send_event("status_changed", status="thinking", payload={"prompt": prompt})
    return types.HookResult(allow=True)

@hooks.post_turn
async def post_turn(response: str):
    await client.send_event("status_changed", status="idle", payload={"response": response})

@hooks.pre_tool_call_decide
async def pre_tool(tool_call: types.ToolCall) -> types.HookResult:
    # 1. Check for interactive permission request tool
    if tool_call.name == "ask_permission":
        await client.send_event("status_changed", status="awaiting-permission", payload={
            "toolName": tool_call.args.get("Action"),
            "toolTarget": tool_call.args.get("Target"),
            "toolArgs": tool_call.args
        })
    # 2. Check for conversational user question
    elif tool_call.name == "ask_question":
        await client.send_event("status_changed", status="awaiting-input", payload={
            "question": tool_call.args.get("questions")
        })
    # 3. Intercept subagent spawning
    elif tool_call.name == "invoke_subagent":
        subagents = tool_call.args.get("Subagents", [])
        for sa in subagents:
            sub_id = f"sub_{os.urandom(6).hex()}"
            sub_info = {
                "id": sub_id,
                "role": sa.get("Role", "Subagent"),
                "typeName": sa.get("TypeName", "self"),
                "status": "running"
            }
            client.current_subagents.append(sub_info)
            await client.send_event("subagent_spawned", payload={
                "subagent": sub_info
            })
    # 4. Default active tool running state
    else:
        await client.send_event("status_changed", status="tool-running", payload={
            "toolName": tool_call.name,
            "toolArgs": tool_call.args
        })
    
    return types.HookResult(allow=True)

@hooks.post_tool_call
async def post_tool(data: Any):
    # If we had subagents spawned by the last tool call, mark them as completed
    if client.current_subagents:
        for sa in client.current_subagents:
            sa["status"] = "completed"
            await client.send_event("subagent_status_changed", payload={
                "subagent": sa
            })
        client.current_subagents = []

@hooks.on_tool_error
async def on_tool_error(error: Exception):
    # If we had subagents spawned by the last tool call, mark them as errored
    if client.current_subagents:
        for sa in client.current_subagents:
            sa["status"] = "errored"
            await client.send_event("subagent_status_changed", payload={
                "subagent": sa
            })
        client.current_subagents = []

    await client.send_event("status_changed", status="errored", payload={
        "errorType": type(error).__name__,
        "errorMessage": str(error)
    })
