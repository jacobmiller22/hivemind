import os
import sys
from google.protobuf import json_format

# Add site-packages to path just in case
sys.path.append("/Users/jacobmiller22/.pyenv/versions/3.12.9/lib/python3.12/site-packages")

from google.antigravity.connections.local import localharness_pb2

pb_path = "/Users/jacobmiller22/.gemini/antigravity-cli/conversations/ab12d317-3b14-4063-97a8-4c7b6aff06fe.pb"

with open(pb_path, "rb") as f:
    data = f.read()

print(f"File size: {len(data)} bytes")

# Try to parse with all message classes
for name in dir(localharness_pb2):
    cls = getattr(localharness_pb2, name)
    if isinstance(cls, type) and hasattr(cls, "FromString"):
        try:
            msg = cls()
            msg.ParseFromString(data)
            # Check if any fields are populated
            fields = msg.ListFields()
            if fields:
                print(f"SUCCESS: Decoded successfully as {name}!")
                print(f"Populated fields:")
                for descriptor, value in fields:
                    print(f"  - {descriptor.name}: type={descriptor.type}, value_type={type(value)}")
                    # Print preview of the field value
                    val_str = str(value)
                    if len(val_str) > 300:
                        val_str = val_str[:300] + "..."
                    print(f"    value: {val_str}")
        except Exception as e:
            pass
