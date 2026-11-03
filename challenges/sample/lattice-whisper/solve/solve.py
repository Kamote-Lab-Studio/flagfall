"""Lattice Whisper: every nonce k is below 2^248 (its top 8 bits are zero).
For each signature, k = t*d + u (mod n) with t = r/s and u = z/s, so d is a
Hidden Number Problem instance: build the usual lattice, run LLL, and read
d off the short vector. Needs: pip install cysignals fpylll"""
import hashlib
import json
import sys
from pathlib import Path

from fpylll import LLL, IntegerMatrix

dist = Path(sys.argv[1] if len(sys.argv) > 1 else Path(__file__).parent.parent / "dist")
data = json.loads((dist / "signatures.json").read_text())
n = 0xFFFFFFFF00000000FFFFFFFFFFFFFFFFBCE6FAADA7179E84F3B9CAC2FC632551
B = 1 << 248

sigs = data["signatures"][:45]
ts, us = [], []
for s in sigs:
    r, sv = int(s["r"], 16), int(s["s"], 16)
    z = int.from_bytes(hashlib.sha256(s["msg"].encode()).digest(), "big")
    si = pow(sv, -1, n)
    ts.append(r * si % n)
    us.append(z * si % n)

# Rows, all scaled by n so they're integers:
#   n*n*e_i                                      one per signature
#   [t_1*n ... t_m*n, B, 0]
#   [(u_1 - B/2)*n ... (u_m - B/2)*n, 0, B*n/2]
# d*row_t + row_u - sum(c_i*row_i) = [(k_i - B/2)*n ..., d*B, B*n/2] is short.
m = len(sigs)
rows = []
for i in range(m):
    row = [0] * (m + 2)
    row[i] = n * n
    rows.append(row)
rows.append([t * n for t in ts] + [B, 0])
rows.append([(u - B // 2) * n for u in us] + [0, B * n // 2])

M = IntegerMatrix.from_matrix(rows)
LLL.reduction(M)

enc = bytes.fromhex((dist / "flag.enc").read_text().strip())
for row in M:
    row = list(row)
    if abs(row[-1]) != B * n // 2:
        continue
    for d in (row[-2] // B % n, -row[-2] // B % n):
        if (ts[0] * d + us[0]) % n < B:
            key = hashlib.shake_256(d.to_bytes(32, "big")).digest(len(enc))
            flag = bytes(a ^ b for a, b in zip(enc, key))
            if flag.startswith(b"flag{"):
                print(f"d = {d:#x}")
                print(flag.decode())
                sys.exit(0)
sys.exit("no key found")
