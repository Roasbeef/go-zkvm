//go:build !tinygo.zkvm

package bigint2

import "math/big"

// Accelerated reports whether this build uses the bigint2 accelerator.
func Accelerated() bool {
	return false
}

func toBig(x []uint32) *big.Int {
	var buf [4 * Words]byte
	for i := 0; i < Words; i++ {
		w := x[Words-1-i]
		buf[4*i] = byte(w >> 24)
		buf[4*i+1] = byte(w >> 16)
		buf[4*i+2] = byte(w >> 8)
		buf[4*i+3] = byte(w)
	}
	return new(big.Int).SetBytes(buf[:])
}

func fromBig(z []uint32, x *big.Int) {
	var buf [4 * Words]byte
	x.FillBytes(buf[:])
	for i := 0; i < Words; i++ {
		z[Words-1-i] = uint32(buf[4*i])<<24 | uint32(buf[4*i+1])<<16 |
			uint32(buf[4*i+2])<<8 | uint32(buf[4*i+3])
	}
}

func modMul256(z, x, y, m *Uint256) {
	mod := toBig(m[:])
	r := new(big.Int).Mul(toBig(x[:]), toBig(y[:]))
	fromBig(z[:], r.Mod(r, mod))
}

func modInv256(z, x, m *Uint256) {
	mod := toBig(m[:])
	r := new(big.Int).ModInverse(toBig(x[:]), mod)
	if r == nil {
		panic("bigint2: no modular inverse")
	}
	fromBig(z[:], r)
}

// finish computes x3 = l^2 - x1 - x2 and y3 = l(x1 - x3) - y1 mod p.
func finish(z *Point, l, x1, y1, x2, p *big.Int) {
	x3 := new(big.Int).Mul(l, l)
	x3.Sub(x3, x1).Sub(x3, x2).Mod(x3, p)
	y3 := new(big.Int).Sub(x1, x3)
	y3.Mul(y3, l).Sub(y3, y1).Mod(y3, p)
	fromBig(z[:Words], x3)
	fromBig(z[Words:], y3)
}

func ecAdd256(z, p, q *Point, c *Curve) {
	prime := toBig(c[:Words])
	x1, y1 := toBig(p[:Words]), toBig(p[Words:])
	x2, y2 := toBig(q[:Words]), toBig(q[Words:])
	dx := new(big.Int).Sub(x2, x1)
	dx.Mod(dx, prime)
	inv := new(big.Int).ModInverse(dx, prime)
	if inv == nil {
		panic("bigint2: ec add of points with equal x")
	}
	l := new(big.Int).Sub(y2, y1)
	l.Mul(l, inv).Mod(l, prime)
	finish(z, l, x1, y1, x2, prime)
}

func ecDouble256(z, p *Point, c *Curve) {
	prime := toBig(c[:Words])
	a := toBig(c[Words : 2*Words])
	x1, y1 := toBig(p[:Words]), toBig(p[Words:])
	den := new(big.Int).Lsh(y1, 1)
	den.Mod(den, prime)
	inv := new(big.Int).ModInverse(den, prime)
	if inv == nil {
		panic("bigint2: ec double of point with zero y")
	}
	l := new(big.Int).Mul(x1, x1)
	l.Mul(l, big.NewInt(3)).Add(l, a).Mul(l, inv).Mod(l, prime)
	finish(z, l, x1, y1, x1, prime)
}
