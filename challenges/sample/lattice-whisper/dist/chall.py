# Lattice Whisper: how signatures.json and flag.enc were made.
import hashlib, json, secrets
from ecdsa import NIST256p  # pip install ecdsa

G, n = NIST256p.generator, NIST256p.order
d = secrets.randbelow(n - 1) + 1          # the private key

sigs = []
for i in range(60):
    msg = f"telemetry batch {i:03d}: all systems nominal"
    z = int.from_bytes(hashlib.sha256(msg.encode()).digest(), "big")
    k = secrets.randbits(248)             # "random enough"
    r = (k * G).x() % n
    s = pow(k, -1, n) * (z + r * d) % n
    sigs.append({"msg": msg, "r": f"{r:x}", "s": f"{s:x}"})
json.dump({"curve": "P-256", "hash": "SHA-256", "signatures": sigs}, open("signatures.json", "w"), indent=2)

flag = open("flag.txt", "rb").read().strip()
key = hashlib.shake_256(d.to_bytes(32, "big")).digest(len(flag))
open("flag.enc", "w").write(bytes(a ^ b for a, b in zip(flag, key)).hex() + "\n")
