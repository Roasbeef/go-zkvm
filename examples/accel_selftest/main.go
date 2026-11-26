// Package main checks the accel packages inside the zkVM and reports their
// cycle costs. It hashes fixed inputs, runs the BIP-340 verification
// vectors and a few x-only tweaks, and compares each result with values
// computed natively by gen/main.go. It commits one byte, 1 if every check
// passed, and prints the cycle count of each step to stdout.
//
// Regenerate the vectors with:
//
//	go run ./examples/accel_selftest/gen > examples/accel_selftest/vectors.go
package main

import (
	"encoding/hex"

	"github.com/roasbeef/go-zkvm/accel/secp256k1"
	"github.com/roasbeef/go-zkvm/accel/sha256"
	"github.com/roasbeef/go-zkvm/zkvm"
)

type shaVector struct {
	n   int
	sum string
}

type schnorrVector struct {
	pub, msg, sig string
	valid         bool
}

type tweakVector struct {
	internal, tweak, out string
	odd                  bool
}

func decode32(s string) [32]byte {
	var out [32]byte
	b, _ := hex.DecodeString(s)
	copy(out[:], b)
	return out
}

func report(name string, start uint64) {
	cycles := zkvm.CycleCount() - start
	zkvm.Print(name + " cycles=" + itoa(cycles) + "\n")
}

func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

func main() {
	ok := true

	for _, v := range shaVectors {
		data := make([]byte, v.n)
		for i := range data {
			data[i] = byte(i*7 + 3)
		}
		start := zkvm.CycleCount()
		sum := sha256.Sum256(data)
		report("sha256 len="+itoa(uint64(v.n)), start)
		if hex.EncodeToString(sum[:]) != v.sum {
			zkvm.Print("FAIL sha256\n")
			ok = false
		}
	}

	for i, v := range schnorrVectors {
		pub := decode32(v.pub)
		msg, _ := hex.DecodeString(v.msg)
		var sig [64]byte
		raw, _ := hex.DecodeString(v.sig)
		copy(sig[:], raw)
		start := zkvm.CycleCount()
		got := secp256k1.VerifySchnorr(pub, msg, sig)
		report("schnorr vector="+itoa(uint64(i)), start)
		if got != v.valid {
			zkvm.Print("FAIL schnorr\n")
			ok = false
		}
	}

	for _, v := range tweakVectors {
		start := zkvm.CycleCount()
		out, odd, err := secp256k1.TweakAddXOnly(decode32(v.internal),
			decode32(v.tweak))
		report("tweak", start)
		if err != nil || out != decode32(v.out) || odd != v.odd {
			zkvm.Print("FAIL tweak\n")
			ok = false
		}
	}

	result := []byte{0}
	if ok {
		result[0] = 1
	}
	zkvm.Commit(result)
	zkvm.Halt(0)
}
