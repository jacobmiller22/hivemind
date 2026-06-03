import os
import sys
import json
import socket
import asyncio
import datetime
import argparse
from typing import Optional, List, Dict, Any

SOCKET_PATH = os.path.expanduser("~/.config/hivemind/hivemind.sock")
FALLBACK_SOCKET_PATH = "/tmp/hivemind.sock"

import subprocess

# Realistic coordinates
PARENT_SESSION_ID = f"session_parent_{os.urandom(4).hex()}"
SUBAGENT_SESSION_ID = f"session_child_{os.urandom(4).hex()}"
SUBAGENT_ID = f"sub_{os.urandom(6).hex()}"

# Dynamically resolve tmux context using the user's actual active tmux session and panes
def resolve_tmux_details():
    pane_id = os.environ.get("TMUX_PANE", "%0")
    session = "hivemind-workspace"
    window = "1"
    
    if pane_id:
        try:
            res = subprocess.run(
                ["tmux", "display-message", "-p", "-F", "#S #I", "-t", pane_id],
                capture_output=True, text=True, timeout=1
            )
            if res.returncode == 0:
                parts = res.stdout.strip().split()
                if len(parts) >= 2:
                    session = parts[0]
                    window = parts[1]
        except Exception:
            pass
            
    # Try to find a second active pane to represent the subagent, or default to a dummy active one
    sub_pane_id = "%1"
    try:
        res = subprocess.run(
            ["tmux", "list-panes", "-a", "-F", "#{pane_id}"],
            capture_output=True, text=True, timeout=1
        )
        if res.returncode == 0:
            panes = [p.strip() for p in res.stdout.strip().split("\n") if p.strip()]
            # Find a pane that is NOT the parent pane if possible, to represent the subagent
            other_panes = [p for p in panes if p != pane_id]
            if other_panes:
                sub_pane_id = other_panes[0]
            elif panes:
                sub_pane_id = panes[0]
    except Exception:
        pass

    return pane_id, session, window, sub_pane_id

PA_PANE, TMUX_SESS, TMUX_WIN, SUB_PANE = resolve_tmux_details()

DEFAULT_CONTEXT = {
    "tmuxPaneId": PA_PANE,
    "tmuxSession": TMUX_SESS,
    "tmuxWindow": TMUX_WIN,
    "cwd": os.getcwd(),
    "gitBranch": "main"
}

SUBAGENT_CONTEXT = {
    "tmuxPaneId": SUB_PANE,
    "tmuxSession": TMUX_SESS,
    "tmuxWindow": TMUX_WIN,
    "cwd": os.getcwd(),
    "gitBranch": "main"
}

def generate_event(session_id: str, event_type: str, status: Optional[str] = None, payload: Optional[dict] = None, context: Optional[dict] = None) -> dict:
    return {
        "type": "event",
        "eventId": f"evt_{os.urandom(8).hex()}",
        "sessionId": session_id,
        "timestamp": datetime.datetime.now(datetime.timezone.utc).isoformat().replace("+00:00", "Z"),
        "eventType": event_type,
        "context": context or DEFAULT_CONTEXT,
        "payload": {
            "status": status,
            **(payload or {})
        }
    }

async def send_single_event(event: dict, socket_paths: List[str]) -> bool:
    writer = None
    connected_path = None
    for path in socket_paths:
        try:
            resolved_path = os.path.expanduser(path)
            reader, writer = await asyncio.open_unix_connection(resolved_path)
            connected_path = resolved_path
            break
        except Exception:
            continue

    if writer is None:
        print(f"[-] Failed to connect to any socket in {socket_paths}")
        return False

    try:
        data = json.dumps(event).encode('utf-8') + b'\n'
        writer.write(data)
        await writer.drain()
        writer.close()
        await writer.wait_closed()
        print(f"[+] Successfully sent event '{event['eventType']}' (status: {event['payload'].get('status')}) to {connected_path}")
        return True
    except Exception as e:
        print(f"[-] Error writing to {connected_path}: {e}")
        return False

async def run_client_simulation(socket_paths: List[str], delay: float):
    print(f"[*] Starting multi-agent telemetry simulation...")
    print(f"[*] Parent Session ID: {PARENT_SESSION_ID}")
    print(f"[*] Subagent Session ID: {SUBAGENT_SESSION_ID}")
    print(f"[*] Subagent ID: {SUBAGENT_ID}")
    
    events = [
        # 1. Parent Session Starts
        generate_event(
            session_id=PARENT_SESSION_ID,
            event_type="session_started",
            status="idle",
            payload={"model": "Gemini 3.5 Flash"}
        ),
        # 2. Parent User Input Turn Begins
        generate_event(
            session_id=PARENT_SESSION_ID,
            event_type="status_changed",
            status="thinking",
            payload={"prompt": "Please write a subagent to optimize index.js"}
        ),
        # 3. Parent runs standard tool
        generate_event(
            session_id=PARENT_SESSION_ID,
            event_type="status_changed",
            status="tool-running",
            payload={"toolName": "list_dir", "toolArgs": {"DirectoryPath": "/Users/jacobmiller22/projects/hivemind"}}
        ),
        # 4. Parent spawns a Sub-agent
        generate_event(
            session_id=PARENT_SESSION_ID,
            event_type="subagent_spawned",
            status="tool-running",
            payload={
                "subagent": {
                    "id": SUBAGENT_ID,
                    "role": "Code Optimizer",
                    "typeName": "self",
                    "status": "running"
                }
            }
        ),
        # 5. Sub-agent Session Starts in background
        generate_event(
            session_id=SUBAGENT_SESSION_ID,
            event_type="session_started",
            status="idle",
            payload={"model": "Gemini 3.5 Flash"},
            context=SUBAGENT_CONTEXT
        ),
        # 6. Sub-agent starts thinking
        generate_event(
            session_id=SUBAGENT_SESSION_ID,
            event_type="status_changed",
            status="thinking",
            payload={"prompt": "Optimize index.js for better runtime performance"},
            context=SUBAGENT_CONTEXT
        ),
        # 7. Sub-agent runs a tool (replacing file content)
        generate_event(
            session_id=SUBAGENT_SESSION_ID,
            event_type="status_changed",
            status="tool-running",
            payload={"toolName": "replace_file_content", "toolArgs": {"TargetFile": "/Users/jacobmiller22/projects/hivemind/index.js"}},
            context=SUBAGENT_CONTEXT
        ),
        # 8. Sub-agent hits a Permission Prompt (highest-priority display state!)
        generate_event(
            session_id=SUBAGENT_SESSION_ID,
            event_type="status_changed",
            status="awaiting-permission",
            payload={
                "toolName": "write_file",
                "toolTarget": "/Users/jacobmiller22/projects/hivemind/index.js",
                "toolArgs": {"Action": "write_file", "Target": "/Users/jacobmiller22/projects/hivemind/index.js"}
            },
            context=SUBAGENT_CONTEXT
        ),
        # 9. Sub-agent gets permission, completes its run and shuts down
        generate_event(
            session_id=SUBAGENT_SESSION_ID,
            event_type="session_stopped",
            status="idle",
            context=SUBAGENT_CONTEXT
        ),
        # 10. Parent receives notification of subagent completion, updates subagent status
        generate_event(
            session_id=PARENT_SESSION_ID,
            event_type="subagent_status_changed",
            status="tool-running",
            payload={
                "subagent": {
                    "id": SUBAGENT_ID,
                    "role": "Code Optimizer",
                    "typeName": "self",
                    "status": "completed"
                }
            }
        ),
        # 11. Parent asks user a direct question
        generate_event(
            session_id=PARENT_SESSION_ID,
            event_type="status_changed",
            status="awaiting-input",
            payload={"question": "Should I proceed with the performance benchmark test?"}
        ),
        # 12. Parent Session terminates
        generate_event(
            session_id=PARENT_SESSION_ID,
            event_type="session_stopped",
            status="idle"
        )
    ]

    for event in events:
        success = await send_single_event(event, socket_paths)
        if not success:
            print("[!] Simulation aborted because event delivery failed. Is a receiver socket listening?")
            return
        await asyncio.sleep(delay)
    
    print("[+] Telemetry simulation finished successfully!")

async def handle_server_client(reader: asyncio.StreamReader, writer: asyncio.StreamWriter):
    addr = writer.get_extra_info('peername')
    print(f"[*] New connection from client")
    try:
        while True:
            data = await reader.readline()
            if not data:
                break
            line = data.decode('utf-8').strip()
            if not line:
                continue
            try:
                event = json.loads(line)
                print(f"\n[+] RECEIVED TELEMETRY EVENT:")
                print(json.dumps(event, indent=2))
            except json.JSONDecodeError:
                print(f"[-] Received non-JSON or invalid JSON content: {line}")
    except asyncio.CancelledError:
        pass
    except Exception as e:
        print(f"[-] Client connection error: {e}")
    finally:
        writer.close()
        await writer.wait_closed()
        print(f"[*] Client connection closed")

async def run_mock_server(path: str):
    resolved_path = os.path.expanduser(path)
    # Ensure directory exists
    os.makedirs(os.path.dirname(resolved_path), exist_ok=True)
    # Remove existing socket file if it exists
    if os.path.exists(resolved_path):
        try:
            os.remove(resolved_path)
        except OSError as e:
            print(f"[-] Failed to remove existing socket {resolved_path}: {e}")
            return

    server = await asyncio.start_unix_server(handle_server_client, path=resolved_path)
    print(f"[+] Mock server listening on Unix Domain Socket: {resolved_path}")
    print("[*] Press Ctrl+C to stop the server")

    try:
        async with server:
            await server.serve_forever()
    except asyncio.CancelledError:
        pass
    finally:
        if os.path.exists(resolved_path):
            os.remove(resolved_path)
        print(f"[*] Stopped mock server and cleaned up socket {resolved_path}")

def main():
    parser = argparse.ArgumentParser(description="Hivemind Telemetry Mock Emitter")
    parser.add_argument("--mode", choices=["client", "server"], default="client", help="Run mode (client to emit events, server to listen)")
    parser.add_argument("--socket", default=FALLBACK_SOCKET_PATH, help="Socket path for mock server to listen on")
    parser.add_argument("--delay", type=float, default=2.0, help="Delay in seconds between client simulation events")
    args = parser.parse_args()

    if args.mode == "server":
        try:
            asyncio.run(run_mock_server(args.socket))
        except KeyboardInterrupt:
            print("\n[*] Exiting server...")
    else:
        # Client mode
        socket_paths = [SOCKET_PATH, FALLBACK_SOCKET_PATH]
        # Allow specifying custom socket via argument
        if args.socket:
            socket_paths.insert(0, args.socket)
        try:
            asyncio.run(run_client_simulation(socket_paths, args.delay))
        except KeyboardInterrupt:
            print("\n[*] Exiting simulation...")

if __name__ == "__main__":
    main()
