// Package sha256 provides a SHA-256 implementation that uses the RISC Zero
// SHA-256 accelerator when compiled for a zkVM guest, and the standard
// library implementation otherwise.
//
// Inside a guest, a software SHA-256 compression costs several thousand
// cycles per 64-byte block. The accelerator processes the same block for a
// small constant number of cycles, so hash-heavy relations such as Merkle
// path checks get much cheaper. The API mirrors crypto/sha256, so callers
// can switch by changing an import.
//
// The accelerated digest keeps its chaining state in the byte order the
// accelerator expects: each state word holds four digest bytes in memory
// order, so the state's bytes are the big-endian digest once the last block
// is processed.
package sha256

const (
	// Size is the size of a SHA-256 checksum in bytes.
	Size = 32

	// BlockSize is the block size of SHA-256 in bytes.
	BlockSize = 64
)
