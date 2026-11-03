"""Crackme v3: the check is acc = rol1(acc) ^ serial[i] ^ TABLE[i % 16],
compared with EXPECT[i] after each step (acc starts at 0x5a). It's
invertible: serial[i] = EXPECT[i] ^ rol1(EXPECT[i-1]) ^ TABLE[i % 16].
Both tables are in .rodata; here they're read from src/data.h, which has
the same bytes. Feed the serial to the binary to get the flag."""
import re
from pathlib import Path

text = (Path(__file__).parent.parent / "src" / "data.h").read_text()


def arr(name):
    body = re.search(name + r"\[[^\]]*\] = \{([^}]*)\}", text).group(1)
    return [int(x, 16) for x in body.split(",")]


table, expect = arr("TABLE"), arr("EXPECT")


def rol1(v):
    return ((v << 1) | (v >> 7)) & 0xFF


prev, serial = 0x5A, ""
for i, e in enumerate(expect):
    serial += chr(e ^ rol1(prev) ^ table[i % 16])
    prev = e
print(serial)
