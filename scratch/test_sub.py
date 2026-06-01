import os
import sys
import json
import socket
import argparse

SOCKET_PATH = os.path.expanduser("~/.config/hivemind/hivemind.sock")

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--socket", default=SOCKET_PATH)
    args = parser.parse_args()

    sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    try:
        sock.connect(args.socket)
    except Exception as e:
        print(f"Failed to connect to {args.socket}: {e}")
        sys.exit(1)

    # Subscribe request
    req = {"type": "subscribe", "sessionId": "test-client"}
    sock.sendall(json.dumps(req).encode('utf-8') + b'\n')

    print(f"Subscribed to {args.socket}. Listening for state updates...")

    f = sock.makefile('r')
    for line in f:
        line = line.strip()
        if not line: continue
        try:
            data = json.loads(line)
            print("--- STATE UPDATE ---")
            print(json.dumps(data, indent=2))
        except Exception as e:
            print(f"Error parsing json: {e}")

if __name__ == "__main__":
    main()
