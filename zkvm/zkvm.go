//go:build tinygo.zkvm
// +build tinygo.zkvm

// Package zkvm provides guest-side functions for programs running inside the
// RISC Zero zkVM. Guest code uses this package to read private witness data
// from the host, commit public claims to the proof journal, and control the
// guest lifecycle.
//
// The key distinction is privacy: data read via Read/ReadValue comes from
// the host-supplied stdin and is private witness material that never appears
// in the proof. Data written via Commit/CommitValue enters the public journal
// and becomes part of the verifiable proof output.
//
// This package also owns the journal finalization logic: each Commit call
// updates a running SHA-256 hash, and Halt finalizes that hash into the
// tagged risc0.Output digest that the prover uses to bind the journal to
// the receipt.
package zkvm

/*
#include <stdint.h>
void sys_write(uint32_t fd, char* buf, int len);
// sys_halt matches Rust's signature - takes exit code and output digest pointer
void sys_halt(uint8_t exit_code, uint32_t* out_state);
void sys_read(uint32_t fd, void* buf, uint32_t len);
uint64_t sys_cycle_count();
*/
import "C"
import (
	"unsafe"
)

const (
	// FD_STDIN is the private guest input stream. Data read from this fd
	// comes from the host-supplied witness and never appears in the proof.
	FD_STDIN = 0

	// FD_STDOUT is the private guest stdout stream. Writes here are
	// visible to the host during execution but are not part of the proof.
	FD_STDOUT = 1

	// FD_STDERR is the private guest stderr stream. Used for debug
	// tracing during development; not part of the proof.
	FD_STDERR = 2

	// FD_JOURNAL is the public journal stream. Writes here become the
	// verifiable proof output committed into the receipt.
	FD_JOURNAL = 3
)

// Read reads data from the host-supplied private witness stream (stdin).
// The data read here is part of the private witness and never appears in
// the proof journal.
func Read(buf []byte) {
	if len(buf) > 0 {
		C.sys_read(FD_STDIN, unsafe.Pointer(&buf[0]), C.uint32_t(len(buf)))
	}
}

// ReadValue reads a typed value from the host private witness stream. The
// value is read as raw bytes matching the in-memory layout of T.
//
// NOTE: This uses an unsafe cast from the pointer to a byte span of
// sizeof(T). The caller must ensure proper alignment for T.
func ReadValue[T any](val *T) {
	size := unsafe.Sizeof(*val)
	C.sys_read(FD_STDIN, unsafe.Pointer(val), C.uint32_t(size))
}

// Write writes data to the host (stdout) - private output
func Write(buf []byte) {
	if len(buf) > 0 {
		C.sys_write(FD_STDOUT, (*C.char)(unsafe.Pointer(&buf[0])), C.int(len(buf)))
	}
}

// WriteStderr writes debug data to stderr
func WriteStderr(buf []byte) {
	if len(buf) > 0 {
		C.sys_write(FD_STDERR, (*C.char)(unsafe.Pointer(&buf[0])), C.int(len(buf)))
	}
}

// Commit writes data to the public journal and updates the running journal
// hash. This performs two operations: (1) writes the raw bytes to FD_JOURNAL
// so they appear in the receipt journal, and (2) feeds the same bytes into
// the running SHA-256 hasher that will produce the final output digest on
// Halt.
func Commit(buf []byte) {
	if len(buf) > 0 {
		C.sys_write(FD_JOURNAL, (*C.char)(unsafe.Pointer(&buf[0])), C.int(len(buf)))
		UpdateProperHasher(buf)
	}
}

// CommitValue commits a typed value to the public journal. Like Commit, this
// both writes the raw bytes to FD_JOURNAL and updates the running hasher.
//
// NOTE: The value is reinterpreted as a byte slice via an unsafe pointer
// cast. This assumes T has a fixed, predictable memory layout (no pointers
// or padding-sensitive fields).
func CommitValue[T any](val *T) {
	size := unsafe.Sizeof(*val)
	C.sys_write(FD_JOURNAL, (*C.char)(unsafe.Pointer(val)), C.int(size))
	bytes := (*[1 << 30]byte)(unsafe.Pointer(val))[:size:size]
	UpdateProperHasher(bytes)
}

// Halt exits the guest program with the given exit code. Before halting, it
// finalizes the running journal hash into a tagged risc0.Output digest and
// passes that digest to sys_halt. The chain is:
//
//  1. Finalize the SHA-256 journal hash (add padding, compute final state).
//  2. Wrap the journal digest + empty assumptions digest into a
//     taggedStruct("risc0.Output", ...) digest.
//  3. Pass the output digest to sys_halt, which the prover uses to bind the
//     journal contents to the receipt.
func Halt(exitCode uint8) {
	outputDigest := FinalizeProperHasher()
	C.sys_halt(C.uint8_t(exitCode), (*C.uint32_t)(unsafe.Pointer(&outputDigest[0])))
}

// CycleCount returns the current cycle count
func CycleCount() uint64 {
	return uint64(C.sys_cycle_count())
}

// Print writes a string to stdout (private)
func Print(s string) {
	Write([]byte(s))
}

// Debug writes a string to stderr for debugging
func Debug(s string) {
	WriteStderr([]byte(s))
}
