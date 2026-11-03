"""Memory Lane: list the processes (the /proc status blocks left in memory).
Kernel workers are children of kthreadd (PPid 2) and have no command line;
one "kworker" has PPid 1, a user ID, and a command line in /dev/shm. Its
environment has STASH=<hex>: the flag XOR'd with its --key."""
import gzip
import re
import sys
from pathlib import Path

dist = Path(sys.argv[1] if len(sys.argv) > 1 else Path(__file__).parent.parent / "dist")
mem = gzip.decompress((dist / "memory.raw.gz").read_bytes())

procs = re.findall(rb"Name:\t([^\n]+)\n.*?\nPid:\t(\d+)\nPPid:\t(\d+)", mem, re.S)
print(f"{len(procs)} processes")
for name, pid, ppid in sorted(procs, key=lambda p: int(p[1])):
    odd = name.startswith(b"kworker") and ppid != b"2"
    print(f"  {int(pid):>5} {int(ppid):>5} {name.decode()}{'   <-- not a kernel thread' if odd else ''}")

cmd = re.search(rb"/dev/shm/[^\x00]*kworker[^\x00]*\x00(?:[^\x00]+\x00)+", mem).group(0).split(b"\x00")
print("cmdline:", b" ".join(cmd).decode())
key = cmd[cmd.index(b"--key") + 1]
stash = bytes.fromhex(re.search(rb"STASH=([0-9a-f]+)", mem).group(1).decode())
print(bytes(b ^ key[i % len(key)] for i, b in enumerate(stash)).decode())
