import zlib
import gzip
import bz2
import lzma

pb_path = "/Users/jacobmiller22/.gemini/antigravity-cli/conversations/ab12d317-3b14-4063-97a8-4c7b6aff06fe.pb"

with open(pb_path, "rb") as f:
    data = f.read()

print(f"Data length: {len(data)}")

try:
    dec = zlib.decompress(data)
    print(f"SUCCESS: zlib decompressed to {len(dec)} bytes")
except Exception as e:
    print(f"zlib failed: {e}")

try:
    dec = gzip.decompress(data)
    print(f"SUCCESS: gzip decompressed to {len(dec)} bytes")
except Exception as e:
    print(f"gzip failed: {e}")

try:
    dec = bz2.decompress(data)
    print(f"SUCCESS: bz2 decompressed to {len(dec)} bytes")
except Exception as e:
    print(f"bz2 failed: {e}")

try:
    dec = lzma.decompress(data)
    print(f"SUCCESS: lzma decompressed to {len(dec)} bytes")
except Exception as e:
    print(f"lzma failed: {e}")
