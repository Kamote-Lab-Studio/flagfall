package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"strings"
)

// ---------------------------------------------------------------- Where Is the Server?
//
// A drawn "photo" of a server rack. The asset sticker names the provider;
// the EXIF GPS tags (the sysadmin's phone) put it in Frankfurt.

func whereIsTheServer() {
	r := rng("where-is-the-server")
	const w, h = 900, 1200
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fill := func(x0, y0, x1, y1 int, c color.RGBA) {
		draw.Draw(img, image.Rect(x0, y0, x1, y1), &image.Uniform{c}, image.Point{}, draw.Src)
	}
	// Cold aisle: floor tiles and a dim back wall.
	fill(0, 0, w, h, color.RGBA{38, 42, 48, 255})
	for y := 980; y < h; y += 60 {
		for x := 0; x < w; x += 60 {
			g := uint8(150 + r.IntN(12))
			fill(x+1, y+1, x+59, y+59, color.RGBA{g, g, g + 6, 255})
		}
	}
	// The rack frame.
	fill(150, 60, 750, 1000, color.RGBA{18, 18, 20, 255})
	fill(170, 80, 730, 980, color.RGBA{28, 29, 32, 255})
	// Servers: 1U and 2U boxes with drive bays and status LEDs.
	y := 100
	for y < 900 {
		units := 1 + r.IntN(2)
		hgt := 30 * units
		fill(180, y, 720, y+hgt-3, color.RGBA{58, 60, 66, 255})
		for bx := 200; bx < 560; bx += 36 {
			fill(bx, y+5, bx+30, y+hgt-8, color.RGBA{40, 42, 46, 255})
			fill(bx+3, y+8, bx+6, y+11, color.RGBA{0, 200, 90, 255})
		}
		led := color.RGBA{0, 220, 100, 255}
		if r.IntN(9) == 0 {
			led = color.RGBA{255, 160, 0, 255}
		}
		fill(690, y+8, 698, y+16, led)
		fill(670, y+8, 678, y+16, color.RGBA{40, 120, 255, 255})
		y += hgt + 2
	}
	// Blanking panel with the asset sticker, a bit crooked like real ones.
	fill(180, 900, 720, 978, color.RGBA{50, 52, 56, 255})
	fill(232, 905, 668, 973, color.RGBA{236, 232, 220, 255})
	text(img, 246, 912, 2, "PROPERTY OF HETZNER ONLINE GMBH", color.RGBA{180, 20, 30, 255})
	text(img, 246, 934, 2, "RACK R14  U22  ASSET 0047213", color.RGBA{30, 30, 30, 255})
	text(img, 246, 958, 1, "DO NOT POWER OFF WITHOUT A TICKET", color.RGBA{30, 30, 30, 255})
	// A label strip on the rack door.
	fill(760, 120, 860, 160, color.RGBA{230, 230, 230, 255})
	text(img, 770, 132, 2, "R14", color.RGBA{20, 20, 20, 255})

	// Camera noise, so it looks less like a drawing.
	b := img.Bounds()
	for i := 0; i < 60000; i++ {
		x, yy := r.IntN(b.Dx()), r.IntN(b.Dy())
		c := img.RGBAAt(x, yy)
		d := uint8(r.IntN(10))
		img.SetRGBA(x, yy, color.RGBA{sat(c.R, d), sat(c.G, d), sat(c.B, d), 255})
	}

	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 88}); err != nil {
		fail(err)
	}
	jpg := out.Bytes()
	// Insert the EXIF block right after the SOI marker.
	exif := exifBlock()
	final := append(append(append([]byte{}, jpg[:2]...), exif...), jpg[2:]...)
	write("where-is-the-server/dist/rack.jpg", final)
}

func sat(v, d uint8) uint8 {
	if int(v)+int(d) > 255 {
		return 255
	}
	return v + d
}

// exifBlock is an APP1 segment with camera tags and GPS coordinates for
// Frankfurt am Main (50°06'38.5"N 8°40'55.2"E).
func exifBlock() []byte {
	type entry struct {
		tag, typ uint16
		count    uint32
		data     []byte // inline when 4 bytes or less
	}
	ascii := func(s string) []byte { return append([]byte(s), 0) }
	rationals := func(v ...uint32) []byte {
		b := make([]byte, 8*len(v)/2)
		for i := 0; i < len(v); i += 2 {
			binary.BigEndian.PutUint32(b[4*i:], v[i])
			binary.BigEndian.PutUint32(b[4*i+4:], v[i+1])
		}
		return b
	}
	be := binary.BigEndian
	// layout writes one IFD at offset off (from the TIFF header) and
	// returns it with its out-of-line data appended.
	layout := func(off uint32, entries []entry) []byte {
		ifd := make([]byte, 2+12*len(entries)+4)
		be.PutUint16(ifd, uint16(len(entries)))
		var extra []byte
		dataAt := off + uint32(len(ifd))
		for i, e := range entries {
			p := ifd[2+12*i:]
			be.PutUint16(p, e.tag)
			be.PutUint16(p[2:], e.typ)
			be.PutUint32(p[4:], e.count)
			if len(e.data) <= 4 {
				copy(p[8:], e.data)
			} else {
				be.PutUint32(p[8:], dataAt+uint32(len(extra)))
				extra = append(extra, e.data...)
				if len(extra)%2 == 1 {
					extra = append(extra, 0)
				}
			}
		}
		return append(ifd, extra...)
	}
	const (
		typByte, typASCII, typLong, typRational = 1, 2, 4, 5
	)
	ifd0Entries := []entry{
		{0x010f, typASCII, 7, ascii("Google")},
		{0x0110, typASCII, 8, ascii("Pixel 8")},
		{0x0132, typASCII, 20, ascii("2026:02:11 14:37:09")},
		{0x8825, typLong, 1, nil}, // GPS IFD offset, filled below
	}
	ifd0 := layout(8, ifd0Entries)
	gpsOff := 8 + uint32(len(ifd0))
	// Point GPSInfo at the GPS IFD (entry 4, value field).
	be.PutUint32(ifd0[2+12*3+8:], gpsOff)
	gps := layout(gpsOff, []entry{
		{0x0000, typByte, 4, []byte{2, 3, 0, 0}},
		{0x0001, typASCII, 2, ascii("N")},
		{0x0002, typRational, 3, rationals(50, 1, 6, 1, 3852, 100)},
		{0x0003, typASCII, 2, ascii("E")},
		{0x0004, typRational, 3, rationals(8, 1, 40, 1, 5520, 100)},
	})
	tiff := append([]byte("MM\x00\x2a\x00\x00\x00\x08"), append(ifd0, gps...)...)
	payload := append([]byte("Exif\x00\x00"), tiff...)
	seg := []byte{0xff, 0xe1, 0, 0}
	be.PutUint16(seg[2:], uint16(len(payload)+2))
	return append(seg, payload...)
}

// text draws s with a 5x7 bitmap font, scaled.
func text(img *image.RGBA, x, y, scale int, s string, c color.RGBA) {
	for _, ch := range strings.ToUpper(s) {
		rows, ok := glyphs[ch]
		if ok {
			for ry, row := range rows {
				for rx := 0; rx < 5; rx++ {
					if row&(1<<(4-rx)) != 0 {
						draw.Draw(img, image.Rect(x+rx*scale, y+ry*scale, x+(rx+1)*scale, y+(ry+1)*scale), &image.Uniform{c}, image.Point{}, draw.Src)
					}
				}
			}
		}
		x += 6 * scale
	}
}

// glyphs is a classic 5x7 font: 7 rows of 5 bits.
var glyphs = map[rune][7]uint8{
	'A': {14, 17, 17, 31, 17, 17, 17}, 'B': {30, 17, 17, 30, 17, 17, 30}, 'C': {14, 17, 16, 16, 16, 17, 14},
	'D': {30, 17, 17, 17, 17, 17, 30}, 'E': {31, 16, 16, 30, 16, 16, 31}, 'F': {31, 16, 16, 30, 16, 16, 16},
	'G': {14, 17, 16, 23, 17, 17, 15}, 'H': {17, 17, 17, 31, 17, 17, 17}, 'I': {14, 4, 4, 4, 4, 4, 14},
	'J': {7, 2, 2, 2, 2, 18, 12}, 'K': {17, 18, 20, 24, 20, 18, 17}, 'L': {16, 16, 16, 16, 16, 16, 31},
	'M': {17, 27, 21, 21, 17, 17, 17}, 'N': {17, 17, 25, 21, 19, 17, 17}, 'O': {14, 17, 17, 17, 17, 17, 14},
	'P': {30, 17, 17, 30, 16, 16, 16}, 'Q': {14, 17, 17, 17, 21, 18, 13}, 'R': {30, 17, 17, 30, 20, 18, 17},
	'S': {15, 16, 16, 14, 1, 1, 30}, 'T': {31, 4, 4, 4, 4, 4, 4}, 'U': {17, 17, 17, 17, 17, 17, 14},
	'V': {17, 17, 17, 17, 17, 10, 4}, 'W': {17, 17, 17, 21, 21, 21, 10}, 'X': {17, 17, 10, 4, 10, 17, 17},
	'Y': {17, 17, 17, 10, 4, 4, 4}, 'Z': {31, 1, 2, 4, 8, 16, 31},
	'0': {14, 17, 19, 21, 25, 17, 14}, '1': {4, 12, 4, 4, 4, 4, 14}, '2': {14, 17, 1, 2, 4, 8, 31},
	'3': {31, 2, 4, 2, 1, 17, 14}, '4': {2, 6, 10, 18, 31, 2, 2}, '5': {31, 16, 30, 1, 1, 17, 14},
	'6': {6, 8, 16, 30, 17, 17, 14}, '7': {31, 1, 2, 4, 8, 8, 8}, '8': {14, 17, 17, 14, 17, 17, 14},
	'9': {14, 17, 17, 15, 1, 2, 12}, '-': {0, 0, 0, 31, 0, 0, 0}, '.': {0, 0, 0, 0, 0, 12, 12},
}
