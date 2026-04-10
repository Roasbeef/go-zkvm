//go:build tinygo.zkvm
// +build tinygo.zkvm

// This file implements the journal digest finalization for the Go guest layer.
//
// Even though libzkvm_platform.a provides the low-level SHA-256 syscalls
// (sys_sha_compress, sys_sha_buffer), the Go guest layer still needs to:
//   - maintain a running SHA-256 hash state across Commit calls
//   - apply SHA-256 padding when Halt is called
//   - wrap the final journal digest into the risc0.Output tagged struct
//   - pass the result to sys_halt
//
// The Rust guest SDK does this automatically inside its journal implementation.
// Since we are not using the Rust SDK, this Go file reimplements that logic
// using the zkVM SHA acceleration syscalls.
package zkvm

/*
#include <stdint.h>
void sys_sha_buffer(uint32_t* out_state, uint32_t* in_state, uint8_t* buf, uint32_t count);
void sys_sha_compress(uint32_t* out_state, uint32_t* in_state, uint8_t* block1, uint8_t* block2);
*/
import "C"
import (
	"encoding/binary"
	"unsafe"
)

// The platform archive gives us the low-level SHA syscalls, but the Go guest
// layer still has to maintain the running journal hash and construct the final
// risc0.Output digest passed to sys_halt.

const (
	// SHA256_BLOCK_SIZE is the SHA-256 block size in bytes.
	SHA256_BLOCK_SIZE = 64 // 64 bytes per block
	// SHA256_BLOCK_WORDS is the SHA-256 block size in 32-bit words.
	SHA256_BLOCK_WORDS = 16 // 16 words per block
	// WORD_SIZE is the number of bytes in one 32-bit word.
	WORD_SIZE = 4
)

// sha256InitStateBE holds the SHA-256 initial hash values (H0..H7) with
// their bytes swapped to little-endian word order. The risc0 RISC-V SHA
// acceleration operates on 32-bit words in the machine's native little-endian
// byte order, so the standard big-endian SHA-256 constants must be
// byte-swapped before use. For example, H0 = 0x6a09e667 becomes
// 0x67e6096a when its four bytes are reversed.
var sha256InitStateBE = [8]uint32{
	1743128938, // 0x67e6096a - byte-swapped H0 (0x6a09e667)
	2242799547, // 0x85ae67bb - byte-swapped H1 (0xbb67ae85)
	1928556092, // 0x72f36e3c - byte-swapped H2 (0x3c6ef372)
	989155237,  // 0x3af54fa5 - byte-swapped H3 (0xa54ff53a)
	2136084049, // 0x7f520e51 - byte-swapped H4 (0x510e527f)
	2355627419, // 0x8c68059b - byte-swapped H5 (0x9b05688c)
	2883158815, // 0xabd9831f - byte-swapped H6 (0x1f83d9ab)
	432922715,  // 0x19cde05b - byte-swapped H7 (0x5be0cd19)
}

// ProperJournalHasher implements the running journal hash state using the zkVM
// SHA syscalls.
type ProperJournalHasher struct {
	state      [8]uint32
	bufferData [SHA256_BLOCK_SIZE]byte // Single-block buffer for leftover bytes
	bufferLen  int                     // Current length of data in buffer
	totalLen   uint64                  // Total bytes processed
}

var properHasher ProperJournalHasher // Static allocation instead of pointer.
var properHasherInitialized bool

// assumptionsDigestWords accumulates the running assumptions list digest. Each
// call to Verify adds one assumption via addAssumptionDigest, building a
// cons-cell linked list. The final value is embedded in the risc0.Output
// tagged struct passed to sys_halt, so the host can match it against the
// set of succinct receipts supplied as assumptions.
var assumptionsDigestWords [8]uint32

// Aligned temporary buffer for SHA block processing
var shaBlockAligned [SHA256_BLOCK_SIZE]byte

// InitProperHasher initializes the running journal hasher state.
func InitProperHasher() {
	for i := 0; i < 8; i++ {
		properHasher.state[i] = sha256InitStateBE[i]
	}
	properHasher.bufferLen = 0
	properHasher.totalLen = 0
	for i := 0; i < 8; i++ {
		assumptionsDigestWords[i] = 0
	}
	properHasherInitialized = true
}

// UpdateProperHasher adds newly committed journal bytes to the running hash.
func UpdateProperHasher(data []byte) {
	if !properHasherInitialized {
		InitProperHasher()
	}
	properHasher.totalLen += uint64(len(data))

	// 1) If we have buffered bytes, top up to a full block first.
	if properHasher.bufferLen > 0 {
		need := SHA256_BLOCK_SIZE - properHasher.bufferLen
		if need > len(data) {
			need = len(data)
		}
		if need > 0 {
			copy(properHasher.bufferData[properHasher.bufferLen:], data[:need])
			properHasher.bufferLen += need
			data = data[need:]
		}
		if properHasher.bufferLen == SHA256_BLOCK_SIZE {
			processBlock(properHasher.bufferData[:])
			properHasher.bufferLen = 0
		}
	}

	// 2) Process as many full 64-byte blocks from data directly as possible.
	for len(data) >= SHA256_BLOCK_SIZE {
		// If unaligned, copy into local buffer first.
		if uintptr(unsafe.Pointer(&data[0]))&3 != 0 {
			copy(properHasher.bufferData[:], data[:SHA256_BLOCK_SIZE])
			processBlock(properHasher.bufferData[:])
		} else {
			processBlock(data[:SHA256_BLOCK_SIZE])
		}
		data = data[SHA256_BLOCK_SIZE:]
	}

	// 3) Buffer any remaining tail bytes.
	if len(data) > 0 {
		copy(properHasher.bufferData[:], data)
		properHasher.bufferLen = len(data)
	}
}

// processBlock compresses a single 64-byte SHA-256 block into the running
// hash state using the zkVM sys_sha_compress syscall. The block is copied
// into a word-aligned static buffer before the syscall because the RISC-V
// SHA acceleration requires 4-byte alignment.
func processBlock(block []byte) {
	// Ensure contiguous, word-aligned block by copying into a static buffer
	for i := 0; i < SHA256_BLOCK_SIZE; i++ {
		shaBlockAligned[i] = block[i]
	}
	var outState [8]uint32
	C.sys_sha_compress(
		(*C.uint32_t)(unsafe.Pointer(&outState[0])),
		(*C.uint32_t)(unsafe.Pointer(&properHasher.state[0])),
		(*C.uint8_t)(unsafe.Pointer(&shaBlockAligned[0])),
		(*C.uint8_t)(unsafe.Pointer(&shaBlockAligned[32])),
	)
	for i := 0; i < 8; i++ {
		properHasher.state[i] = outState[i]
	}
}

// FinalizeProperHasher completes the padded journal hash and returns the final
// tagged `risc0.Output` digest passed to `sys_halt`.
func FinalizeProperHasher() [8]uint32 {
	if !properHasherInitialized {
		InitProperHasher()
	}

	// Clone current state for finalization
	finalState := properHasher.state

	// Prepare padding
	remaining := properHasher.bufferData[:properHasher.bufferLen]

	// Compute padding length in bytes
	padLen := computePaddedLength(len(remaining))
	padWords := padLen / 4

	// Create buffer of u32s to ensure word aligned
	padBuf := make([]uint32, padWords)
	padBufu8 := (*[1 << 30]byte)(unsafe.Pointer(&padBuf[0]))[:padLen:padLen]

	// Copy remaining bytes
	if len(remaining) > 0 {
		copy(padBufu8, remaining)
	}

	// Add 0x80 byte (end marker)
	padBufu8[len(remaining)] = 0x80

	// Add trailer with number of bits written. This needs to be big endian.
	// Only use low 32 bits for 32-bit architecture
	bitsTrailer := uint32(properHasher.totalLen * 8)

	// Swap bits to BE
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, bitsTrailer)
	bitsTrailer = binary.LittleEndian.Uint32(b)

	padBuf[padWords-1] = bitsTrailer

	// Process final padded block(s)
	var journalDigest [8]uint32
	if padWords > 0 {
		numBlocks := padWords / 16 // 16 words per block
		C.sys_sha_buffer(
			(*C.uint32_t)(unsafe.Pointer(&journalDigest[0])),
			(*C.uint32_t)(unsafe.Pointer(&finalState[0])),
			(*C.uint8_t)(unsafe.Pointer(&padBufu8[0])),
			C.uint32_t(numBlocks), // Number of 64-byte blocks
		)
	} else {
		journalDigest = finalState
	}

	// Create the "risc0.Output" tagged struct
	// This combines the journal digest with the assumptions digest
	outputDigest := taggedStruct(
		"risc0.Output",
		[][8]uint32{journalDigest, assumptionsDigestWords},
	)

	// Return the output digest instead of trying to write to halt_output_digest
	return outputDigest
}

// SumSHA256 computes a plain SHA-256 digest using the guest-side accelerated
// SHA path and returns the result as raw digest bytes.
func SumSHA256(data []byte) [32]byte {
	return digestWordsToBytes(shaBuffer(sha256InitStateBE, data))
}

// computePaddedLength computes the total padded message length in bytes for
// SHA-256. The padding consists of a 0x80 end marker, zero-fill to the next
// block boundary minus 8 bytes, and a 64-bit big-endian bit count trailer.
// The result is always a multiple of SHA256_BLOCK_SIZE (64 bytes).
func computePaddedLength(dataLen int) int {
	const WordSize = 4
	const BlockWords = 16
	// Add one byte for end marker
	nWords := alignUp(dataLen+1, WordSize) / WordSize
	// Add two words for length at end (even though we only
	// use one of them, being a 32-bit architecture)
	nWords += 2

	nWords = alignUp(nWords, BlockWords)
	return nWords * WordSize
}

// alignUp rounds addr up to the nearest multiple of al.
func alignUp(addr, al int) int {
	return (addr + al - 1) & ^(al - 1)
}

// taggedStruct creates a tagged struct digest as defined by the risc0 spec.
// The tagged struct convention hashes:
//
//	SHA256(SHA256(tag_string) || digest_0 || digest_1 || ... || count_u16_le)
//
// This is how risc0 creates domain-separated composite digests. For example,
// "risc0.Output" combines the journal digest and assumptions digest into the
// final output digest that sys_halt expects.
func taggedStruct(tag string, digests [][8]uint32) [8]uint32 {
	return taggedStructWithData(tag, digests, nil)
}

// taggedStructWithData creates a tagged struct digest as defined by the risc0
// spec, including any trailing little-endian scalar words.
func taggedStructWithData(
	tag string, digests [][8]uint32, data []uint32,
) [8]uint32 {
	// Hash the tag string
	tagDigest := shaBuffer(sha256InitStateBE, []byte(tag))

	// Create buffer for all bytes to hash
	// Tag digest (32 bytes) + digests (32 bytes each) + words (4 bytes each)
	// + count (2 bytes)
	allBytes := make([]byte, 0, 32+len(digests)*32+len(data)*4+2)

	// Add tag digest
	for _, word := range tagDigest {
		allBytes = binary.LittleEndian.AppendUint32(allBytes, word)
	}

	// Add all digests
	for _, digest := range digests {
		for _, word := range digest {
			allBytes = binary.LittleEndian.AppendUint32(allBytes, word)
		}
	}

	for _, word := range data {
		allBytes = binary.LittleEndian.AppendUint32(allBytes, word)
	}

	// Add count of digests as u16 in little-endian
	allBytes = binary.LittleEndian.AppendUint16(allBytes, uint16(len(digests)))

	return shaBuffer(sha256InitStateBE, allBytes)
}

// shaBuffer computes the SHA-256 hash of data using the zkVM sys_sha_compress
// syscall, starting from the given initial hash state. This handles padding
// internally and returns the final 8-word hash state.
func shaBuffer(initialState [8]uint32, bytes []byte) [8]uint32 {
	var outState [8]uint32

	// Compute padding
	padLen := computePaddedLength(len(bytes))
	padWords := padLen / 4

	// Create buffer of u32s to ensure word aligned
	padBuf := make([]uint32, padWords)
	padBufu8 := (*[1 << 30]byte)(unsafe.Pointer(&padBuf[0]))[:padLen:padLen]

	// Copy bytes
	if len(bytes) > 0 {
		copy(padBufu8, bytes)
	}

	// Add 0x80 marker
	padBufu8[len(bytes)] = 0x80

	// Add trailer with number of bits written (big endian)
	bitsTrailer := uint32(len(bytes) * 8)
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, bitsTrailer)
	bitsTrailer = binary.LittleEndian.Uint32(b)

	padBuf[padWords-1] = bitsTrailer

	// Hash per block using sys_sha_compress
	blocks := padWords / 16
	inState := initialState
	for i := 0; i < blocks; i++ {
		base := i * SHA256_BLOCK_SIZE
		for j := 0; j < SHA256_BLOCK_SIZE; j++ {
			shaBlockAligned[j] = padBufu8[base+j]
		}
		C.sys_sha_compress(
			(*C.uint32_t)(unsafe.Pointer(&outState[0])),
			(*C.uint32_t)(unsafe.Pointer(&inState[0])),
			(*C.uint8_t)(unsafe.Pointer(&shaBlockAligned[0])),
			(*C.uint8_t)(unsafe.Pointer(&shaBlockAligned[32])),
		)
		inState = outState
	}

	return outState
}

// digestWordsToBytes converts an 8-word SHA-256 state (little-endian word
// order, as used by the risc0 SHA acceleration) into a 32-byte digest.
func digestWordsToBytes(words [8]uint32) [32]byte {
	var digest [32]byte
	for i, word := range words {
		binary.LittleEndian.PutUint32(digest[i*4:], word)
	}

	return digest
}

// digestBytesToWords converts a 32-byte digest into the 8-word little-endian
// representation expected by the risc0 tagged-struct and SHA acceleration APIs.
func digestBytesToWords(digest [32]byte) [8]uint32 {
	var words [8]uint32
	for i := 0; i < len(words); i++ {
		words[i] = binary.LittleEndian.Uint32(digest[i*4:])
	}

	return words
}

// taggedListCons prepends one element to a risc0 tagged linked list. The risc0
// assumptions list is built as a right-folded cons list:
//
//	cons(assumption_0, cons(assumption_1, ... cons(assumption_N, nil)))
//
// Each cons cell is a "risc0.Assumptions" tagged struct with (head, tail)
// digests. The empty list is the all-zero digest.
func taggedListCons(tag string, head, tail [8]uint32) [8]uint32 {
	return taggedStruct(tag, [][8]uint32{head, tail})
}

// addAssumptionDigest adds one verified assumption to the running assumptions
// list. Each assumption is a "risc0.Assumption" tagged struct containing the
// receipt claim digest and control root (zero for unconditional receipts). The
// running list uses cons-cell construction so the final assumptions digest
// reflects all Verify calls made during the guest's execution. The resulting
// digest is embedded in the risc0.Output passed to sys_halt, allowing the
// host's recursion pipeline to match guest-side assumptions against the
// supplied succinct leaf receipts.
func addAssumptionDigest(claimDigest, controlRoot [8]uint32) {
	assumptionDigest := taggedStruct(
		"risc0.Assumption",
		[][8]uint32{claimDigest, controlRoot},
	)
	assumptionsDigestWords = taggedListCons(
		"risc0.Assumptions",
		assumptionDigest,
		assumptionsDigestWords,
	)
}
