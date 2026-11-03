package main

import (
	"bytes"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/netip"
	"strings"
	"time"
)

// ---------------------------------------------------------------- Packet Archaeology
//
// A workstation browses and resolves names as usual; meanwhile malware
// sends a file out as base32 chunks in DNS query names.

var (
	wsIP   = netip.MustParseAddr("10.20.5.23")
	dnsIP  = netip.MustParseAddr("10.20.5.1")
	wsMAC  = []byte{0x3c, 0x52, 0x82, 0x1a, 0x4f, 0x07}
	gwMAC  = []byte{0x00, 0x1b, 0x21, 0x9e, 0x22, 0x10}
	exfilZ = "t.updates-cdn.example"
)

const exfilFile = `-- notes.txt --
vpn: vpn.northwind.example  user: j.okafor
backup nas: 10.20.9.14 (admin / Winter2026!)
do not lose this: flag{dns_is_not_a_file_transfer}
`

type pcapWriter struct {
	buf bytes.Buffer
	t   time.Time
}

func newPcap(start time.Time) *pcapWriter {
	w := &pcapWriter{t: start}
	// Global header: magic, v2.4, UTC, accuracy, snaplen, Ethernet.
	_ = binary.Write(&w.buf, binary.LittleEndian, []uint32{0xa1b2c3d4, 0x00040002, 0, 0, 65535, 1})
	return w
}

func (w *pcapWriter) packet(after time.Duration, frame []byte) {
	w.t = w.t.Add(after)
	_ = binary.Write(&w.buf, binary.LittleEndian, []uint32{
		uint32(w.t.Unix()), uint32(w.t.Nanosecond() / 1000), uint32(len(frame)), uint32(len(frame)),
	})
	w.buf.Write(frame)
}

func checksum(b []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(b); i += 2 {
		sum += uint32(b[i])<<8 | uint32(b[i+1])
	}
	if len(b)%2 == 1 {
		sum += uint32(b[len(b)-1]) << 8
	}
	for sum > 0xffff {
		sum = sum>>16 + sum&0xffff
	}
	return ^uint16(sum)
}

var ipID uint16 = 0x4a10

// ipFrame wraps a transport segment in IPv4 and Ethernet.
func ipFrame(src, dst netip.Addr, proto byte, seg []byte, outbound bool) []byte {
	ip := make([]byte, 20)
	ip[0], ip[8], ip[9] = 0x45, 64, proto
	binary.BigEndian.PutUint16(ip[2:], uint16(20+len(seg)))
	ipID++
	binary.BigEndian.PutUint16(ip[4:], ipID)
	ip[6] = 0x40 // don't fragment
	s, d := src.As4(), dst.As4()
	copy(ip[12:], s[:])
	copy(ip[16:], d[:])
	binary.BigEndian.PutUint16(ip[10:], checksum(ip))

	// Transport checksum over the pseudo-header.
	pseudo := append(append(append([]byte{}, s[:]...), d[:]...), 0, proto, byte(len(seg)>>8), byte(len(seg)))
	sumAt := 6 // UDP
	if proto == 6 {
		sumAt = 16 // TCP
	}
	binary.BigEndian.PutUint16(seg[sumAt:], 0)
	binary.BigEndian.PutUint16(seg[sumAt:], checksum(append(pseudo, seg...)))

	eth := make([]byte, 14)
	if outbound {
		copy(eth, gwMAC)
		copy(eth[6:], wsMAC)
	} else {
		copy(eth, wsMAC)
		copy(eth[6:], gwMAC)
	}
	eth[12], eth[13] = 0x08, 0x00
	return append(append(eth, ip...), seg...)
}

func udp(sport, dport uint16, payload []byte) []byte {
	h := make([]byte, 8)
	binary.BigEndian.PutUint16(h, sport)
	binary.BigEndian.PutUint16(h[2:], dport)
	binary.BigEndian.PutUint16(h[4:], uint16(8+len(payload)))
	return append(h, payload...)
}

func tcp(sport, dport uint16, seq, ack uint32, flags byte, payload []byte) []byte {
	h := make([]byte, 20)
	binary.BigEndian.PutUint16(h, sport)
	binary.BigEndian.PutUint16(h[2:], dport)
	binary.BigEndian.PutUint32(h[4:], seq)
	binary.BigEndian.PutUint32(h[8:], ack)
	h[12], h[13] = 5<<4, flags
	binary.BigEndian.PutUint16(h[14:], 64240)
	return append(h, payload...)
}

func dnsName(name string) []byte {
	var b []byte
	for _, l := range strings.Split(name, ".") {
		b = append(append(b, byte(len(l))), l...)
	}
	return append(b, 0)
}

// dnsQuery and dnsAnswer build an A query and its answer (nil addr: NXDOMAIN).
func dnsQuery(id uint16, name string) []byte {
	h := []byte{byte(id >> 8), byte(id), 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0}
	return append(append(h, dnsName(name)...), 0, 1, 0, 1)
}

func dnsAnswer(id uint16, name string, addr *netip.Addr) []byte {
	q := dnsQuery(id, name)
	q[2], q[3] = 0x81, 0x80
	if addr == nil {
		q[3] = 0x83 // NXDOMAIN
		return q
	}
	q[7] = 1 // one answer
	a4 := addr.As4()
	ans := []byte{0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0x01, 0x2c, 0, 4}
	return append(append(q, ans...), a4[:]...)
}

func packetArchaeology() {
	r := rng("packet-archaeology")
	w := newPcap(time.Date(2026, 3, 14, 9, 12, 3, 0, time.UTC))
	ms := func(lo, hi int) time.Duration { return time.Duration(lo+r.IntN(hi-lo)) * time.Millisecond }
	dnsID := uint16(0x1f00)
	resolve := func(name string, addr *netip.Addr) {
		dnsID += uint16(1 + r.IntN(40))
		sport := uint16(49152 + r.IntN(16000))
		w.packet(ms(5, 400), ipFrame(wsIP, dnsIP, 17, udp(sport, 53, dnsQuery(dnsID, name)), true))
		w.packet(ms(8, 40), ipFrame(dnsIP, wsIP, 17, udp(53, sport, dnsAnswer(dnsID, name, addr)), false))
	}
	browse := func(host string, ip netip.Addr, path string) {
		a := ip
		resolve(host, &a)
		sport := uint16(50000 + r.IntN(10000))
		seq, ack := r.Uint32(), r.Uint32()
		w.packet(ms(2, 20), ipFrame(wsIP, ip, 6, tcp(sport, 80, seq, 0, 0x02, nil), true))
		w.packet(ms(10, 60), ipFrame(ip, wsIP, 6, tcp(80, sport, ack, seq+1, 0x12, nil), false))
		w.packet(ms(1, 3), ipFrame(wsIP, ip, 6, tcp(sport, 80, seq+1, ack+1, 0x10, nil), true))
		req := []byte(fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64)\r\nAccept: */*\r\n\r\n", path, host))
		w.packet(ms(1, 5), ipFrame(wsIP, ip, 6, tcp(sport, 80, seq+1, ack+1, 0x18, req), true))
		body := "<html><body>ok</body></html>"
		res := []byte(fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Type: text/html\r\nContent-Length: %d\r\n\r\n%s", len(body), body))
		w.packet(ms(20, 120), ipFrame(ip, wsIP, 6, tcp(80, sport, ack+1, seq+1+uint32(len(req)), 0x18, res), false))
		w.packet(ms(1, 5), ipFrame(wsIP, ip, 6, tcp(sport, 80, seq+1+uint32(len(req)), ack+1+uint32(len(res)), 0x11, nil), true))
	}

	benign := []struct {
		host string
		ip   string
		path string
	}{
		{"intranet.northwind.example", "10.20.1.10", "/"},
		{"news.example.com", "93.184.215.34", "/today"},
		{"wiki.northwind.example", "10.20.1.12", "/Onboarding"},
		{"updates.example.org", "198.51.100.40", "/check?v=4.2.1"},
		{"weather.example.net", "203.0.113.80", "/manila"},
	}
	lookups := []string{"time.windows.com", "login.northwind.example", "fonts.example.com", "mail.northwind.example", "cdn.example.com"}

	enc := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte(exfilFile)))
	var chunks []string
	for len(enc) > 0 {
		n := min(30, len(enc))
		chunks = append(chunks, enc[:n])
		enc = enc[n:]
	}

	for i, c := range chunks {
		// Normal traffic between the exfiltration queries.
		if i%2 == 0 {
			b := benign[r.IntN(len(benign))]
			browse(b.host, netip.MustParseAddr(b.ip), b.path)
		} else {
			a := netip.AddrFrom4([4]byte{10, 20, 1, byte(20 + r.IntN(30))})
			resolve(lookups[r.IntN(len(lookups))], &a)
		}
		resolve(fmt.Sprintf("%s.%02d.%s", c, i, exfilZ), nil)
	}
	for _, b := range benign[:3] {
		browse(b.host, netip.MustParseAddr(b.ip), b.path)
	}
	write("packet-archaeology/dist/capture.pcap", w.buf.Bytes())
}
