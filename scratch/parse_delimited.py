import sys
from google.protobuf.internal.decoder import _DecodeVarint32

sys.path.append("/Users/jacobmiller22/.pyenv/versions/3.12.9/lib/python3.12/site-packages")
from google.antigravity.connections.local import localharness_pb2

pb_path = "/Users/jacobmiller22/.gemini/antigravity-cli/conversations/ab12d317-3b14-4063-97a8-4c7b6aff06fe.pb"

with open(pb_path, "rb") as f:
    data = f.read()

print(f"File size: {len(data)} bytes")

# We'll try different message types
for name in ["OutputEvent", "StepUpdate", "InputEvent", "InitializeConversationEvent"]:
    cls = getattr(localharness_pb2, name)
    print(f"\n--- Trying length-delimited {name} ---")
    pos = 0
    count = 0
    success = True
    while pos < len(data):
        try:
            msg_len, new_pos = _DecodeVarint32(data, pos)
            if msg_len <= 0 or new_pos + msg_len > len(data):
                success = False
                break
            msg_data = data[new_pos:new_pos + msg_len]
            msg = cls()
            msg.ParseFromString(msg_data)
            fields = msg.ListFields()
            if fields:
                count += 1
                pos = new_pos + msg_len
            else:
                success = False
                break
        except Exception as e:
            success = False
            break
    if count > 0:
        print(f"SUCCESS: Read {count} length-delimited {name} messages!")
    else:
        print(f"Failed to read as length-delimited {name}")
