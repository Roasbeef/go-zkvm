//go:build !tinygo.zkvm

package sha256

import (
	"crypto/sha256"
	"hash"
)

// New returns a new hash.Hash computing the SHA-256 checksum.
func New() hash.Hash {
	return sha256.New()
}

// Sum256 returns the SHA-256 checksum of the data.
func Sum256(data []byte) [Size]byte {
	return sha256.Sum256(data)
}

// Accelerated reports whether this build uses the zkVM accelerator.
func Accelerated() bool {
	return false
}
