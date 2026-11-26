// Package bigint2 exposes RISC Zero's bigint2 accelerator programs for
// 256-bit modular arithmetic and short Weierstrass curve operations.
//
// Inside a zkVM guest each call runs a precompiled bigint2 program: the host
// supplies the result nondeterministically and a short verification program
// checks it in the circuit. Outside a guest the same operations run on
// math/big, so code built on this package can be tested natively.
//
// Numbers are little-endian arrays of 32-bit words. Every function checks
// that its result is fully reduced, because the bigint2 programs only check
// congruence and a dishonest prover could otherwise return a value at or
// above the modulus. A failed check panics, which makes the guest fail
// rather than commit a wrong journal.
//
// The callers own the preconditions the programs cannot express: ModInv256
// needs a non-zero input, ECAdd256 needs two non-identity points with
// different x coordinates, and ECDouble256 needs a non-identity point with
// a non-zero y coordinate. Every point must lie on the curve.
package bigint2

// Words is the width of a 256-bit number in 32-bit words.
const Words = 8

// Uint256 is a 256-bit number as little-endian 32-bit words.
type Uint256 = [Words]uint32

// Point is an affine point: x then y.
type Point = [2 * Words]uint32

// Curve is a short Weierstrass curve y^2 = x^3 + ax + b: the field prime,
// then a, then b.
type Curve = [3 * Words]uint32

// Less reports whether x < y.
func Less(x, y *Uint256) bool {
	for i := Words - 1; i >= 0; i-- {
		if x[i] != y[i] {
			return x[i] < y[i]
		}
	}
	return false
}

// mustReduced panics unless x < m.
func mustReduced(x, m *Uint256) {
	if !Less(x, m) {
		panic("bigint2: result is not reduced")
	}
}

// pointCoord returns coordinate i of p.
func pointCoord(p *Point, i int) *Uint256 {
	return (*Uint256)(p[i*Words : (i+1)*Words])
}

// curvePrime returns the field prime of c.
func curvePrime(c *Curve) *Uint256 {
	return (*Uint256)(c[:Words])
}

// ModMul256 sets z = x*y mod m. m must be non-zero.
func ModMul256(z, x, y, m *Uint256) {
	modMul256(z, x, y, m)
	mustReduced(z, m)
}

// ModInv256 sets z = x^-1 mod m. m must be prime and x must be non-zero
// mod m.
func ModInv256(z, x, m *Uint256) {
	modInv256(z, x, m)
	mustReduced(z, m)
}

// ECAdd256 sets z = p + q on c. Both points must be on the curve, not the
// identity, and have different x coordinates.
func ECAdd256(z, p, q *Point, c *Curve) {
	ecAdd256(z, p, q, c)
	mustReduced(pointCoord(z, 0), curvePrime(c))
	mustReduced(pointCoord(z, 1), curvePrime(c))
}

// ECDouble256 sets z = 2p on c. p must be on the curve, not the identity,
// and have a non-zero y coordinate.
func ECDouble256(z, p *Point, c *Curve) {
	ecDouble256(z, p, c)
	mustReduced(pointCoord(z, 0), curvePrime(c))
	mustReduced(pointCoord(z, 1), curvePrime(c))
}
