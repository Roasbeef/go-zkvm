package secp256k1

import (
	"encoding/csv"
	"encoding/hex"
	"math/big"
	"math/rand"
	"os"
	"testing"
)

// TestBIP340Vectors runs the official BIP-340 verification vectors from
// bitcoin/bips bip-0340/test-vectors.csv.
func TestBIP340Vectors(t *testing.T) {
	f, err := os.Open("testdata/bip340-vectors.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows[1:] {
		pk, _ := hex.DecodeString(row[2])
		msg, _ := hex.DecodeString(row[4])
		sigBytes, _ := hex.DecodeString(row[5])
		var pub [32]byte
		var sig [64]byte
		copy(pub[:], pk)
		copy(sig[:], sigBytes)
		want := row[6] == "TRUE"
		if got := VerifySchnorr(pub, msg, sig); got != want {
			t.Fatalf("vector %s: got %v want %v (%s)", row[0], got,
				want, row[7])
		}
	}
}

var (
	refP = mustHex("fffffffffffffffffffffffffffffffffffffffffffffffffffff" +
		"ffefffffc2f")
	refN = mustHex("fffffffffffffffffffffffffffffffebaaedce6af48a03bbfd25" +
		"e8cd0364141")
	refGX = mustHex("79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f28" +
		"15b16f81798")
	refGY = mustHex("483ada7726a3c4655da4fbfc0e1108a8fd17b448a68554199c47d" +
		"08ffb10d4b8")
)

func mustHex(s string) *big.Int {
	v, ok := new(big.Int).SetString(s, 16)
	if !ok {
		panic(s)
	}
	return v
}

// refPoint is a math/big affine point; nil x means infinity.
type refPoint struct{ x, y *big.Int }

func refAdd(a, b refPoint) refPoint {
	if a.x == nil {
		return b
	}
	if b.x == nil {
		return a
	}
	var l *big.Int
	if a.x.Cmp(b.x) == 0 {
		if a.y.Cmp(b.y) != 0 || a.y.Sign() == 0 {
			return refPoint{}
		}
		l = new(big.Int).Mul(a.x, a.x)
		l.Mul(l, big.NewInt(3))
		den := new(big.Int).Lsh(a.y, 1)
		l.Mul(l, den.ModInverse(den, refP))
	} else {
		l = new(big.Int).Sub(b.y, a.y)
		den := new(big.Int).Sub(b.x, a.x)
		den.Mod(den, refP)
		l.Mul(l, den.ModInverse(den, refP))
	}
	l.Mod(l, refP)
	x := new(big.Int).Mul(l, l)
	x.Sub(x, a.x).Sub(x, b.x).Mod(x, refP)
	y := new(big.Int).Sub(a.x, x)
	y.Mul(y, l).Sub(y, a.y).Mod(y, refP)
	return refPoint{x, y}
}

func refMul(k *big.Int, p refPoint) refPoint {
	acc := refPoint{}
	for i := k.BitLen() - 1; i >= 0; i-- {
		acc = refAdd(acc, acc)
		if k.Bit(i) == 1 {
			acc = refAdd(acc, p)
		}
	}
	return acc
}

func refLiftX(x *big.Int) (refPoint, bool) {
	c := new(big.Int).Exp(x, big.NewInt(3), refP)
	c.Add(c, big.NewInt(7)).Mod(c, refP)
	y := new(big.Int).ModSqrt(c, refP)
	if y == nil {
		return refPoint{}, false
	}
	if y.Bit(0) == 1 {
		y.Sub(refP, y)
	}
	return refPoint{x, y}, true
}

func bytes32(v *big.Int) [32]byte {
	var out [32]byte
	v.FillBytes(out[:])
	return out
}

// TestTweakMatchesReference compares TweakAddXOnly against a math/big
// implementation for random keys and tweaks, plus edge-case tweaks.
func TestTweakMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	g := refPoint{refGX, refGY}
	tweaks := []*big.Int{
		big.NewInt(0), big.NewInt(1), big.NewInt(2),
		new(big.Int).Sub(refN, big.NewInt(1)),
	}
	for i := 0; i < 40; i++ {
		tweaks = append(tweaks, new(big.Int).Rand(rng, refN))
	}
	for i, tweak := range tweaks {
		k := new(big.Int).Rand(rng, refN)
		k.Add(k, big.NewInt(1))
		internal := refMul(k, g)
		lifted, ok := refLiftX(internal.x)
		if !ok {
			t.Fatal("reference lift failed")
		}
		want := refAdd(lifted, refMul(tweak, g))

		got, odd, err := TweakAddXOnly(bytes32(internal.x),
			bytes32(tweak))
		if err != nil {
			t.Fatalf("tweak %d: %v", i, err)
		}
		if got != bytes32(want.x) || odd != (want.y.Bit(0) == 1) {
			t.Fatalf("tweak %d mismatch", i)
		}
	}

	// A tweak that cancels the internal key gives infinity: with P =
	// lift_x(G.x) = G, tweak n-1 yields G - G.
	_, _, err := TweakAddXOnly(bytes32(refGX),
		bytes32(new(big.Int).Sub(refN, big.NewInt(1))))
	if err != ErrInfinity {
		t.Fatalf("expected infinity, got %v", err)
	}
	if _, _, err := TweakAddXOnly(bytes32(refGX), bytes32(refN)); err !=
		ErrInvalidTweak {

		t.Fatalf("expected invalid tweak, got %v", err)
	}
	if _, _, err := TweakAddXOnly(bytes32(refP), [32]byte{1}); err !=
		ErrInvalidPubKey {

		t.Fatalf("expected invalid key, got %v", err)
	}
}

// TestSqrtMatchesReference checks feSqrt on squares and non-squares.
func TestSqrtMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for i := 0; i < 50; i++ {
		v := new(big.Int).Rand(rng, refP)
		var x, z word
		b := bytes32(v)
		wordFromBytes(&x, &b)
		ok := feSqrt(&z, &x)
		want := new(big.Int).ModSqrt(v, refP) != nil
		if ok != want {
			t.Fatalf("sqrt %d: got %v want %v", i, ok, want)
		}
	}
}

// TestGeneratorTable checks the generated table against doubling.
func TestGeneratorTable(t *testing.T) {
	p := refPoint{refGX, refGY}
	for i := 0; i < 256; i++ {
		entry := generatorPowers[i]
		var bx, by [32]byte
		wordToBytes(&bx, entry.x())
		wordToBytes(&by, entry.y())
		if bx != bytes32(p.x) || by != bytes32(p.y) {
			t.Fatalf("table entry %d mismatch", i)
		}
		if !isOnCurve(&entry) {
			t.Fatalf("table entry %d off curve", i)
		}
		p = refAdd(p, p)
	}
}

// TestScalarMultMatchesReference checks the windowed multiply against the
// reference for random and edge-case scalars.
func TestScalarMultMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	g := refPoint{refGX, refGY}
	scalars := []*big.Int{
		big.NewInt(0), big.NewInt(1), big.NewInt(15), big.NewInt(16),
		new(big.Int).Sub(refN, big.NewInt(1)),
	}
	for i := 0; i < 20; i++ {
		scalars = append(scalars, new(big.Int).Rand(rng, refN))
	}
	base := refMul(big.NewInt(12345), g)
	var p point
	bx, by := bytes32(base.x), bytes32(base.y)
	var x, y word
	wordFromBytes(&x, &bx)
	wordFromBytes(&y, &by)
	p.setXY(&x, &y)
	for i, k := range scalars {
		var kw word
		kb := bytes32(k)
		wordFromBytes(&kw, &kb)
		var got point
		scalarMult(&got, &kw, &p)
		want := refMul(k, base)
		if want.x == nil {
			if !got.inf {
				t.Fatalf("scalar %d: want infinity", i)
			}
			continue
		}
		var gx [32]byte
		wordToBytes(&gx, got.x())
		if got.inf || gx != bytes32(want.x) {
			t.Fatalf("scalar %d mismatch", i)
		}
	}
}
