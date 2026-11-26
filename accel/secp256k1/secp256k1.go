// Package secp256k1 implements the secp256k1 operations that Taproot
// verification needs, on top of the bigint2 accelerator: BIP-340 Schnorr
// verification and BIP-341 x-only key tweaking.
//
// Field multiplication and inversion, and affine point addition and
// doubling, run as bigint2 programs inside a zkVM guest. Additions and
// subtractions of field elements run in software, where they are cheaper
// than an accelerator call. Outside a guest the bigint2 package falls back
// to math/big, so this package can be tested against a reference
// implementation natively.
//
// The package verifies public data only. Nothing here is constant time, so
// it must never handle secret keys.
package secp256k1

import (
	"errors"

	"github.com/roasbeef/go-zkvm/accel/bigint2"
	"github.com/roasbeef/go-zkvm/accel/sha256"
)

var (
	// ErrInvalidPubKey is returned for an x coordinate that is not on
	// the curve or is not below the field prime.
	ErrInvalidPubKey = errors.New("secp256k1: invalid x-only public key")

	// ErrInvalidTweak is returned for a tweak at or above the group
	// order.
	ErrInvalidTweak = errors.New("secp256k1: tweak overflows the group " +
		"order")

	// ErrInfinity is returned when a tweaked key is the point at
	// infinity.
	ErrInfinity = errors.New("secp256k1: tweaked key is infinity")
)

// Accelerated reports whether this build uses the zkVM accelerator.
func Accelerated() bool {
	return bigint2.Accelerated()
}

// challengeTag is SHA-256("BIP0340/challenge").
var challengeTag = sha256.Sum256([]byte("BIP0340/challenge"))

// TweakAddXOnly returns the x coordinate and y parity of lift_x(internal) +
// tweak*G, the BIP-341 output key for a tweak.
func TweakAddXOnly(internal, tweak [32]byte) ([32]byte, bool, error) {
	var x word
	wordFromBytes(&x, &internal)
	var p point
	if !liftX(&p, &x) {
		return [32]byte{}, false, ErrInvalidPubKey
	}
	var t word
	wordFromBytes(&t, &tweak)
	if !bigint2.Less(&t, &groupOrder) {
		return [32]byte{}, false, ErrInvalidTweak
	}
	var tg, q point
	scalarBaseMult(&tg, &t)
	add(&q, &p, &tg)
	if q.inf {
		return [32]byte{}, false, ErrInfinity
	}
	var out [32]byte
	wordToBytes(&out, q.x())
	return out, q.y()[0]&1 == 1, nil
}

// IsValidXOnly reports whether pubKey is the x coordinate of a curve point.
func IsValidXOnly(pubKey [32]byte) bool {
	var x word
	wordFromBytes(&x, &pubKey)
	var p point
	return liftX(&p, &x)
}

// VerifySchnorr reports whether sig is a valid BIP-340 signature by the
// x-only key pubKey over msg.
func VerifySchnorr(pubKey [32]byte, msg []byte, sig [64]byte) bool {
	var px word
	wordFromBytes(&px, &pubKey)
	var p point
	if !liftX(&p, &px) {
		return false
	}

	var rBytes, sBytes [32]byte
	copy(rBytes[:], sig[:32])
	copy(sBytes[:], sig[32:])
	var r, s word
	wordFromBytes(&r, &rBytes)
	wordFromBytes(&s, &sBytes)
	if !bigint2.Less(&r, &fieldPrime) || !bigint2.Less(&s, &groupOrder) {
		return false
	}

	h := sha256.New()
	h.Write(challengeTag[:])
	h.Write(challengeTag[:])
	h.Write(rBytes[:])
	h.Write(pubKey[:])
	h.Write(msg)
	var eBytes [32]byte
	copy(eBytes[:], h.Sum(nil))
	var e word
	wordFromBytes(&e, &eBytes)
	reduceOnce(&e, &e, &groupOrder)

	// R = sG - eP = sG + e(-P).
	var sg, negP, ep, rp point
	scalarBaseMult(&sg, &s)
	negate(&negP, &p)
	scalarMult(&ep, &e, &negP)
	add(&rp, &sg, &ep)
	if rp.inf || rp.y()[0]&1 == 1 {
		return false
	}
	return *rp.x() == r
}
