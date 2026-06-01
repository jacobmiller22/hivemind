import os
import sys
import json
import time
import tempfile
import argparse
import datetime
import subprocess
from typing import Optional, Dict, Any

DEFAULT_SESSIONS_DIR = os.path.expanduser("~/.config/hivemind/sessions")

# Realistic session / subagent identifiers
PARENT_SESSION_ID = f"session_parent_{os.urandom(4).hex()}"
SUBAGENT_ID = f"sub_{os.urandom(6).hex()}"


# ---------------------------------------------------------------------------
# Tmux context resolution (same pattern as mock_emitter.py)
# ---------------------------------------------------------------------------

def resolve_tmux_details():
    pane_id = os.environ.get("TMUX_PANE", "%0")
    session = "developer-swarm"
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

    return pane_id, session, window


PANE_ID, TMUX_SESS, TMUX_WIN = resolve_tmux_details()


# ---------------------------------------------------------------------------
# File I/O helpers
# ---------------------------------------------------------------------------

def session_filepath(sessions_dir: str, session_id: str) -> str:
    """Return the full path for a session's JSON file."""
    return os.path.join(sessions_dir, f"{session_id}.json")


def write_session_file(sessions_dir: str, session_id: str, state: dict) -> str:
    """Atomically write *state* as JSON to <sessions_dir>/<session_id>.json.

    Writes to a temporary file in the same directory first, then uses
    os.replace() so the daemon never reads a partially-written file.
    """
    target = session_filepath(sessions_dir, session_id)
    fd, tmp_path = tempfile.mkstemp(dir=sessions_dir, suffix=".tmp")
    try:
        with os.fdopen(fd, "w") as f:
            json.dump(state, f, indent=2)
            f.write("\n")
        os.replace(tmp_path, target)
    except Exception:
        # Best-effort cleanup of the temp file on failure
        try:
            os.unlink(tmp_path)
        except OSError:
            pass
        raise
    return target


def delete_session_file(sessions_dir: str, session_id: str) -> None:
    """Remove the session JSON file if it exists."""
    target = session_filepath(sessions_dir, session_id)
    if os.path.exists(target):
        os.remove(target)


# ---------------------------------------------------------------------------
# State builder
# ---------------------------------------------------------------------------

def build_state(
    session_id: str,
    status: str,
    model: str = "Gemini 3.5 Flash",
    subagents: Optional[Dict[str, Any]] = None,
) -> dict:
    """Build a SessionState dict matching the daemon's expected schema."""
    return {
        "sessionId": session_id,
        "tmuxPaneId": PANE_ID,
        "tmuxSession": TMUX_SESS,
        "tmuxWindow": TMUX_WIN,
        "cwd": os.getcwd(),
        "gitBranch": "main",
        "model": model,
        "status": status,
        "subagents": subagents or {},
    }


# ---------------------------------------------------------------------------
# Simulation
# ---------------------------------------------------------------------------

TOTAL_STEPS = 9


def _log_step(step: int, message: str, filename: str, verb: str = "updated") -> None:
    print(f"[{step}/{TOTAL_STEPS}] ✔ {message} → {verb} {filename}")


import shutil

def write_transcript_step(sessions_dir, uuid, step_index, source, type_name, status, content=None, tool_calls=None):
    logs_dir = os.path.join(sessions_dir, uuid, ".system_generated", "logs")
    os.makedirs(logs_dir, exist_ok=True)
    transcript_path = os.path.join(logs_dir, "transcript.jsonl")
    
    step = {
        "step_index": step_index,
        "source": source,
        "type": type_name,
        "status": status,
        "created_at": datetime.datetime.now(datetime.timezone.utc).isoformat().replace("+00:00", "Z"),
    }
    if content is not None:
        step["content"] = content
    if tool_calls is not None:
        step["tool_calls"] = tool_calls
        
    with open(transcript_path, "a") as f:
        f.write(json.dumps(step) + "\n")
        f.flush()

def delete_transcript_session(sessions_dir, uuid):
    path = os.path.join(sessions_dir, uuid)
    if os.path.exists(path):
        shutil.rmtree(path)

def run_simulation(sessions_dir: str, delay: float, mode: str = "transcript") -> None:
    os.makedirs(sessions_dir, exist_ok=True)
    
    print(f"[*] Starting file-based telemetry simulation (mode: {mode})...")
    print(f"[*] Sessions directory: {sessions_dir}")
    print(f"[*] Parent Session ID: {PARENT_SESSION_ID}")
    print(f"[*] Subagent ID:       {SUBAGENT_ID}")
    print()

    if mode == "json":
        filename = f"{PARENT_SESSION_ID}.json"
        subagent_spawned_at = datetime.datetime.now(datetime.timezone.utc).isoformat().replace("+00:00", "Z")

        # Step 1 — idle
        state = build_state(PARENT_SESSION_ID, status="idle")
        write_session_file(sessions_dir, PARENT_SESSION_ID, state)
        _log_step(1, "Session started (status: idle)", filename, verb="wrote")

        # Step 2 — thinking
        time.sleep(2 * delay)
        state["status"] = "thinking"
        write_session_file(sessions_dir, PARENT_SESSION_ID, state)
        _log_step(2, "Status changed (status: thinking)", filename)

        # Step 3 — tool-running, no subagents
        time.sleep(2 * delay)
        state["status"] = "tool-running"
        write_session_file(sessions_dir, PARENT_SESSION_ID, state)
        _log_step(3, "Status changed (status: tool-running)", filename)

        # Step 4 — tool-running, subagent spawned (running)
        time.sleep(2 * delay)
        state["subagents"] = {
            SUBAGENT_ID: {
                "id": SUBAGENT_ID,
                "role": "Code Optimizer",
                "typeName": "self",
                "status": "running",
                "spawnedAt": subagent_spawned_at,
            }
        }
        write_session_file(sessions_dir, PARENT_SESSION_ID, state)
        _log_step(4, f"Subagent spawned (subagent: {SUBAGENT_ID}, status: running)", filename)

        # Step 5 — subagent completed, parent still tool-running
        time.sleep(3 * delay)
        state["subagents"][SUBAGENT_ID]["status"] = "completed"
        write_session_file(sessions_dir, PARENT_SESSION_ID, state)
        _log_step(5, f"Subagent completed (subagent: {SUBAGENT_ID}, status: completed)", filename)

        # Step 6 — awaiting-permission
        time.sleep(2 * delay)
        state["status"] = "awaiting-permission"
        write_session_file(sessions_dir, PARENT_SESSION_ID, state)
        _log_step(6, "Status changed (status: awaiting-permission)", filename)

        # Step 7 — idle
        time.sleep(3 * delay)
        state["status"] = "idle"
        write_session_file(sessions_dir, PARENT_SESSION_ID, state)
        _log_step(7, "Status changed (status: idle)", filename)

        # Step 8 — completed
        time.sleep(2 * delay)
        state["status"] = "completed"
        write_session_file(sessions_dir, PARENT_SESSION_ID, state)
        _log_step(8, "Status changed (status: completed)", filename)

        # Step 9 — delete file (session done)
        time.sleep(2 * delay)
        delete_session_file(sessions_dir, PARENT_SESSION_ID)
        _log_step(9, "Session finished — file removed", filename, verb="deleted")

    else: # mode == "transcript"
        filename = f"{PARENT_SESSION_ID}/.system_generated/logs/transcript.jsonl"
        
        # Step 1 — USER_INPUT (thinking)
        user_info = f"""<user_information>
App Data Directory: /Users/jacobmiller22/.gemini/antigravity
Conversation ID: {PARENT_SESSION_ID}
The user has 1 active workspaces, each defined by a URI and a CorpusName. Multiple URIs potentially map to the same CorpusName. The mapping is shown as follows in the format [URI] -> [CorpusName]:
/Users/jacobmiller22/projects/hivemind -> /Users/jacobmiller22/projects/hivemind
</user_information>
<USER_SETTINGS_CHANGE>
The user changed setting `Model Selection` from None to Gemini 3.5 Flash (High).
</USER_SETTINGS_CHANGE>"""
        write_transcript_step(sessions_dir, PARENT_SESSION_ID, 0, "USER_EXPLICIT", "USER_INPUT", "DONE", content=f"<USER_REQUEST>\nPlease optimize index.js\n</USER_REQUEST>\n{user_info}")
        _log_step(1, "USER_INPUT written (status: thinking)", filename, verb="wrote")

        # Step 2 — PLANNER_RESPONSE (thinking - in progress)
        time.sleep(2 * delay)
        write_transcript_step(sessions_dir, PARENT_SESSION_ID, 1, "MODEL", "PLANNER_RESPONSE", "IN_PROGRESS")
        _log_step(2, "PLANNER_RESPONSE in progress (status: thinking)", filename)

        # Step 3 — PLANNER_RESPONSE with tool call (tool-running)
        time.sleep(2 * delay)
        write_transcript_step(sessions_dir, PARENT_SESSION_ID, 2, "MODEL", "PLANNER_RESPONSE", "DONE", tool_calls=[
            {"name": "list_dir", "args": {"DirectoryPath": "/Users/jacobmiller22/projects/hivemind"}}
        ])
        _log_step(3, "PLANNER_RESPONSE with tool call (status: tool-running)", filename)

        # Step 4 — Tool output completes (thinking)
        time.sleep(2 * delay)
        write_transcript_step(sessions_dir, PARENT_SESSION_ID, 3, "MODEL", "LIST_DIRECTORY", "DONE", content="index.js\npackage.json")
        _log_step(4, "Tool execution complete (status: thinking)", filename)

        # Step 5 — Spawn subagent (tool-running)
        time.sleep(2 * delay)
        write_transcript_step(sessions_dir, PARENT_SESSION_ID, 4, "MODEL", "PLANNER_RESPONSE", "DONE", tool_calls=[
            {"name": "invoke_subagent", "args": {
                "Subagents": [{"Role": "Code Optimizer", "TypeName": "self", "Prompt": "Optimize index.js"}]
            }}
        ])
        _log_step(5, f"PLANNER_RESPONSE spawn subagent (status: tool-running)", filename)

        # Step 6 — Subagent tool completed (thinking)
        time.sleep(3 * delay)
        write_transcript_step(sessions_dir, PARENT_SESSION_ID, 5, "MODEL", "INVOKE_SUBAGENT", "DONE", content="Subagent finished optimizing!")
        _log_step(6, "Subagent tool complete (status: thinking)", filename)

        # Step 7 — Awaiting permission (awaiting-permission)
        time.sleep(2 * delay)
        write_transcript_step(sessions_dir, PARENT_SESSION_ID, 6, "MODEL", "PLANNER_RESPONSE", "DONE", tool_calls=[
            {"name": "ask_permission", "args": {"Action": "write_file", "Target": "/Users/jacobmiller22/projects/hivemind/index.js"}}
        ])
        _log_step(7, "PLANNER_RESPONSE ask permission (status: awaiting-permission)", filename)

        # Step 8 — Awaiting input (awaiting-input)
        time.sleep(3 * delay)
        write_transcript_step(sessions_dir, PARENT_SESSION_ID, 7, "MODEL", "PLANNER_RESPONSE", "DONE", tool_calls=[
            {"name": "ask_question", "args": {"questions": [{"question": "Should I run benchmarks?"}]}}
        ])
        _log_step(8, "PLANNER_RESPONSE ask question (status: awaiting-input)", filename)

        # Step 9 — Completed turn & Idle (idle)
        time.sleep(2 * delay)
        write_transcript_step(sessions_dir, PARENT_SESSION_ID, 8, "MODEL", "PLANNER_RESPONSE", "DONE", content="Successfully optimized!")
        _log_step(9, "PLANNER_RESPONSE complete with response (status: idle)", filename)

        # Step 10 — Cleanup/Delete session (session done)
        time.sleep(2 * delay)
        delete_transcript_session(sessions_dir, PARENT_SESSION_ID)
        _log_step(10, "Session finished — transcript folder removed", filename, verb="deleted")

    print()
    print("[+] File-based telemetry simulation finished successfully!")


# ---------------------------------------------------------------------------
# CLI entry point
# ---------------------------------------------------------------------------

def main():
    parser = argparse.ArgumentParser(
        description="Hivemind File-Based Telemetry Mock Emitter"
    )
    parser.add_argument(
        "--sessions-dir",
        default=DEFAULT_SESSIONS_DIR,
        help=f"Path to sessions directory (default: {DEFAULT_SESSIONS_DIR})",
    )
    parser.add_argument(
        "--delay",
        type=float,
        default=1.0,
        help="Multiplier for sleep durations (default: 1.0). Use 0.5 for 2× speed.",
    )
    parser.add_argument(
        "--mode",
        choices=["transcript", "json"],
        default="transcript",
        help="Simulation mode: 'transcript' (Antigravity-mode) or 'json' (default: transcript)",
    )
    args = parser.parse_args()

    sessions_dir = os.path.expanduser(args.sessions_dir)

    try:
        run_simulation(sessions_dir, args.delay, args.mode)
    except KeyboardInterrupt:
        print(f"\n[!] Interrupted — cleaning up session files...")
        if args.mode == "json":
            delete_session_file(sessions_dir, PARENT_SESSION_ID)
            print(f"[*] Removed {PARENT_SESSION_ID}.json")
        else:
            delete_transcript_session(sessions_dir, PARENT_SESSION_ID)
            print(f"[*] Removed transcript folder {PARENT_SESSION_ID}/")
        sys.exit(1)


if __name__ == "__main__":
    main()
