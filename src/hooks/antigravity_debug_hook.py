import sys
import os
import json
import datetime

LOG_FILE_PATH = "/tmp/antigravity_hook_debug.log"

def log_debug_hook(event_name: str, payload: dict):
    """Log the parsed stdin JSON payload to the flat log file."""
    record = {
        "timestamp": datetime.datetime.now(datetime.timezone.utc).isoformat().replace("+00:00", "Z"),
        "event": event_name,
        "payload": payload
    }
    
    try:
        with open(LOG_FILE_PATH, "a", encoding="utf-8") as f:
            f.write(json.dumps(record) + "\n")
            f.flush()
    except Exception as e:
        # Best effort logging, don't crash the hook command execution
        sys.stderr.write(f"Error writing to hook log file {LOG_FILE_PATH}: {e}\n")

def main():
    # 1. Parse CLI arguments
    event_name = sys.argv[1] if len(sys.argv) > 1 else "Unknown"
    
    # 2. Read stdin
    raw_stdin = sys.stdin.read().strip()
    payload = {}
    
    if raw_stdin:
        try:
            payload = json.loads(raw_stdin)
        except Exception as e:
            sys.stderr.write(f"Error parsing JSON input from stdin: {e}\n")
            # Log raw stdin as fallback
            payload = {"raw_stdin": raw_stdin, "error": str(e)}
            
    # 3. Log hook execution details
    log_debug_hook(event_name, payload)
    
    # 4. Generate stdout response according to the event specification
    response = {}
    
    if event_name == "PreToolUse":
        response = {
            "decision": "allow",
            "reason": "Debugging hook automatically allowing tool call."
        }
    elif event_name == "PostToolUse":
        response = {}
    elif event_name == "PreInvocation":
        response = {
            "injectSteps": []
        }
    elif event_name == "PostInvocation":
        response = {
            "injectSteps": [],
            "terminationBehavior": ""
        }
    elif event_name == "Stop":
        response = {
            "decision": "allow",
            "reason": "Debugging hook automatically allowing stopping."
        }
    else:
        # Fallback response to avoid blocking execution
        response = {}
        
    # Write response to stdout as JSON
    sys.stdout.write(json.dumps(response))
    sys.stdout.flush()

if __name__ == "__main__":
    main()
