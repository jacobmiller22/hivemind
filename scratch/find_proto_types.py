import sys
from google.protobuf.internal.decoder import _DecodeVarint32

sys.path.append("/Users/jacobmiller22/.pyenv/versions/3.12.9/lib/python3.12/site-packages")
from google.antigravity.connections.local import localharness_pb2

pb_path = "/Users/jacobmiller22/.gemini/antigravity-cli/conversations/ab12d317-3b14-4063-97a8-4c7b6aff06fe.pb"

with open(pb_path, "rb") as f:
    data = f.read()

print(f"File size: {len(data)} bytes")

# Check if there is a single message of any type that parses at the start
for name in dir(localharness_pb2):
    cls = getattr(localharness_pb2, name)
    if not (isinstance(cls, type) and hasattr(cls, "FromString")):
        continue
    
    # Try as direct message
    try:
        msg = cls()
        msg.ParseFromString(data)
        fields = msg.ListFields()
        if fields:
            print(f"Direct match: {name} successfully parsed the whole file!")
            continue
    except Exception:
        pass

    # Try as varint-length-prefixed message at start
    try:
        msg_len, new_pos = _DecodeVarint32(data, 0)
        if 0 < msg_len <= len(data) - new_pos:
            msg_data = data[new_pos:new_pos + msg_len]
            msg = cls()
            msg.ParseFromString(msg_data)
            fields = msg.ListFields()
            if fields:
                print(f"Delimited match: {name} parsed first message of length {msg_len}!")
    except Exception:
        pass
