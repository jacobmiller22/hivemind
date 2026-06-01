import struct
import sys

sys.path.append("/Users/jacobmiller22/.pyenv/versions/3.12.9/lib/python3.12/site-packages")
from google.antigravity.connections.local import localharness_pb2

pb_path = "/Users/jacobmiller22/.gemini/antigravity-cli/conversations/ab12d317-3b14-4063-97a8-4c7b6aff06fe.pb"

with open(pb_path, "rb") as f:
    data = f.read()

print(f"File size: {len(data)} bytes")

for name in dir(localharness_pb2):
    cls = getattr(localharness_pb2, name)
    if not (isinstance(cls, type) and hasattr(cls, "FromString")):
        continue
    
    pos = 0
    count = 0
    success = True
    while pos < len(data):
        if pos + 4 > len(data):
            success = False
            break
        msg_len = struct.unpack("<I", data[pos:pos+4])[0]
        if msg_len <= 0 or pos + 4 + msg_len > len(data):
            success = False
            break
        
        try:
            msg_data = data[pos+4 : pos+4+msg_len]
            msg = cls()
            msg.ParseFromString(msg_data)
            fields = msg.ListFields()
            if fields:
                count += 1
                pos += 4 + msg_len
            else:
                success = False
                break
        except Exception:
            success = False
            break
            
    if count > 0:
        print(f"Match: {name} successfully parsed {count} messages using 4-byte little-endian length prefix!")
        print(f"Total parsed bytes: {pos} / {len(data)}")
