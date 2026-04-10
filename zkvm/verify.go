//go:build tinygo.zkvm
// +build tinygo.zkvm

// verify.go implements the Go guest-side zkVM composition primitive. When a
// guest calls Verify(imageID, journal), it asserts that the host will supply
// a valid unconditional receipt matching that (imageID, journal) pair. The
// host-side risc0 recursion pipeline resolves these assertions during proof
// generation by supplying the actual succinct leaf receipts as assumptions.
//
// The implementation must reproduce exactly the same digest chain as the Rust
// guest SDK's env::verify. That chain is:
//
//  1. Hash the journal bytes into a journalDigest (SHA-256).
//  2. Construct a risc0.SystemState tagged struct for the post-state
//     (all zeros for unconditional receipts).
//  3. Construct a risc0.Output tagged struct from (journalDigest, zeroDigest).
//  4. Construct a risc0.ReceiptClaim tagged struct from
//     (zero, imageID, postState, output, exitCode=0, input=0).
//  5. Pass the resulting claimDigest to sys_verify_integrity with a zero
//     control root (unconditional receipt).
//  6. Update the running assumptions digest so sys_halt's final
//     risc0.Output includes this assumption.
//
// Getting any of these steps wrong causes a silent mismatch between the
// guest-visible and host-visible assumptions lists, which makes proof
// generation fail with an opaque "assumptions mismatch" error.

package zkvm

/*
#include <stdint.h>
void sys_verify_integrity(uint32_t* claim_digest, uint32_t* control_root);
*/
import "C"

import "unsafe"

// Verify records an assumption that there exists a valid unconditional receipt
// for the given guest image ID and public journal bytes.
//
// This is the core composition primitive: the batch aggregation guest calls
// this once per leaf receipt, asserting that a valid proof exists for each
// (leaf_image_id, leaf_journal) pair. The host supplies the actual succinct
// leaf receipts as assumptions, and the risc0 recursion pipeline resolves
// them during proof generation.
func Verify(imageID [32]byte, journal []byte) {
	if !properHasherInitialized {
		InitProperHasher()
	}

	// Reconstruct the exact digest chain that the Rust SDK would
	// produce for env::verify(image_id, journal).
	var zeroDigest [8]uint32
	imageIDWords := digestBytesToWords(imageID)

	// Step 1: Hash the leaf journal into a 256-bit digest.
	journalDigest := shaBuffer(sha256InitStateBE, journal)

	// Step 2: Construct the post-execution system state. For
	// unconditional receipts this is all zeros (no memory image).
	postStateDigest := taggedStructWithData(
		"risc0.SystemState", [][8]uint32{zeroDigest}, []uint32{0},
	)

	// Step 3: Combine journal + assumptions into the output digest.
	// The assumptions slot is zero because we are asserting a simple
	// unconditional receipt (no nested composition within the leaf).
	outputDigest := taggedStruct(
		"risc0.Output",
		[][8]uint32{journalDigest, zeroDigest},
	)

	// Step 4: Build the full receipt claim digest. The field order
	// matches the Rust ReceiptClaim struct: (input, pre_state_digest,
	// post_state_digest, output, exit_code, input).
	claimDigest := taggedStructWithData(
		"risc0.ReceiptClaim",
		[][8]uint32{zeroDigest, imageIDWords, postStateDigest, outputDigest},
		[]uint32{0, 0},
	)

	// Step 5: Register the assumption with the zkVM via the
	// sys_verify_integrity syscall. The host must supply a matching
	// succinct receipt during proof generation.
	C.sys_verify_integrity(
		(*C.uint32_t)(unsafe.Pointer(&claimDigest[0])),
		(*C.uint32_t)(unsafe.Pointer(&zeroDigest[0])),
	)

	// Step 6: Update the running assumptions digest so the final
	// risc0.Output passed to sys_halt reflects this assumption.
	addAssumptionDigest(claimDigest, zeroDigest)
}
