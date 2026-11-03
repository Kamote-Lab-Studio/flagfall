"""Baby RSA: with e = 3 and no padding, m^3 < n, so c = m^3 exactly and
the flag is the integer cube root of c."""
import sys
from pathlib import Path

dist = Path(sys.argv[1] if len(sys.argv) > 1 else Path(__file__).parent.parent / "dist")
vals = dict(line.split(" = ") for line in (dist / "output.txt").read_text().splitlines())
c = int(vals["c"])


def iroot(x, k):
    lo, hi = 0, 1 << (x.bit_length() // k + 1)
    while lo < hi:
        mid = (lo + hi) // 2
        if mid**k < x:
            lo = mid + 1
        else:
            hi = mid
    return lo


m = iroot(c, 3)
assert m**3 == c, "not a perfect cube: the message must have wrapped around n"
print(m.to_bytes((m.bit_length() + 7) // 8, "big").decode())
