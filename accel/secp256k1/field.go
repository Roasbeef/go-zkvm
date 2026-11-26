package secp256k1

import (
	"math/bits"

	"github.com/roasbeef/go-zkvm/accel/bigint2"
)

// word is a 256-bit number as little-endian 32-bit words.
type word = bigint2.Uint256

var (
	// fieldPrime is p = 2^256 - 2^32 - 977.
	fieldPrime = word{
		0xfffffc2f, 0xfffffffe, 0xffffffff, 0xffffffff,
		0xffffffff, 0xffffffff, 0xffffffff, 0xffffffff,
	}

	// groupOrder is n, the order of the generator.
	groupOrder = word{
		0xd0364141, 0xbfd25e8c, 0xaf48a03b, 0xbaaedce6,
		0xfffffffe, 0xffffffff, 0xffffffff, 0xffffffff,
	}

	// curveParams is p, a = 0, b = 7 in the layout the bigint2 curve
	// programs read.
	curveParams = bigint2.Curve{
		0xfffffc2f, 0xfffffffe, 0xffffffff, 0xffffffff,
		0xffffffff, 0xffffffff, 0xffffffff, 0xffffffff,
		0, 0, 0, 0, 0, 0, 0, 0,
		7, 0, 0, 0, 0, 0, 0, 0,
	}

	seven = word{7}
)

// wordFromBytes decodes a big-endian 32-byte number.
func wordFromBytes(z *word, b *[32]byte) {
	for i := 0; i < 8; i++ {
		j := 28 - 4*i
		z[i] = uint32(b[j])<<24 | uint32(b[j+1])<<16 |
			uint32(b[j+2])<<8 | uint32(b[j+3])
	}
}

// wordToBytes encodes x as a big-endian 32-byte number.
func wordToBytes(b *[32]byte, x *word) {
	for i := 0; i < 8; i++ {
		j := 28 - 4*i
		b[j] = byte(x[i] >> 24)
		b[j+1] = byte(x[i] >> 16)
		b[j+2] = byte(x[i] >> 8)
		b[j+3] = byte(x[i])
	}
}

func isZero(x *word) bool {
	var acc uint32
	for _, w := range x {
		acc |= w
	}
	return acc == 0
}

// modAdd sets z = x + y mod m for reduced x and y.
func modAdd(z, x, y, m *word) {
	var sum word
	var carry uint32
	for i := 0; i < 8; i++ {
		sum[i], carry = bits.Add32(x[i], y[i], carry)
	}
	if carry != 0 || !bigint2.Less(&sum, m) {
		var borrow uint32
		for i := 0; i < 8; i++ {
			sum[i], borrow = bits.Sub32(sum[i], m[i], borrow)
		}
	}
	*z = sum
}

// modSub sets z = x - y mod m for reduced x and y.
func modSub(z, x, y, m *word) {
	var diff word
	var borrow uint32
	for i := 0; i < 8; i++ {
		diff[i], borrow = bits.Sub32(x[i], y[i], borrow)
	}
	if borrow != 0 {
		var carry uint32
		for i := 0; i < 8; i++ {
			diff[i], carry = bits.Add32(diff[i], m[i], carry)
		}
	}
	*z = diff
}

// reduceOnce sets z = x mod m for x < 2m, which holds for any 256-bit x
// and m = n.
func reduceOnce(z, x, m *word) {
	*z = *x
	if !bigint2.Less(z, m) {
		var borrow uint32
		for i := 0; i < 8; i++ {
			z[i], borrow = bits.Sub32(z[i], m[i], borrow)
		}
	}
}

func feMul(z, x, y *word) { bigint2.ModMul256(z, x, y, &fieldPrime) }

func feSqr(z, x *word) { bigint2.ModMul256(z, x, x, &fieldPrime) }

// feSqrN squares x n times into z.
func feSqrN(z, x *word, n int) {
	*z = *x
	for i := 0; i < n; i++ {
		feSqr(z, z)
	}
}

// feSqrt sets z to a square root of x and reports whether x is a square.
// It raises x to (p+1)/4 with libsecp256k1's addition chain: 253 squarings
// and 13 multiplications.
func feSqrt(z, x *word) bool {
	var x2, x3, x6, x9, x11, x22, x44, x88, x176, x220, x223, t word

	feSqr(&x2, x)
	feMul(&x2, &x2, x)
	feSqr(&x3, &x2)
	feMul(&x3, &x3, x)
	feSqrN(&x6, &x3, 3)
	feMul(&x6, &x6, &x3)
	feSqrN(&x9, &x6, 3)
	feMul(&x9, &x9, &x3)
	feSqrN(&x11, &x9, 2)
	feMul(&x11, &x11, &x2)
	feSqrN(&x22, &x11, 11)
	feMul(&x22, &x22, &x11)
	feSqrN(&x44, &x22, 22)
	feMul(&x44, &x44, &x22)
	feSqrN(&x88, &x44, 44)
	feMul(&x88, &x88, &x44)
	feSqrN(&x176, &x88, 88)
	feMul(&x176, &x176, &x88)
	feSqrN(&x220, &x176, 44)
	feMul(&x220, &x220, &x44)
	feSqrN(&x223, &x220, 3)
	feMul(&x223, &x223, &x3)

	feSqrN(&t, &x223, 23)
	feMul(&t, &t, &x22)
	feSqrN(&t, &t, 6)
	feMul(&t, &t, &x2)
	feSqrN(&t, &t, 2)

	var check word
	feSqr(&check, &t)
	*z = t
	return check == *x
}
