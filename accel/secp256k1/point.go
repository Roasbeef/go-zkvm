package secp256k1

import "github.com/roasbeef/go-zkvm/accel/bigint2"

// point is an affine point or the identity.
type point struct {
	xy  bigint2.Point
	inf bool
}

func (p *point) x() *word { return (*word)(p.xy[:8]) }

func (p *point) y() *word { return (*word)(p.xy[8:]) }

// setXY sets p to the affine point (x, y).
func (p *point) setXY(x, y *word) {
	copy(p.xy[:8], x[:])
	copy(p.xy[8:], y[:])
	p.inf = false
}

// double sets z = 2p.
func double(z, p *point) {
	// The secp256k1 group has prime order, so no point has y = 0.
	if p.inf || isZero(p.y()) {
		z.inf = true
		return
	}
	var r point
	bigint2.ECDouble256(&r.xy, &p.xy, &curveParams)
	*z = r
}

// add sets z = p + q, handling the identity and equal x coordinates, which
// the bigint2 addition program cannot.
func add(z, p, q *point) {
	switch {
	case p.inf:
		*z = *q
		return
	case q.inf:
		*z = *p
		return
	}
	if *p.x() == *q.x() {
		if *p.y() == *q.y() {
			double(z, p)
		} else {
			z.inf = true
		}
		return
	}
	var r point
	bigint2.ECAdd256(&r.xy, &p.xy, &q.xy, &curveParams)
	*z = r
}

// negate sets z = -p.
func negate(z, p *point) {
	*z = *p
	if p.inf {
		return
	}
	var zero word
	modSub(z.y(), &zero, p.y(), &fieldPrime)
}

// bit returns bit i of k.
func bit(k *word, i int) bool {
	return (k[i/32]>>(uint(i)%32))&1 == 1
}

// scalarBaseMult sets z = kG with the precomputed table of 2^i G, so it
// needs one addition per set bit and no doublings.
func scalarBaseMult(z *point, k *word) {
	acc := point{inf: true}
	for i := 0; i < 256; i++ {
		if bit(k, i) {
			add(&acc, &acc, &generatorPowers[i])
		}
	}
	*z = acc
}

// scalarMult sets z = kp with a fixed 4-bit window: a table of p..15p,
// then four doublings and at most one addition per nibble of k.
func scalarMult(z *point, k *word, p *point) {
	var table [16]point
	table[0].inf = true
	table[1] = *p
	for i := 2; i < 16; i++ {
		add(&table[i], &table[i-1], p)
	}
	acc := point{inf: true}
	for i := 63; i >= 0; i-- {
		double(&acc, &acc)
		double(&acc, &acc)
		double(&acc, &acc)
		double(&acc, &acc)
		nibble := (k[i/8] >> (uint(i%8) * 4)) & 0xf
		if nibble != 0 {
			add(&acc, &acc, &table[nibble])
		}
	}
	*z = acc
}

// isOnCurve reports whether p satisfies y^2 = x^3 + 7 with reduced
// coordinates.
func isOnCurve(p *point) bool {
	if p.inf {
		return false
	}
	if !bigint2.Less(p.x(), &fieldPrime) ||
		!bigint2.Less(p.y(), &fieldPrime) {

		return false
	}
	var lhs, rhs word
	feSqr(&lhs, p.y())
	feSqr(&rhs, p.x())
	feMul(&rhs, &rhs, p.x())
	modAdd(&rhs, &rhs, &seven, &fieldPrime)
	return lhs == rhs
}

// liftX returns the point with x coordinate x and an even y coordinate, as
// BIP-340 defines it.
func liftX(p *point, x *word) bool {
	if !bigint2.Less(x, &fieldPrime) {
		return false
	}
	var c, y word
	feSqr(&c, x)
	feMul(&c, &c, x)
	modAdd(&c, &c, &seven, &fieldPrime)
	if !feSqrt(&y, &c) {
		return false
	}
	if y[0]&1 == 1 {
		modSub(&y, &word{}, &y, &fieldPrime)
	}
	p.setXY(x, &y)
	return true
}
