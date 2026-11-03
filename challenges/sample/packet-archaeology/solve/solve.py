"""Packet Archaeology: the odd DNS queries go to *.t.updates-cdn.example.
Each name is <base32 chunk>.<sequence>.t.updates-cdn.example: sort by
sequence, join the chunks, base32-decode. (In Wireshark: filter
dns.qry.name contains "updates-cdn", then copy the names out.)"""
import base64
import struct
import sys
from pathlib import Path

dist = Path(sys.argv[1] if len(sys.argv) > 1 else Path(__file__).parent.parent / "dist")
raw = (dist / "capture.pcap").read_bytes()
off, chunks = 24, {}
while off < len(raw):
    _, _, incl, _ = struct.unpack("<IIII", raw[off : off + 16])
    frame = raw[off + 16 : off + 16 + incl]
    off += 16 + incl
    if frame[12:14] != b"\x08\x00" or frame[23] != 17:  # IPv4, UDP
        continue
    ihl = (frame[14] & 0x0F) * 4
    udp = frame[14 + ihl :]
    if struct.unpack(">H", udp[2:4])[0] != 53:
        continue
    dns = udp[8:]
    if dns[2] & 0x80:  # a response
        continue
    labels, p = [], 12
    while dns[p]:
        labels.append(dns[p + 1 : p + 1 + dns[p]].decode())
        p += 1 + dns[p]
    name = ".".join(labels)
    if name.endswith(".t.updates-cdn.example"):
        chunks[int(labels[1])] = labels[0]

data = "".join(chunks[i] for i in sorted(chunks)).upper()
data += "=" * (-len(data) % 8)
text = base64.b32decode(data).decode()
print(text)
print(next(line[line.index("flag{") :] for line in text.splitlines() if "flag{" in line))
