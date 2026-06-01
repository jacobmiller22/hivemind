import math
import os

def calculate_entropy(data):
    if not data:
        return 0.0
    entropy = 0
    for x in range(256):
        p_x = data.count(x) / len(data)
        if p_x > 0:
            entropy += - p_x * math.log2(p_x)
    return entropy

conv_dir = "/Users/jacobmiller22/.gemini/antigravity-cli/conversations"
files = [f for f in os.listdir(conv_dir) if f.endswith(".pb")]

for f in files[:5]:
    path = os.path.join(conv_dir, f)
    with open(path, "rb") as file:
        data = file.read()
        ent = calculate_entropy(data)
        print(f"File: {f}, Size: {len(data)} bytes, Entropy: {ent:.6f} bits/byte")
