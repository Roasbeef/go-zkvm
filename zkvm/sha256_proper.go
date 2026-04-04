//go:build tinygo.zkvm
// +build tinygo.zkvm

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

const (
	SHA256_BLOCK_SIZE  = 64 // 64 bytes per block
	SHA256_BLOCK_WORDS = 16 // 16 words per block
	WORD_SIZE          = 4
)

// SHA256 initial state - already byte-swapped for RISC-V little-endian
// These values are from the sort.go example
var sha256InitStateBE = [8]uint32{
	1743128938, // 0x67e6096a - byte-swapped 0x6a09e667
	2242799547, // 0x85ae67bb - byte-swapped 0xbb67ae85
	1928556092, // 0x72f36e3c - byte-swapped 0x3c6ef372
	989155237,  // 0x3af54fa5 - byte-swapped 0xa54ff53a
	2136084049, // 0x7f520e51 - byte-swapped 0x510e527f
	2355627419, // 0x8c68059b - byte-swapped 0x9b05688c
	2883158815, // 0xabd9831f - byte-swapped 0x1f83d9ab
	432922715,  // 0x19cde05b - byte-swapped 0x5be0cd19
}

// ProperJournalHasher implements SHA256 using sys_sha_buffer
type ProperJournalHasher struct {
	state      [8]uint32
	bufferData [SHA256_BLOCK_SIZE]byte // Single-block buffer for leftover bytes
	bufferLen  int                     // Current length of data in buffer
	totalLen   uint64                  // Total bytes processed
}

var properHasher ProperJournalHasher // Static allocation instead of pointer
var properHasherInitialized bool

// Aligned temporary buffer for SHA block processing
var shaBlockAligned [SHA256_BLOCK_SIZE]byte

// InitProperHasher initializes with SHA256 initial state
func InitProperHasher() {
	for i := 0; i < 8; i++ {
		properHasher.state[i] = sha256InitStateBE[i]
	}
	properHasher.bufferLen = 0
	properHasher.totalLen = 0
	properHasherInitialized = true
}

// UpdateProperHasher adds data to the hash
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

// processBlock processes a single 64-byte block
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

// FinalizeProperHasher completes the hash with padding
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
	assumptionsDigest := [8]uint32{0, 0, 0, 0, 0, 0, 0, 0} // Empty assumptions
	outputDigest := taggedStruct("risc0.Output", [][8]uint32{journalDigest, assumptionsDigest})

	// Return the output digest instead of trying to write to halt_output_digest
	return outputDigest
}

// computePaddedLength computes the padded length in bytes
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

func alignUp(addr, al int) int {
	return (addr + al - 1) & ^(al - 1)
}

// swapEndian converts little-endian to big-endian (or vice versa)
func swapEndian(val uint32) uint32 {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, val)
	return binary.LittleEndian.Uint32(b)
}

// taggedStruct creates a tagged struct digest as per RISC Zero spec
func taggedStruct(tag string, digests [][8]uint32) [8]uint32 {
	// Hash the tag string
	tagDigest := shaBuffer(sha256InitStateBE, []byte(tag))

	// Create buffer for all bytes to hash
	// Tag digest (32 bytes) + digests (32 bytes each) + count (2 bytes)
	allBytes := make([]byte, 0, 32+len(digests)*32+2)

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

	// Add count of digests as u16 in little-endian
	allBytes = binary.LittleEndian.AppendUint16(allBytes, uint16(len(digests)))

	return shaBuffer(sha256InitStateBE, allBytes)
}

// shaBuffer computes SHA256 of data using sys_sha_buffer
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
// Note: InitProperHasher is called lazily when needed, not in init()
