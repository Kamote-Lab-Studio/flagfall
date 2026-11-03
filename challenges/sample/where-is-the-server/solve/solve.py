"""Where Is the Server?: the sticker on the rack says HETZNER ONLINE GMBH,
and the photo still has its EXIF GPS tags (exiftool rack.jpg). They point
to Frankfurt am Main. Needs: pip install pillow"""
import sys
from pathlib import Path

from PIL import Image

dist = Path(sys.argv[1] if len(sys.argv) > 1 else Path(__file__).parent.parent / "dist")
exif = Image.open(dist / "rack.jpg").getexif()
gps = exif.get_ifd(0x8825)


def deg(v, ref):
    d = float(v[0]) + float(v[1]) / 60 + float(v[2]) / 3600
    return -d if ref in ("S", "W") else d


lat, lon = deg(gps[2], gps[1]), deg(gps[4], gps[3])
print(f"camera: {exif.get(0x010F)} {exif.get(0x0110)}; GPS {lat:.4f}, {lon:.4f}")
assert 50.0 < lat < 50.2 and 8.5 < lon < 8.8, "expected Frankfurt am Main"
print("Frankfurt am Main, and the sticker says HETZNER ONLINE:")
print("flag{frankfurt_hetzner}")
