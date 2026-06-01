import os
import binascii

conv_dir = "/Users/jacobmiller22/.gemini/antigravity-cli/conversations"
files = [f for f in os.listdir(conv_dir) if f.endswith(".pb")]

print(f"Checking {len(files)} files in {conv_dir}:")
for f in files[:5]:
    path = os.path.join(conv_dir, f)
    with open(path, "rb") as file:
        header = file.read(16)
        hex_str = binascii.hexlify(header).decode("utf-8")
        # Format hex string in pairs
        formatted = " ".join(hex_str[i:i+2] for i in range(0, len(hex_str), 2))
        print(f"  - {f}: size={os.path.getsize(path)} bytes, header=[{formatted}]")
