package main

import (
	"crypto/ecdh"
	"crypto/sha256"
	"crypto/sha3"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
)

// randInt returns a uniform integer in [0, 2^bits).
func randInt(r *RNG, bits int) *big.Int {
	b := make([]byte, (bits+7)/8)
	_, _ = r.Read(b)
	if extra := len(b)*8 - bits; extra > 0 {
		b[0] &= 0xff >> extra
	}
	return new(big.Int).SetBytes(b)
}

// prime returns a random prime of exactly bits bits for which ok holds.
func prime(r *RNG, bits int, ok func(*big.Int) bool) *big.Int {
	for {
		p := randInt(r, bits)
		p.SetBit(p, bits-1, 1).SetBit(p, bits-2, 1).SetBit(p, 0, 1)
		if p.ProbablyPrime(40) && ok(p) {
			return p
		}
	}
}

// ---------------------------------------------------------------- Baby RSA

func babyRSA() {
	r := rng("baby-rsa")
	three := big.NewInt(3)
	// e = 3 needs p-1 and q-1 not divisible by 3.
	coprime := func(p *big.Int) bool {
		return new(big.Int).Mod(new(big.Int).Sub(p, big.NewInt(1)), three).Sign() != 0
	}
	p, q := prime(r, 1024, coprime), prime(r, 1024, coprime)
	n := new(big.Int).Mul(p, q)
	m := new(big.Int).SetBytes([]byte("flag{cube_root_of_all_evil}"))
	c := new(big.Int).Exp(m, three, n)
	if new(big.Int).Exp(m, three, nil).Cmp(n) >= 0 {
		fail(fmt.Errorf("baby-rsa: m^3 wrapped around n; the challenge would be unsolvable"))
	}
	write("baby-rsa/dist/output.txt", fmt.Appendf(nil, "n = %d\ne = 3\nc = %d\n", n, c))
	write("baby-rsa/dist/chall.py", []byte(babyRSASource))
}

const babyRSASource = `# Baby RSA: how output.txt was made.
from Crypto.Util.number import getPrime, bytes_to_long

FLAG = open("flag.txt", "rb").read().strip()

e = 3
while True:
    p, q = getPrime(1024), getPrime(1024)
    if (p - 1) % e and (q - 1) % e:
        break
n = p * q
m = bytes_to_long(FLAG)  # no padding: it's faster this way
c = pow(m, e, n)

print(f"n = {n}")
print(f"e = {e}")
print(f"c = {c}")
`

// ---------------------------------------------------------------- Lattice Whisper

// p256N is the order of the P-256 base point.
var p256N, _ = new(big.Int).SetString("ffffffff00000000ffffffffffffffffbce6faada7179e84f3b9cac2fc632551", 16)

// p256Mul returns the x coordinate of k*G.
func p256Mul(k *big.Int) *big.Int {
	priv, err := ecdh.P256().NewPrivateKey(k.FillBytes(make([]byte, 32)))
	if err != nil {
		fail(err)
	}
	pub := priv.PublicKey().Bytes() // 04 || X || Y
	return new(big.Int).SetBytes(pub[1:33])
}

const latticeSigs = 60
const latticeNonceBits = 248 // the top 8 bits of every nonce are zero

func latticeWhisper() {
	r := rng("lattice-whisper")
	var d *big.Int
	for d == nil || d.Sign() == 0 || d.Cmp(p256N) >= 0 {
		d = randInt(r, 256)
	}
	type sig struct {
		Msg string `json:"msg"`
		R   string `json:"r"`
		S   string `json:"s"`
	}
	sigs := make([]sig, 0, latticeSigs)
	for i := 0; i < latticeSigs; i++ {
		msg := fmt.Sprintf("telemetry batch %03d: all systems nominal", i)
		h := sha256.Sum256([]byte(msg))
		z := new(big.Int).SetBytes(h[:])
		var k *big.Int
		for k == nil || k.Sign() == 0 {
			k = randInt(r, latticeNonceBits)
		}
		rr := new(big.Int).Mod(p256Mul(k), p256N)
		s := new(big.Int).Mul(rr, d)
		s.Add(s, z).Mul(s, new(big.Int).ModInverse(k, p256N)).Mod(s, p256N)
		if rr.Sign() == 0 || s.Sign() == 0 {
			fail(fmt.Errorf("lattice-whisper: degenerate signature"))
		}
		sigs = append(sigs, sig{Msg: msg, R: rr.Text(16), S: s.Text(16)})
	}
	b, _ := json.MarshalIndent(map[string]any{"curve": "P-256", "hash": "SHA-256", "signatures": sigs}, "", "  ")
	write("lattice-whisper/dist/signatures.json", append(b, '\n'))

	flagText := []byte("flag{hidden_number_problem_solved}")
	ks := sha3.SumSHAKE256(d.FillBytes(make([]byte, 32)), len(flagText))
	for i := range flagText {
		flagText[i] ^= ks[i]
	}
	write("lattice-whisper/dist/flag.enc", []byte(hex.EncodeToString(flagText)+"\n"))
	write("lattice-whisper/dist/chall.py", []byte(latticeSource))
}

const latticeSource = `# Lattice Whisper: how signatures.json and flag.enc were made.
import hashlib, json, secrets
from ecdsa import NIST256p  # pip install ecdsa

G, n = NIST256p.generator, NIST256p.order
d = secrets.randbelow(n - 1) + 1          # the private key

sigs = []
for i in range(60):
    msg = f"telemetry batch {i:03d}: all systems nominal"
    z = int.from_bytes(hashlib.sha256(msg.encode()).digest(), "big")
    k = secrets.randbits(248)             # "random enough"
    r = (k * G).x() % n
    s = pow(k, -1, n) * (z + r * d) % n
    sigs.append({"msg": msg, "r": f"{r:x}", "s": f"{s:x}"})
json.dump({"curve": "P-256", "hash": "SHA-256", "signatures": sigs}, open("signatures.json", "w"), indent=2)

flag = open("flag.txt", "rb").read().strip()
key = hashlib.shake_256(d.to_bytes(32, "big")).digest(len(flag))
open("flag.enc", "w").write(bytes(a ^ b for a, b in zip(flag, key)).hex() + "\n")
`
