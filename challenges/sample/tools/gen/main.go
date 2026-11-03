// Command gen builds the downloadable files of the sample challenges
// (their dist/ folders) and the data headers of the C ones. It's
// deterministic: every run writes the same bytes, so the files in git can
// be checked against their source.
//
//	go run ./challenges/sample/tools/gen            (from the repo root)
//
// The C binaries are compiled separately, in Docker, by
// challenges/sample/tools/build-binaries.sh, which runs this first.
package main

import (
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
)

var root = flag.String("dir", "challenges/sample", "the sample pack folder")

// RNG is a deterministic random source: rand.Rand's helpers plus Read.
type RNG struct {
	*rand.Rand
	cc *rand.ChaCha8
}

func (r *RNG) Read(b []byte) (int, error) { return r.cc.Read(b) }

// rng returns the random source for one challenge.
func rng(name string) *RNG {
	var seed [32]byte
	copy(seed[:], "flagfall-sample/"+name)
	cc := rand.NewChaCha8(seed)
	return &RNG{Rand: rand.New(cc), cc: cc}
}

// write saves a file under the sample pack, creating folders.
func write(rel string, data []byte) {
	p := filepath.Join(*root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		fail(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		fail(err)
	}
	fmt.Printf("  %s (%d bytes)\n", rel, len(data))
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gen:", err)
	os.Exit(1)
}

func main() {
	flag.Parse()
	if _, err := os.Stat(filepath.Join(*root, "embed.go")); err != nil {
		fail(fmt.Errorf("%s isn't the sample pack folder (run from the repo root, or pass -dir)", *root))
	}
	for _, step := range []struct {
		name string
		run  func()
	}{
		{"baby-rsa", babyRSA},
		{"lattice-whisper", latticeWhisper},
		{"packet-archaeology", packetArchaeology},
		{"memory-lane", memoryLane},
		{"where-is-the-server", whereIsTheServer},
		{"crackme-v3", crackme},
		{"obfuscated-oracle", oracle},
	} {
		fmt.Println(step.name)
		step.run()
	}
}
