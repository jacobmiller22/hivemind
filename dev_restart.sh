#!/bin/bash
# dev_restart.sh - Quick rebuild and execution script for development testing.

set -e

# Harmonious HSL color codes for gorgeous TUI logging
COLOR_INFO="\033[1;36m"    # Cyan
COLOR_SUCCESS="\033[1;32m" # Green
COLOR_WARN="\033[1;33m"    # Yellow
COLOR_RESET="\033[0m"

echo -e "${COLOR_INFO}[*] Stopping existing hivemind daemons and clients...${COLOR_RESET}"

# 1. Gracefully stop daemon using PID file if it exists
PID_FILE="$HOME/.config/hivemind/daemon.pid"
if [ -f "$PID_FILE" ]; then
    PID=$(cat "$PID_FILE")
    if ps -p "$PID" > /dev/null; then
        echo -e "    Found active daemon PID: $PID. Sending SIGTERM..."
        kill "$PID" || true
        sleep 0.5
        # Escalation to SIGKILL if still alive
        if ps -p "$PID" > /dev/null; then
            echo -e "    ${COLOR_WARN}Daemon still alive. Force killing PID: $PID...${COLOR_RESET}"
            kill -9 "$PID" || true
        fi
    fi
    rm -f "$PID_FILE"
fi

# 2. Kill any stray daemon/client processes matching the name
echo -e "    Terminating any other running hivemind processes..."
pkill -f "hivemind daemon" || true
pkill -f "hivemind" || true

# 3. Clean up socket files to ensure a fresh listener
echo -e "    Cleaning up local Unix sockets..."
rm -f "$HOME/.config/hivemind/hivemind.sock"
rm -f "/tmp/hivemind.sock"

# 4. Rebuild the binary
echo -e "${COLOR_INFO}[*] Rebuilding hivemind binary...${COLOR_RESET}"
go build -o hivemind ./cmd/hivemind

echo -e "${COLOR_SUCCESS}[+] Build succeeded! Starting hivemind...${COLOR_RESET}"
echo ""

# 5. Execute
exec ./hivemind "$@"
