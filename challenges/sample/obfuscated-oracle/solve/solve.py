"""Obfuscated Oracle: each program byte is XOR'd with (0x5a + 13*pos), and
the opcode values are shuffled (learned by reversing the interpreter's
switch; here read from src/ops.h, which has the same numbers). Disassemble,
and the program turns out to check, for each character,
((in[i] ^ k) * m + prev) & 0xff == t, where prev is the previous
character. Solve them in order."""
import re
import struct
import sys
from pathlib import Path

here = Path(__file__).parent.parent
dist = Path(sys.argv[1] if len(sys.argv) > 1 else here / "dist")
defs = (here / "src" / "ops.h").read_text()
ops = {int(v, 16): k for k, v in re.findall(r"#define OP_(\w+) (0x[0-9a-f]+)", defs)}
raw = (dist / "check.vm").read_bytes()
code = bytes(b ^ ((0x5A + 13 * i) & 0xFF) for i, b in enumerate(raw))

prog, pc = [], 0
while pc < len(code):
    op = ops[code[pc]]
    if op == "PUSH":
        prog.append((pc, op, code[pc + 1]))
        pc += 2
    elif op in ("JMP", "JZ", "JNZ"):
        prog.append((pc, op, struct.unpack("<h", code[pc + 1 : pc + 3])[0]))
        pc += 3
    else:
        prog.append((pc, op, None))
        pc += 1
for pc, op, arg in prog[:16]:
    print(f"  {pc:04x}  {op:<5} {'' if arg is None else arg}")
print(f"  ... {len(prog)} instructions")

# Per character: PUSH i, LOAD, PUSH k, XOR, PUSH m, MUL,
# (PUSH 0x33 | PUSH i-1, LOAD), ADD, PUSH t, SUB, JNZ fail
flag, j = [], 4  # skip the length check: LEN, PUSH 25, SUB, JNZ
while prog[j][1] == "PUSH" and prog[j + 1][1] == "LOAD":
    k, m = prog[j + 2][2], prog[j + 4][2]
    j += 6
    if prog[j + 1][1] == "LOAD":
        prev, j = flag[prog[j][2]], j + 2
    else:
        prev, j = prog[j][2], j + 1
    t = prog[j + 1][2]
    j += 4
    flag.append(next(c for c in range(256) if ((c ^ k) * m + prev) & 0xFF == t))
print(bytes(flag).decode())
