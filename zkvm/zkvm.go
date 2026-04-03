//go:build tinygo.zkvm
// +build tinygo.zkvm

// Package zkvm provides guest-side functions for RISC Zero zkVM
package zkvm

/*
#include <stdint.h>
void sys_write(uint32_t fd, char* buf, int len);
// sys_halt matches Rust's signature - takes exit code and output digest pointer
void sys_halt(uint8_t exit_code, uint32_t* out_state);
void sys_read(uint32_t fd, void* buf, uint32_t len);
uint64_t sys_cycle_count();
void sys_sha_buffer(uint32_t* out_state, uint32_t* in_state, uint8_t* buf, uint32_t count);
*/
import "C"
import (
	"unsafe"
)

const (
	FD_STDIN   = 0
	FD_STDOUT  = 1
	FD_STDERR  = 2
	FD_JOURNAL = 3
)

// Read reads data from the host (stdin)
func Read(buf []byte) {
	if len(buf) > 0 {
		C.sys_read(FD_STDIN, unsafe.Pointer(&buf[0]), C.uint32_t(len(buf)))
	}
}

// ReadValue reads a typed value from the host
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

// Commit commits data to the journal (public output)
func Commit(buf []byte) {
	if len(buf) > 0 {
		C.sys_write(FD_JOURNAL, (*C.char)(unsafe.Pointer(&buf[0])), C.int(len(buf)))
		// Update journal hasher with committed data
		UpdateProperHasher(buf)
	}
}

// CommitValue commits a typed value to the journal
func CommitValue[T any](val *T) {
	size := unsafe.Sizeof(*val)
	C.sys_write(FD_JOURNAL, (*C.char)(unsafe.Pointer(val)), C.int(size))
	// Update journal hasher with committed data
	bytes := (*[1 << 30]byte)(unsafe.Pointer(val))[:size:size]
	UpdateProperHasher(bytes)
}

// Halt exits the program with the given exit code
func Halt(exitCode uint8) {
	// Finalize the journal digest and get output digest
	outputDigest := FinalizeProperHasher()
	// Pass the output digest to the halt syscall (matches Rust's sys_halt signature)
	C.sys_halt(C.uint8_t(exitCode), (*C.uint32_t)(unsafe.Pointer(&outputDigest[0])))
}

// HaltWithDigest exits with a specific digest (for testing)
func HaltWithDigest(exitCode uint8, digest [8]uint32) {
	C.sys_halt(C.uint8_t(exitCode), (*C.uint32_t)(unsafe.Pointer(&digest[0])))
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
