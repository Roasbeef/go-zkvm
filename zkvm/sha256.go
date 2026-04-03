//go:build tinygo.zkvm && dummy_sha
// +build tinygo.zkvm,dummy_sha

// This file contains a dummy SHA256 implementation for testing
// Use sha256_proper.go for real SHA256 computation

package zkvm

/*
#include <stdint.h>
extern uint32_t halt_output_digest[8];
*/
import "C"

// SHA256 initial hash values - already BE to LE converted for RISC-V
// From the sort.go example - these are the correct converted values
var sha256InitState = [8]uint32{
	1743128938, // 0x67e6096a - byte-swapped 0x6a09e667
	2242799547, // 0x85ae67bb - byte-swapped 0xbb67ae85
	1928556092, // 0x72f36e3c - byte-swapped 0x3c6ef372
	989155237,  // 0x3af54fa5 - byte-swapped 0xa54ff53a
	2136084049, // 0x7f520e51 - byte-swapped 0x510e527f
	2355627419, // 0x8c68059b - byte-swapped 0x9b05688c
	2883158815, // 0xabd9831f - byte-swapped 0x1f83d9ab
	432922715,  // 0x19cde05b - byte-swapped 0x5be0cd19
}

// JournalHasher tracks SHA256 digest of journal data
type JournalHasher struct {
	state     [8]uint32
	buffer    [64]byte // SHA256 block size
	bufferLen int
	totalLen  uint64
}

var journalHasher JournalHasher

// InitJournalHasher initializes the journal hasher with SHA256 initial state
func InitJournalHasher() {
	for i := 0; i < 8; i++ {
		journalHasher.state[i] = sha256InitState[i]
	}
	journalHasher.bufferLen = 0
	journalHasher.totalLen = 0
}

// UpdateJournalHasher updates the hasher with new data
// For now, this is a simplified version that just tracks the data
// TODO: Implement full SHA256 or use ecall_sha
func UpdateJournalHasher(data []byte) {
	journalHasher.totalLen += uint64(len(data))

	// For now, just XOR the data into the state as a simple checksum
	// This will produce a non-zero digest to test the mechanism
	for i, b := range data {
		journalHasher.state[i%8] ^= uint32(b)
	}
}

// FinalizeJournalHasher updates the halt_output_digest in assembly
// and returns the digest
func FinalizeJournalHasher() {
	// For testing, create a simple non-zero digest
	// In a real implementation, we'd compute SHA256 properly
	for i := 0; i < 8; i++ {
		// Set a pattern that shows we wrote data
		if journalHasher.totalLen > 0 {
			C.halt_output_digest[i] = C.uint32_t(0x12340000 | uint32(i))
		} else {
			C.halt_output_digest[i] = 0
		}
	}
}

func init() {
	InitJournalHasher()
}
