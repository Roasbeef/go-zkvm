# go-zkvm Tutorial

This tutorial walks through building Go programs that run inside a zero-knowledge
virtual machine. We start from a blank screen and end with a real cryptographic
proof application. Each chapter builds on the one before it, so read them in
order.

---

## Chapter 1: Foundations

A zkVM is a virtual machine whose execution produces a cryptographic proof. You
run a program inside it, and the VM gives back two things: the program's public
output, and a proof that the output was produced by running that exact program on
some input. Anyone can check the proof without re-running the program and without
seeing the input.

RISC Zero's zkVM is one concrete implementation of this idea. It emulates a
RISC-V CPU and uses STARK-based proofs. The proof system is post-quantum secure
and transparent -- there is no trusted setup ceremony. The guest program is a
normal RISC-V ELF binary. The prover executes every instruction, records the
execution trace, and produces a receipt -- a bundle containing the public journal
output and a cryptographic seal. The verifier checks the seal against the guest's
image ID (a hash of the binary) and the journal. If the check passes, the
verifier knows that the claimed program really did produce the claimed output.

### How risc0 Works (Briefly)

The proof lifecycle has four phases:

1. **Build.** Compile your guest program to a RISC-V ELF binary and package it
   with the risc0 kernel.
2. **Execute.** The zkVM runs the guest on private input, recording an execution
   trace -- a table of every CPU state transition across every cycle.
3. **Prove.** The STARK prover transforms the execution trace into a
   cryptographic proof. This is the expensive step. The proof size is
   polylogarithmic in the trace length, so a million-cycle execution produces a
   proof that is only marginally larger than a thousand-cycle execution.
4. **Verify.** Anyone with the receipt, the image ID, and the journal can check
   the proof. Verification is fast -- milliseconds, not minutes -- and requires
   no access to the private input.

The receipt is the artifact that travels between prover and verifier. It contains
the journal (public output), the seal (STARK proof), and metadata including the
image ID. The verifier does not need the guest binary itself -- just the image
ID, which is a 32-byte hash they can obtain from a trusted source or compute
themselves from the binary.

### Why Go Guests Matter

Rust is the first-class guest language in risc0. But most Bitcoin and Lightning
Network tooling is written in Go: `btcd`, `lnd`, `btcutil`, `btcec`. If we want
to prove statements about Bitcoin key derivation, transaction construction, or
signature verification, we need those Go libraries available inside the VM. That
is what this project provides.

### Guest vs. Host

There are always two programs involved in a zkVM proof:

- **Guest code** runs inside the VM. It reads private input, performs
  computation, and writes public output. The proof covers every instruction the
  guest executes.
- **Host code** runs outside the VM, on your normal machine. It compiles the
  guest, feeds private input into the VM, drives the prover, and hands the
  receipt to a verifier.

The guest is a cross-compiled RISC-V binary. The host is a normal Go (or Rust)
program that calls into the risc0 proving engine.

### The Privacy Model

The VM exposes two communication channels to the guest:

- **stdin** (fd 0) is the private witness stream. The host writes secret data
  here before execution. The guest reads it during execution. The data never
  appears in the proof.
- **journal** (fd 3) is the public claim stream. Anything the guest writes here
  becomes part of the verifiable output. The verifier sees these bytes.

The guest also has stdout (fd 1) and stderr (fd 2) for debug output, but those
are private -- visible to the host during execution, not part of the proof.

This separation is the entire point. The guest can read a secret, compute on it,
and commit only a derived public value. The proof guarantees the computation was
done correctly without revealing the secret.

### Motivation: Seed Lifting

The paper "Protecting Quantum Procrastinators with Signature Lifting" motivates
the `bip32-pq-zkp` application built on top of this toolkit. The core idea: a
Bitcoin HD wallet owner can prove they know a BIP-32 seed that derives to a
specific Taproot output key, without ever revealing the seed. The proof is a
STARK receipt. If a quantum computer eventually threatens ECDSA/Schnorr
signatures, the wallet owner has a pre-existing proof of key ownership that does
not depend on the classical signature scheme.

That application is the subject of Chapter 7. For now, we start with something
much simpler.

### What This Repo Provides

Getting a non-Rust language onto the risc0 zkVM requires solving four problems
that Rust guests get "for free":

1. A compilation target that matches the zkVM memory map.
2. Access to the guest syscall layer (`sys_read`, `sys_write`, `sys_halt`, etc.).
3. A way to package the user ELF with the risc0 kernel ELF.
4. A host that can feed private witness data and verify receipts.

This repo supplies all four pieces:

- A TinyGo fork (`tinygo-zkvm`) with a `zkvm-platform` target.
- A Go guest API (`zkvm/`) that wraps the syscalls.
- An R0BF packer (`convert_to_r0bf.go`) for creating guest binaries.
- A Go host package (`host/`) backed by the Rust proving engine via FFI.

The sibling `risc0` checkout provides the platform archive and kernel ELF. The
sibling `tinygo-zkvm` checkout provides the compiler. See
[go-zkvm-overview.md](go-zkvm-overview.md) for the full architectural picture.

---

## Chapter 2: Your First Guest

The simplest possible guest program lives in `examples/simple/main.go`:

```go
package main

import "github.com/roasbeef/go-zkvm/zkvm"

func main() {
	zkvm.Print("Hello from Go zkVM!\n")
	zkvm.Halt(0)
}
```

That is the entire program. Two lines of real work. We will take them apart.

### `zkvm.Print`

`Print` writes a string to stdout (fd 1). This is *private* debug output. The
host process can see it during execution, but it never appears in the proof
journal. Think of it as `fmt.Println` for the VM -- useful during development,
invisible to verifiers.

Under the hood, `Print` converts the string to a byte slice and calls
`sys_write(FD_STDOUT, ...)`, a zkVM syscall provided by the platform library.

### `zkvm.Halt`

`Halt` exits the guest with an exit code. Exit code 0 means success. But `Halt`
does more than just exit -- it finalizes the journal digest and passes it to
`sys_halt`. We will cover the digest machinery in Chapter 5. For now, the
important thing is: **every guest must call `Halt`**. If your guest returns from
`main()` without calling `Halt`, the VM runtime will halt for you, but the
journal digest will not be finalized correctly and your proof will have an empty
or broken journal.

### No Journal Output

This guest does not call `Commit` or `CommitValue`. That means nothing is written
to the public journal. The proof says: "this program ran successfully." It does
not say anything else. The receipt journal will be empty, which is fine -- some
proofs just need to attest that a computation completed without error.

### Building the Guest

The build pipeline has three stages:

1. **Compile with TinyGo.** The `tinygo-zkvm` fork provides a `zkvm-platform`
   target that produces a RISC-V ELF matching the risc0 guest memory map.

2. **Link with `libzkvm_platform.a`.** This upstream risc0 archive provides the
   syscall implementations (`sys_write`, `sys_halt`, `sys_read`, etc.) that the
   `zkvm` package calls through cgo.

3. **Pack with `convert_to_r0bf.go`.** The risc0 executor expects a combined
   binary containing both the user ELF and the `v1compat` kernel ELF. The packer
   writes them into an R0BF (RISC Zero Binary Format) container.

From the repo root:

```bash
make simple
```

That single target does all three steps and produces `simple.bin`. The Makefile
invocation expands to:

```bash
# Step 1+2: compile and link.
tinygo build -target=zkvm-platform -scheduler=none -no-debug \
  -ldflags='-extldflags=/path/to/libzkvm_platform.a' \
  -o simple.elf ./examples/simple

# Step 3: pack with kernel.
go run ./convert_to_r0bf.go simple.elf v1compat.elf simple.bin
```

### What convert_to_r0bf Does

The R0BF (RISC Zero Binary Format) packer is worth understanding because it
explains why there are two ELF files involved. The risc0 executor does not load
a bare user ELF directly. It expects a combined container with this layout:

```
[magic: "R0BF" 4B] [format_version: u32_le]
[header_len: u32_le] [header: KV pairs]
[user_elf_len: u32_le] [user_elf: bytes]
[kernel_elf: bytes (remaining)]
```

The user ELF is your TinyGo-compiled guest. The kernel ELF is `v1compat.elf`,
the risc0 kernel that provides the initial execution environment before your
guest code starts running. They are paired together so the executor can load
both halves from a single file.

### Running the Guest

Execute without proving (fast, good for development):

```bash
cd go-guest-host
cargo run --release -- ../simple.bin --raw-journal --execute-only
```

Generate a full STARK proof:

```bash
cd go-guest-host
cargo run --release -- ../simple.bin --raw-journal
```

Or use the Go host package (Chapter 6 covers this in detail):

```bash
CGO_ENABLED=1 go test ./host -run TestHostFFISimpleGuest -v
```

See [running.md](running.md) for the full prerequisite setup, including building
the TinyGo fork and the risc0 platform archive.

---

## Chapter 3: Private Input, Public Output

The multiply example in `examples/multiply/main.go` is where things get
interesting:

```go
package main

import "github.com/roasbeef/go-zkvm/zkvm"

func main() {
	var a, b uint64
	zkvm.ReadValue(&a)
	zkvm.ReadValue(&b)

	if a == 0 || b == 0 {
		zkvm.Debug("Error: Zero factor\n")
		zkvm.Halt(1)
	}
	if a == 1 || b == 1 {
		zkvm.Debug("Error: Trivial factors\n")
		zkvm.Halt(1)
	}

	product := a * b

	if product/a != b {
		zkvm.Debug("Error: Integer overflow\n")
		zkvm.Halt(1)
	}

	zkvm.CommitValue(&product)

	zkvm.Print("Successfully computed product\n")
	zkvm.Halt(0)
}
```

### `ReadValue`: Private Witness Input

```go
func ReadValue[T any](val *T) {
	size := unsafe.Sizeof(*val)
	C.sys_read(FD_STDIN, unsafe.Pointer(val), C.uint32_t(size))
}
```

`ReadValue` is a generic function that reads `sizeof(T)` bytes from the private
witness stream (fd 0) directly into the pointed-to value. The bytes come from the
host-supplied stdin buffer and are never part of the proof. Here we read two
`uint64` values -- 8 bytes each, little-endian, because RISC-V is
little-endian.

The host side is responsible for writing those 16 bytes into the stdin buffer
before execution starts. The reference CLI in `go-guest-host/` has built-in
sample witness data for each example. When using the Go `host` package, you pass
the witness bytes in `ExecuteRequest.Stdin` or `ProveRequest.Stdin`.

### `CommitValue`: Public Journal Output

```go
func CommitValue[T any](val *T) {
	size := unsafe.Sizeof(*val)
	C.sys_write(FD_JOURNAL, (*C.char)(unsafe.Pointer(val)), C.int(size))
	bytes := (*[1 << 30]byte)(unsafe.Pointer(val))[:size:size]
	UpdateProperHasher(bytes)
}
```

`CommitValue` does two things:

1. Writes the raw bytes of the value to the journal stream (fd 3). These bytes
   become the verifiable public output.
2. Feeds the same bytes into a running SHA-256 hasher. This is critical for
   journal integrity -- more on this in Chapter 5.

In the multiply example, only the product is committed. The verifier sees `391`
(the default sample product) but never learns the factors `17` and `23`.

### The Proof Model

After proving, the receipt contains:

- The **image ID**: a deterministic hash of the guest binary. It tells the
  verifier exactly which program ran.
- The **journal**: the raw bytes committed by `CommitValue`. Here, 8 bytes
  encoding the `uint64` product.
- The **seal**: the STARK proof that the guest with that image ID, when run on
  *some* private input, produced exactly that journal.

The verifier checks the seal against the image ID and the journal. If the check
passes, the verifier knows: "a program that multiplies two non-trivial factors
produced 391." The verifier does not know the factors. The verifier does not need
to trust the prover. The math guarantees it.

### Image ID

The image ID deserves its own paragraph because it is easy to misunderstand. It
is not a hash of the source code -- it is a hash of the compiled, linked,
packaged guest binary loaded into the zkVM memory image. If you recompile the
guest with a different compiler version, different optimization flags, or a
different `libzkvm_platform.a`, you get a different image ID. The image ID
pinpoints the exact binary.

This means the verifier must know the expected image ID in advance. In practice,
you publish the image ID alongside your application so that verifiers can check
receipts against it. The current deterministic image ID for `multiply` is:

```
db8cb4b1a0a6045cc3e64f1eb6f2927eadd73f33bbceb261b91da1b3068e10f2
```

Build it yourself with `make multiply` using the same toolchain to reproduce
this value.

### How Verification Works in Practice

A verification flow typically looks like this:

1. The prover publishes the image ID for their guest program. This is a one-time
   step -- the image ID does not change unless the guest binary changes.
2. The prover generates a receipt by running `Prove` with their private witness.
3. The prover sends the receipt to the verifier (or publishes it).
4. The verifier calls `Verify` with the receipt and the known image ID.
5. The verifier reads the journal from the verified receipt and interprets it.

For the multiply example, the verifier would read 8 bytes from the journal,
decode them as a little-endian `uint64`, and obtain the product. The verifier
knows this product was computed by the multiply guest (identified by image ID)
from *some* pair of non-trivial, non-overflowing factors. The verifier cannot
determine the factors.

The journal format is defined by the guest code. The verifier must know how to
parse it. In simple cases (like a single `uint64`), this is trivial. In more
complex cases (like the 72-byte claim in Chapter 7), the verifier needs a
decoder that mirrors the guest's commit layout.

---

## Chapter 4: Structured Data and Policy

The policy check example in `examples/policy_check/main.go` shows how a real
guest program handles multiple witness values, input validation, derived
computation, and a multi-field public commit:

```go
package main

import "github.com/roasbeef/go-zkvm/zkvm"

const maxPolicyItems = 16

func main() {
	var itemCount uint32
	zkvm.ReadValue(&itemCount)

	if itemCount == 0 || itemCount > maxPolicyItems {
		zkvm.Debug("Error: invalid item count\n")
		zkvm.Halt(1)
	}

	var subtotal uint64
	for i := uint32(0); i < itemCount; i++ {
		var item uint64
		zkvm.ReadValue(&item)
		if item == 0 {
			zkvm.Debug("Error: item value must be non-zero\n")
			zkvm.Halt(1)
		}

		nextSubtotal := subtotal + item
		if nextSubtotal < subtotal {
			zkvm.Debug("Error: subtotal overflow\n")
			zkvm.Halt(1)
		}
		subtotal = nextSubtotal
	}

	var discount uint64
	var limit uint64
	zkvm.ReadValue(&discount)
	zkvm.ReadValue(&limit)

	if discount > subtotal {
		zkvm.Debug("Error: discount exceeds subtotal\n")
		zkvm.Halt(1)
	}

	total := subtotal - discount
	var approved uint32
	if total <= limit {
		approved = 1
	}

	zkvm.CommitValue(&itemCount)
	zkvm.CommitValue(&approved)
	zkvm.CommitValue(&subtotal)
	zkvm.CommitValue(&discount)
	zkvm.CommitValue(&total)
	zkvm.CommitValue(&limit)
	zkvm.Halt(0)
}
```

### The Pattern

Every serious guest follows this structure:

1. **Read** private witness data from stdin.
2. **Validate** all inputs inside the guest. The guest is the trusted
   computation -- if it accepts garbage input and produces garbage output, the
   proof is valid but meaningless. Input validation is the guest's
   responsibility.
3. **Compute** the derived result.
4. **Commit** only the public summary to the journal.

In the policy check guest, the host provides a private list of item prices, a
discount, and a spending limit. The guest validates everything (non-zero items,
no overflow, discount does not exceed subtotal), computes the totals, derives an
approval bit, and commits a compact 28-byte public summary.

### Multiple Witness Values

The witness stream is just a byte sequence. `ReadValue` consumes `sizeof(T)`
bytes each time it is called. The host and guest must agree on the wire format:
which types, in which order. There is no framing, no length prefix on the stream
itself -- the protocol is implicit in the code.

For `policy_check`, the wire format is:

```
[item_count: u32_le]
[item_0: u64_le] [item_1: u64_le] ... [item_N: u64_le]
[discount: u64_le]
[limit: u64_le]
```

The guest reads `itemCount` first, then loops to read exactly that many items.

### Structured Public Commit

The journal is also just a byte sequence. Each `CommitValue` call appends
`sizeof(T)` bytes. The verifier parses the journal using the same layout
convention. For the default built-in sample, the raw journal hex is:

```
0300000001000000f5000000000000001400000000000000e100000000000000fa00000000000000
```

Decoded:

| Field       | Type   | Value |
|-------------|--------|-------|
| item count  | u32_le | 3     |
| approved    | u32_le | 1     |
| subtotal    | u64_le | 245   |
| discount    | u64_le | 20    |
| total       | u64_le | 225   |
| limit       | u64_le | 250   |

The verifier sees: "3 items totaling 245, with a 20 discount yielding 225, which
is under the 250 limit, so the purchase was approved." The verifier never sees
individual item prices.

### Validation as Guest Responsibility

Notice the overflow check:

```go
nextSubtotal := subtotal + item
if nextSubtotal < subtotal {
	zkvm.Debug("Error: subtotal overflow\n")
	zkvm.Halt(1)
}
```

This is not paranoia. A malicious host could feed witness data designed to cause
an integer overflow, producing a small `subtotal` from large items and tricking
the approval check. The guest must defend against this because the guest's code
is the only thing the proof guarantees.

A `Halt(1)` terminates execution with a non-zero exit code. The prover still
produces a receipt, but it records the failure. The verifier can detect this from
the exit code in the receipt metadata.

### Bounds Checking

The `maxPolicyItems = 16` cap prevents unbounded iteration inside the guest.
zkVM execution is metered in cycles, so unbounded loops are expensive and could
cause the prover to run far longer than expected. Capping iteration bounds in the
guest is standard practice.

### Writing Your Own Guest: The Checklist

Based on the patterns we have seen in the three examples, here is what every
guest program needs:

1. **Import `github.com/roasbeef/go-zkvm/zkvm`.** This is the only package your
   guest needs from this repo.
2. **Read private input with `ReadValue` or `Read`.** Match the host-side wire
   format exactly.
3. **Validate all inputs.** Check bounds, check for overflow, reject malformed
   data. The proof is only as meaningful as the guest's validation logic.
4. **Commit public output with `CommitValue` or `Commit`.** Only commit what the
   verifier needs to see.
5. **Call `zkvm.Halt(0)` on success.** This finalizes the journal digest. Use
   `Halt(1)` for failure.
6. **Do not use `fmt.Printf` or `os.Exit`.** These will not work in the TinyGo
   zkVM environment. Use `zkvm.Print` for debug output and `zkvm.Halt` for
   exit.
7. **Keep heap allocations minimal.** TinyGo's allocator inside the zkVM is
   functional but not as battle-tested as standard Go's. Prefer stack-allocated
   arrays with fixed maximum sizes, as shown in the BIP-32 guest.

---

## Chapter 5: Journal Digest Internals

This chapter explains the machinery behind `Commit` and `Halt`. If you skip this
and your journal comes back empty, come back here.

### The Problem

Writing bytes to fd 3 (the journal stream) makes them visible to the host
process. But the risc0 proof system does not just trust the raw journal bytes. It
requires the guest to produce a **journal digest** -- a SHA-256 hash of the
journal contents -- and pass that digest to `sys_halt`. The digest is what
actually gets bound into the cryptographic receipt. On the verifier side, the
receipt contains the journal bytes and the digest, and the verifier re-hashes
the journal to confirm the digest matches.

This means: if you write to fd 3 but do not update the digest, the prover and
verifier will disagree about what the journal contains. The proof will fail or
the journal will appear empty.

### How Commit Works

Look at `Commit` in `zkvm/zkvm.go`:

```go
func Commit(buf []byte) {
	if len(buf) > 0 {
		C.sys_write(FD_JOURNAL, (*C.char)(unsafe.Pointer(&buf[0])), C.int(len(buf)))
		UpdateProperHasher(buf)
	}
}
```

Two operations, every time:

1. `sys_write(FD_JOURNAL, ...)` sends the bytes to the host-visible journal.
2. `UpdateProperHasher(buf)` feeds the same bytes into a running SHA-256 hash
   maintained by the guest.

`CommitValue` does the same thing for a typed value: writes the raw bytes to
the journal, then feeds them to the hasher.

**Always use `Commit` or `CommitValue`.** Do not call `sys_write(FD_JOURNAL, ...)`
directly unless you also update the hasher manually. This is the most common
source of "my journal is empty" bugs.

### How Halt Finalizes the Digest

Look at `Halt`:

```go
func Halt(exitCode uint8) {
	outputDigest := FinalizeProperHasher()
	C.sys_halt(C.uint8_t(exitCode), (*C.uint32_t)(unsafe.Pointer(&outputDigest[0])))
}
```

`FinalizeProperHasher` does three things:

1. **Pads and finalizes the SHA-256 journal hash.** Standard SHA-256 padding:
   append a `0x80` byte, zero-fill to the block boundary minus 8 bytes, append
   the 64-bit big-endian bit count. Then compress the final block(s).

2. **Wraps the journal digest in a tagged struct.** The risc0 convention uses
   `taggedStruct("risc0.Output", [journalDigest, assumptionsDigest])` to create
   the final output digest. The assumptions digest is all-zeros for ordinary
   guests that do not register proof assumptions. Composed guests update it via
   helpers such as `zkvm.Verify(...)`. The tagged
   struct convention is:

   ```
   SHA256( SHA256(tag_string) || digest_0 || digest_1 || count_u16_le )
   ```

   For `"risc0.Output"` with a journal digest and an empty assumptions digest,
   that expands to:

   ```
   SHA256( SHA256("risc0.Output") || journal_digest || zero_digest || 0x0200 )
   ```

3. **Passes the output digest to `sys_halt`.** The prover uses this 32-byte
   digest to bind the journal contents to the receipt.

### The Byte-Swap Convention

If you look at `sha256_proper.go`, you will notice the initial SHA-256 hash
values are byte-swapped:

```go
var sha256InitStateBE = [8]uint32{
	1743128938, // 0x67e6096a - byte-swapped H0 (0x6a09e667)
	2242799547, // 0x85ae67bb - byte-swapped H1 (0xbb67ae85)
	// ...
}
```

risc0's RISC-V SHA acceleration syscalls (`sys_sha_compress`, `sys_sha_buffer`)
operate on 32-bit words in the machine's native little-endian byte order. The
standard SHA-256 spec defines the initial hash values in big-endian. So we
byte-swap the constants at initialization time and work in little-endian
throughout. The bit-count trailer in the padding is also stored with its bytes
swapped into a little-endian word.

This is a detail you should never need to touch directly -- the `zkvm` package
handles it -- but if you are debugging a digest mismatch, the byte order is
usually the first thing to check.

### The SHA Syscalls

The Go guest does not implement SHA-256 in software. It calls two zkVM syscalls
provided by `libzkvm_platform.a`:

- `sys_sha_compress(out_state, in_state, block_half_1, block_half_2)`:
  compresses a single 64-byte block into the hash state. Takes pointers to two
  32-byte halves of the block separately.
- `sys_sha_buffer(out_state, in_state, buf, num_blocks)`: compresses multiple
  contiguous 64-byte blocks in one call.

These syscalls are accelerated inside the zkVM -- they execute as a single
"SHA extension" operation rather than hundreds of individual RISC-V instructions.
This matters because every instruction inside the VM adds to the proof size
(measured in cycles). Using the SHA syscalls keeps journal finalization cheap.

### The Running Hasher State

The `ProperJournalHasher` struct in `sha256_proper.go` maintains the incremental
hash state between `Commit` calls:

```go
type ProperJournalHasher struct {
	state      [8]uint32             // Current SHA-256 hash state.
	bufferData [SHA256_BLOCK_SIZE]byte // Leftover bytes < 64.
	bufferLen  int                    // How many leftover bytes.
	totalLen   uint64                 // Total bytes committed.
}
```

The hasher is initialized lazily on the first `Commit` call. Each `Commit` feeds
data through in 64-byte blocks. If a `Commit` call provides fewer than 64 bytes,
the data is buffered until the next call completes a full block. On `Halt`, any
remaining buffered bytes are padded to a full block and processed.

This means you can call `Commit` multiple times with small values (as
`policy_check` does with six separate `CommitValue` calls), and the hasher
accumulates them correctly across calls. The final digest accounts for every byte
committed in order.

### Why the Go Layer Does This

The Rust guest SDK handles all of this automatically inside its journal
implementation. Since we are not using the Rust SDK, the Go guest layer
reimplements the finalization logic. This is the reason `sha256_proper.go` exists
even on the archive-linked build path: the platform archive gives us the syscall
stubs, but the Go code must maintain the running hash state, apply SHA-256
padding, construct the tagged `risc0.Output` struct, and pass the result to
`sys_halt`.

### Common Failure: Empty Journal

If your journal comes back empty after a guest run, check these causes in order:

1. The guest never called `Commit` or `CommitValue`. Nothing was written.
2. The guest wrote to fd 3 directly (bypassing the hasher) and the digest does
   not match the journal contents.
3. The guest returned from `main()` without calling `Halt`, so the digest was
   never finalized.
4. The guest exited via a panic or trap instead of `Halt`.

Always use `zkvm.Commit` or `zkvm.CommitValue` for journal writes, and always
exit through `zkvm.Halt`.

---

## Chapter 6: Host Integration from Go

So far we have only written guest code. Now we need a host program to drive the
prover and check the receipt. The `host` package provides a typed Go API over the
risc0 Rust proving engine, loaded through a shared library at runtime.

### The Architecture

```
Go host program
    |
    v
host.Client (Go)
    |
    v  (JSON-over-FFI)
host-ffi cdylib (Rust)
    |
    v
risc0-zkvm prover (Rust)
```

The Go package does not reimplement STARK proving. It calls into a Rust shared
library (`libgo_zkvm_host.dylib` on macOS, `.so` on Linux) via `dlopen`/`dlsym`.
The communication between Go and Rust uses a JSON-over-buffer protocol that is
internal to the FFI layer. From the Go side, you work with typed structs.

### Creating a Client

```go
import "github.com/roasbeef/go-zkvm/host"

client, err := host.New()
if err != nil {
	log.Fatal(err)
}
defer client.Close()
```

`New` loads the shared library and checks the ABI version. Library lookup
follows a precedence chain:

1. `host.WithLibraryPath("/explicit/path/to/lib.dylib")` -- strongest override.
2. The `GO_ZKVM_HOST_LIBRARY_PATH` environment variable.
3. Sibling-layout fallback: `../host-ffi/target/release/libgo_zkvm_host.{dylib,so}`
   relative to the `host` package source.

The library is loaded once per process. Subsequent `New` calls reuse the handle.

### Execute: Fast Iteration

```go
guestBinary, err := host.ReadGuestFile("./multiply.bin")
if err != nil {
	log.Fatal(err)
}

result, err := client.Execute(host.ExecuteRequest{
	GuestBinary: guestBinary,
	Stdin:       witnessBytes,
})
if err != nil {
	log.Fatal(err)
}

fmt.Printf("image ID:  %s\n", result.ImageID)
fmt.Printf("journal:   %x\n", result.Journal)
fmt.Printf("exit code: %s\n", result.ExitCode)
fmt.Printf("segments:  %d\n", result.SegmentCount)
```

`Execute` runs the guest to completion without generating a proof. It is
significantly faster than proving because it skips the STARK arithmetic. Use this
for development: compile your guest, execute it, check the journal output, fix
bugs, repeat. Only switch to `Prove` when the guest logic is correct.

### Prove: Generate a Receipt

```go
proveResult, err := client.Prove(host.ProveRequest{
	GuestBinary: guestBinary,
	Stdin:       witnessBytes,
})
if err != nil {
	log.Fatal(err)
}

fmt.Printf("seal: %d bytes\n", proveResult.SealBytes)
fmt.Printf("prover: %s\n", proveResult.ProverName)
```

`Prove` runs the guest and generates a STARK proof. On Apple Silicon, the Rust
prover will use Metal GPU acceleration transparently. The result includes the
serialized receipt, journal, image ID, and proof metadata.

By default, `Prove` performs a Rust-side self-verification of the receipt before
returning. This catches prover bugs early. Disable it with
`host.WithReceiptSelfVerify(false)` if you want to shave a few seconds.

### Verify: Check a Receipt

```go
verifyResult, err := client.Verify(host.VerifyRequest{
	Receipt:         proveResult.Receipt,
	ImageID:         proveResult.ImageID,
	ExpectedJournal: proveResult.Journal,
})
if err != nil {
	log.Fatal(err)
}

fmt.Printf("verified: %v\n", verifyResult.Verified)
```

`Verify` checks a serialized receipt against an expected image ID. The optional
`ExpectedJournal` field additionally checks that the committed journal matches.
Verification is fast and requires no access to the original witness data --
that is the entire point of zero-knowledge proofs.

### Complete Host Program

Putting it all together, here is a self-contained host program that reads a
guest binary, provides witness bytes, proves, and verifies:

```go
package main

import (
	"encoding/binary"
	"fmt"
	"log"

	"github.com/roasbeef/go-zkvm/host"
)

func main() {
	guest, err := host.ReadGuestFile("./multiply.bin")
	if err != nil {
		log.Fatal(err)
	}

	// Build the private witness: two uint64 factors.
	var witness [16]byte
	binary.LittleEndian.PutUint64(witness[0:8], 17)
	binary.LittleEndian.PutUint64(witness[8:16], 23)

	client, err := host.New()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	// Execute first to validate guest logic.
	execResult, err := client.Execute(host.ExecuteRequest{
		GuestBinary: guest,
		Stdin:       witness[:],
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("execute: image_id=%s journal=%x\n",
		execResult.ImageID, execResult.Journal)

	// Generate a STARK proof.
	proveResult, err := client.Prove(host.ProveRequest{
		GuestBinary: guest,
		Stdin:       witness[:],
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("prove: seal=%d bytes, prover=%s\n",
		proveResult.SealBytes, proveResult.ProverName)

	// Verify the receipt.
	verifyResult, err := client.Verify(host.VerifyRequest{
		Receipt:         proveResult.Receipt,
		ImageID:         proveResult.ImageID,
		ExpectedJournal: proveResult.Journal,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("verified: %v\n", verifyResult.Verified)
}
```

### Error Handling

All host operations return `*host.HostError` on failure. You can inspect the
structured error:

```go
var hostErr *host.HostError
if errors.As(err, &hostErr) {
	fmt.Printf("op=%s code=%s message=%s\n",
		hostErr.Op, hostErr.Code, hostErr.Message)
}
```

Common error codes include `invalid_guest_binary` (malformed `.bin` file),
`execute_failed` (guest crashed), `prove_failed` (prover error), and
`verify_failed` (receipt does not match).

### Building the Witness

The witness is just a `[]byte`. You are responsible for serializing your private
input into the exact byte layout the guest expects. For the multiply example,
that means two little-endian `uint64` values:

```go
var witness [16]byte
binary.LittleEndian.PutUint64(witness[0:8], 17)
binary.LittleEndian.PutUint64(witness[8:16], 23)
```

For more complex witnesses, use `binary.Write` to a `bytes.Buffer`:

```go
var buf bytes.Buffer
binary.Write(&buf, binary.LittleEndian, uint32(3))        // item count
binary.Write(&buf, binary.LittleEndian, uint64(100))       // item 0
binary.Write(&buf, binary.LittleEndian, uint64(65))        // item 1
binary.Write(&buf, binary.LittleEndian, uint64(80))        // item 2
binary.Write(&buf, binary.LittleEndian, uint64(20))        // discount
binary.Write(&buf, binary.LittleEndian, uint64(250))       // limit
witness := buf.Bytes()
```

The key rule: the host's serialization order and types must match the guest's
`ReadValue` call sequence exactly. There is no schema negotiation. If the host
writes a `uint32` where the guest reads a `uint64`, the guest will read garbage
and either produce wrong output or fail validation.

### Computing the Image ID

You can compute the image ID for a guest binary without executing or proving it:

```go
imageID, err := client.ComputeImageID(guestBinary)
```

This is useful for pinning the image ID in your application configuration. The
image ID is deterministic, so you can compute it once, store it, and use it for
all future verification calls.

### Convenience Helpers

For one-shot operations where you do not need to reuse the client, the package
provides file-based helpers that create a temporary client internally:

```go
result, err := host.ExecuteFile("./my-guest.bin", witnessBytes)
result, err := host.ProveFile("./my-guest.bin", witnessBytes)
imageID, err := host.ComputeImageIDFile("./my-guest.bin")
```

### Build Requirement: cgo

The `host` package requires cgo because it uses `dlopen` to load the Rust shared
library. When cgo is disabled (`CGO_ENABLED=0`), the package compiles with a
stub implementation where every method returns:

```
host: the FFI-backed host package requires cgo
```

Make sure `CGO_ENABLED=1` is set when building or testing code that uses the
`host` package.

See [host-api.md](host-api.md) for the full API reference including all request
and result types, option functions, error codes, and the FFI memory model.

---

## Chapter 7: Building a Real Application

The `bip32-pq-zkp` project is a real application built on `go-zkvm`. It proves
the statement:

> "I know a BIP-32 seed and derivation path such that, following BIP-32 child
> key derivation and BIP-86 Taproot output-key construction, the result is this
> specific 32-byte Taproot output key."

The seed and path are private. The Taproot output key is public. The proof
receipt lets anyone verify the claim without seeing the seed.

### The Guest

The guest lives in `bip32-pq-zkp/guest/main.go`:

```go
package main

import (
	"github.com/roasbeef/bip32-pq-zkp/bip32"
	"github.com/roasbeef/go-zkvm/zkvm"
)

const (
	maxSeedBytes     = 64
	maxPathDepth     = 16
	flagRequireBIP86 = 1
)

func main() {
	zkvm.Debug("bip32: start\n")

	var flags uint32
	zkvm.ReadValue(&flags)

	var seedLen uint32
	zkvm.ReadValue(&seedLen)
	if seedLen < 16 || seedLen > maxSeedBytes {
		zkvm.Debug("invalid seed length\n")
		zkvm.Halt(1)
	}

	var seed [maxSeedBytes]byte
	zkvm.Read(seed[:int(seedLen)])

	var pathLen uint32
	zkvm.ReadValue(&pathLen)
	if pathLen > maxPathDepth {
		zkvm.Debug("invalid path length\n")
		zkvm.Halt(1)
	}

	var path [maxPathDepth]uint32
	for i := uint32(0); i < pathLen; i++ {
		zkvm.ReadValue(&path[i])
	}

	pathSlice := path[:int(pathLen)]

	var opts []bip32.TaprootDeriveOption
	if flags&flagRequireBIP86 != 0 {
		opts = append(opts, bip32.WithBIP86PathVerification())
	}

	claim, err := bip32.DeriveTaprootClaim(
		seed[:int(seedLen)], pathSlice, opts...,
	)
	if err != nil {
		zkvm.Debug("DeriveTaprootClaim failed\n")
		zkvm.Halt(1)
	}

	zkvm.Commit(claim.Encode())
	zkvm.Halt(0)
}
```

This is the same pattern from Chapter 4, scaled up to a real cryptographic
computation. Read private witness data, validate it, compute the result, commit
only the public claim.

The core proof computation happens inside `bip32.DeriveTaprootClaim`. That
function runs the full BIP-32 HMAC-SHA512 child key derivation chain, computes
the BIP-340 x-only public key, applies the BIP-86 Taproot tweak, and packages
the result into a `PublicClaim`. All of those Go crypto operations execute inside
the VM, and every instruction is covered by the proof.

A few things to note about the guest structure:

- The `seed` and `path` arrays are stack-allocated with fixed maximum sizes.
  This avoids heap allocation in the TinyGo guest environment, where the
  allocator may behave differently than standard Go.
- `zkvm.Read` (not `ReadValue`) is used for the raw seed bytes, since the seed
  is a variable-length byte slice rather than a fixed-size type.
- The `flags` field controls policy enforcement. When `flagRequireBIP86` is set,
  the guest verifies that the derivation path matches the BIP-86 shape
  (`m/86'/coin_type'/account'/change/index`) before proceeding.

### The Public Claim

The guest commits exactly 72 bytes to the journal:

```go
type PublicClaim struct {
	Version          uint32   //  4 bytes
	Flags            uint32   //  4 bytes
	TaprootOutputKey [32]byte // 32 bytes
	PathCommitment   [32]byte // 32 bytes
}                                // 72 bytes total
```

The `TaprootOutputKey` is the final x-only output key. The `PathCommitment` is a
domain-separated SHA-256 hash of the derivation path:

```go
func CommitPath(path []uint32) [32]byte {
	h := sha256.New()
	h.Write([]byte("bip32-pq-zkp:path:v1"))

	var word [4]byte
	binary.LittleEndian.PutUint32(word[:], uint32(len(path)))
	h.Write(word[:])

	for _, index := range path {
		binary.LittleEndian.PutUint32(word[:], index)
		h.Write(word[:])
	}

	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}
```

The verifier sees the output key and the path commitment but cannot recover the
actual derivation path from the commitment. If the verifier independently knows
the expected path (e.g., the standard BIP-86 path `m/86'/0'/0'/0/0`), they can
recompute the commitment and check it matches.

### The Witness Wire Format

The host builds the private stdin bytes using `BuildWitnessStdin`:

```go
func BuildWitnessStdin(cfg WitnessConfig) ([]byte, bool, error) {
	// ... resolve seed and path from config ...

	var stdin bytes.Buffer

	binary.Write(&stdin, binary.LittleEndian, witness.flags)
	binary.Write(&stdin, binary.LittleEndian, uint32(len(witness.seed)))
	stdin.Write(witness.seed)
	binary.Write(&stdin, binary.LittleEndian, uint32(len(witness.path)))
	binary.Write(&stdin, binary.LittleEndian, witness.path)

	return stdin.Bytes(), witness.usingTestVector, nil
}
```

The wire format is:

```
[flags:      u32_le]
[seed_len:   u32_le]
[seed:       seed_len bytes]
[path_len:   u32_le]
[path:       path_len * u32_le]
```

This matches exactly what the guest reads: `ReadValue(&flags)`,
`ReadValue(&seedLen)`, `Read(seed[:seedLen])`, `ReadValue(&pathLen)`, then a loop
of `ReadValue(&path[i])`. The host and guest must agree on this layout byte for
byte. There is no self-describing framing -- the protocol is defined by the
paired code on each side.

### The Host Runner

The `bip32-pq-zkp` project wraps the `go-zkvm/host` package in a higher-level
`Runner` that handles the full prove-and-verify lifecycle:

```go
func (r *Runner) Prove(cfg ProveConfig) (*ProveReport, error) {
	guestPath, guestBinary, imageID, err := r.loadGuest(cfg.GuestPath)
	// ...

	stdin, usingTestVector, err := BuildWitnessStdin(cfg.Witness)
	// ...

	result, err := r.client.Prove(zkvmhost.ProveRequest{
		GuestBinary: guestBinary,
		Stdin:       stdin,
	})
	// ...

	claim, err := DecodePublicClaim(result.Journal)
	// ...

	// Write receipt and claim.json to disk.
	writeReceipt(cfg.ReceiptOutputPath, result.Receipt)
	WriteClaimFile(cfg.ClaimOutputPath, claimFile)

	return &ProveReport{...}, nil
}
```

The verify flow loads a stored receipt from disk, verifies it against the guest
image ID, decodes the journal into a `PublicClaim`, and optionally checks the
claim fields against explicit expectations:

```go
func (r *Runner) Verify(cfg VerifyConfig) (*VerifyReport, error) {
	// Phase 1: cryptographic verification.
	result, err := r.client.Verify(zkvmhost.VerifyRequest{
		Receipt: receiptBytes,
		ImageID: imageID,
	})
	// ...

	// Phase 2: semantic claim validation.
	claim, err := DecodePublicClaim(result.Journal)
	// ...
	verifyClaimExpectations(cfg.Expectations, claim)
	// ...
}
```

The two-phase pattern -- first verify the receipt cryptographically, then decode
and validate the public claim semantically -- is the standard way to consume
zkVM proofs in a real application. The cryptographic check confirms the proof is
valid. The semantic check confirms the proof says what you expect it to say.

### The Claim File Artifact

The prove flow writes two artifacts to disk:

- **receipt.bin** -- the raw serialized risc0 receipt. This is the cryptographic
  proof. It is opaque binary data.
- **claim.json** -- a human-readable summary of the public claim, including the
  image ID, Taproot output key, path commitment, flags, and proof metadata.

The `claim.json` file is not cryptographically verified by itself -- it is a
convenience artifact. The verification flow checks the receipt against the image
ID and then compares the decoded journal against the stored claim fields. This
catches both receipt tampering (cryptographic check) and claim file tampering
(semantic check).

### Applying the Pattern to Your Own Application

The `bip32-pq-zkp` project demonstrates the full stack, but the pattern
generalizes to any application. The recipe is:

1. **Define your proof statement.** What private input does the prover have?
   What public claim should the verifier see?
2. **Write the guest.** Read the private input, validate it, compute the result,
   commit the public claim. Use fixed-size arrays and careful bounds checking.
3. **Define the witness wire format.** Document the byte layout. Write a
   `BuildWitness` function on the host side.
4. **Define the journal format.** Document the public claim structure. Write an
   `DecodeClaim` function on the host side.
5. **Write the host-side runner.** Use the `host` package to execute, prove, and
   verify. Decode the journal into your claim type. Validate the claim fields
   against expectations.
6. **Pin the image ID.** Build the guest with deterministic tooling (`make
   platform-standalone`), compute the image ID, and publish it so verifiers can
   check receipts.

### What the Proof Means

When a verifier holds a valid receipt for the `bip32-pq-zkp` guest with image ID
`X`, journal containing Taproot output key `P` and path commitment `C`, the
verifier knows:

1. The program with image ID `X` ran to completion inside the zkVM.
2. Someone supplied a private seed and path that, when processed through BIP-32
   derivation and BIP-86 Taproot construction, produce output key `P`.
3. The derivation path hashes to commitment `C`.
4. The person who generated the proof had access to the seed. The seed never
   left the prover's machine.

If the Taproot output key `P` matches a UTXO on the Bitcoin blockchain, the
proof demonstrates knowledge of the corresponding HD wallet seed without
revealing it.

---

## Chapter 8: Advanced Topics

This chapter covers practical concerns that arise once you move beyond the sample
programs.

### Legacy vs. Archive-Linked Builds

The first working prototype of Go guests used handwritten RISC-V assembly for
each zkVM syscall:

```
Go guest -> TinyGo -> custom sys_zkvm.S -> zkVM
```

The current path links against the upstream `libzkvm_platform.a` archive:

```
Go guest -> TinyGo -> link libzkvm_platform.a -> zkVM
```

The archive-linked path is better in every way: it stays aligned with upstream
risc0 syscall ABI changes, covers syscalls we never implemented by hand, and
reduces the surface area for bugs. Some historical documentation in `docs/`
references the older handwritten path, but the supported build flow uses
`-target=zkvm-platform` with the archive. See
[go-zkvm-overview.md](go-zkvm-overview.md) for more context on the two paths.

### Common Failure Modes

**Empty journal.** The most common cause is forgetting to call `Halt`. If the
guest returns from `main()` without calling `zkvm.Halt(0)`, the TinyGo runtime
will halt the process, but the journal digest will not be finalized. The host
sees an empty journal. Second most common cause: writing to fd 3 directly
without calling `UpdateProperHasher`. Always use `Commit` or `CommitValue`.

**ABI drift.** The image ID is a hash of the exact guest binary. If you rebuild
the guest with a different TinyGo version, different Go version, different
`libzkvm_platform.a`, or different risc0 checkout, the image ID changes. A
receipt generated with one image ID will fail verification against a different
one. Pin your toolchain versions. Use `make platform-standalone` for
deterministic archive builds from a published risc0 commit.

**Kernel/user ELF mismatch.** The R0BF container packs both the user ELF (your
guest) and the kernel ELF (`v1compat.elf`). They must come from the same risc0
version. If you update one without the other, the executor will reject the
binary or produce incorrect results.

**ABI version mismatch.** The Go `host` package checks the ABI version reported
by the loaded Rust shared library. If you rebuild `host-ffi` without rebuilding
the Go binary (or vice versa), the versions may diverge. Rebuild both sides when
updating.

**Guest panics without debug output.** TinyGo's panic handler may not produce
useful output inside the zkVM. If your guest crashes silently, add
`zkvm.Debug("checkpoint N\n")` calls at key points to narrow down the failure
location using execute-only mode.

### Cycle Counting

```go
start := zkvm.CycleCount()
// ... expensive computation ...
elapsed := zkvm.CycleCount() - start
```

`CycleCount` calls `sys_cycle_count()`, which returns the number of VM cycles
executed so far. This is useful for profiling guest code. Proof generation cost
scales with cycle count -- more cycles means more rows in the execution trace,
which means a larger STARK proof and longer proving time.

The SHA syscalls (`sys_sha_compress`, `sys_sha_buffer`) are particularly
cycle-efficient because the VM treats them as single operations rather than
expanding them into hundreds of individual RISC-V instructions for the SHA-256
round function. If your guest does heavy hashing, the accelerated syscalls
(which the `zkvm` package uses automatically for journal finalization) are
substantially cheaper than a pure-software SHA-256 implementation.

### Execute-Only Mode for Fast Iteration

During development, use execute-only mode exclusively. Execute runs the guest
through the RISC-V interpreter without generating a proof. It is orders of
magnitude faster than proving -- seconds instead of minutes.

From the reference CLI:

```bash
cargo run --release -- ../my_guest.bin --raw-journal --execute-only
```

From the Go host:

```go
result, err := client.Execute(host.ExecuteRequest{
	GuestBinary: guest,
	Stdin:       witness,
})
```

Execute still computes the image ID, runs the guest, and produces the journal.
The only thing it skips is the STARK arithmetic. Use it to validate guest logic,
check journal output, and debug witness format issues. Switch to `Prove` only
after execute produces the correct result.

### Metal Acceleration on Apple Silicon

On macOS with Apple Silicon, the risc0 Rust prover automatically uses Metal GPU
acceleration for the STARK arithmetic. This is handled entirely on the Rust side
-- neither the Go guest code nor the Go host code needs any changes. The
acceleration applies to `Prove` calls through the `host` package and to the
reference CLI.

To confirm Metal is being used, check the prover name in the prove result:

```go
fmt.Println(proveResult.ProverName) // typically "local" with Metal
```

The local prover selects the Metal backend automatically when it detects a
compatible GPU. If you need to force CPU-only proving (slower but useful for
debugging prover issues):

```bash
export RISC0_FORCE_CPU_PROVER=1
```

### Disassembly Tips for Guest Debugging

When a guest crashes inside the VM, the error message typically includes a
program counter (PC) value. To map this back to your Go source code:

```bash
# Extract the user ELF from a packed .bin file.
go run ./extract_r0bf.go my_guest.bin user.elf kernel.elf

# Disassemble the user ELF.
riscv32-unknown-elf-objdump -d user.elf | less

# Search for the PC address.
/0x00012345
```

TinyGo with `-no-debug` strips debug symbols, so you will be reading raw RISC-V
assembly. Adding `zkvm.Debug` checkpoints is usually faster than reading
disassembly for application-level bugs, but disassembly is essential for
diagnosing crashes in the runtime startup sequence or in cgo-generated code.

### Guest Binary Size and Segment Count

The guest binary size affects the image ID but not the proof size directly. What
matters for proof cost is the **execution trace**: the total number of VM cycles
(and thus rows) the guest executes. The trace is split into segments, and each
segment is proven independently. The `ExecuteResult.SegmentCount` and
`ExecuteResult.SessionRows` fields give you this information after an execute-
only run.

Smaller guests tend to produce fewer cycles, but the relationship is not linear.
A tiny guest that calls into a large library (like the BIP-32 derivation code)
will have a small binary but a large execution trace. Profile with `CycleCount`
to understand where your cycles go.

### Building the Full Stack

For reference, here is the complete build sequence from a clean checkout:

```bash
# 1. Build the TinyGo fork (one-time setup).
cd ../tinygo-zkvm
git submodule update --init --recursive
make llvm-source
LLVM_BUILDDIR=/opt/homebrew/opt/llvm make

# 2. Build the risc0 platform archive.
cd ../go-zkvm
make platform-standalone

# 3. Build sample guests.
make simple multiply policy-check

# 4. Build the Rust host FFI library.
make host-ffi

# 5. Run the integration tests.
make test-host-ffi

# 6. (Optional) Prove and verify all samples via the reference CLI.
make verify-samples
```

See [running.md](running.md) for detailed prerequisites, platform-specific
notes, and the exact environment variables required for each step.

### Development Workflow Summary

The typical development cycle for a new guest program looks like this:

1. Write the guest code in `examples/myguest/main.go`. Import `zkvm`, use
   `ReadValue`, `CommitValue`, and `Halt`.
2. Add a Makefile target modeled on the existing `simple` or `multiply` targets.
3. Build with `make myguest`.
4. Execute-only to check the journal output:
   ```bash
   cd go-guest-host
   cargo run --release -- ../myguest.bin --raw-journal --execute-only
   ```
5. Fix bugs, rebuild, re-execute. This loop is fast.
6. Once the journal output is correct, prove:
   ```bash
   cargo run --release -- ../myguest.bin --raw-journal
   ```
7. Write host-side code that uses the `host` package to drive proving and
   verification programmatically.
8. If you need to debug, add `zkvm.Debug` checkpoints. If that is not enough,
   extract the user ELF and disassemble.

The key insight for productivity is: stay in execute-only mode as long as
possible. Proving takes roughly 100x longer than executing. Get the guest logic
right first, then prove once to confirm the receipt works.

---

## Chapter 9: Recursive Composition

Every chapter so far has dealt with a single guest program producing a single
proof. But what happens when you want one proof to vouch for another? Suppose
you have already proved that key A was derived correctly and, separately, that
key B was derived correctly. Now you want a single receipt that says "both A and
B were derived correctly" without making the verifier check two separate proofs.
That is **recursive composition**: a guest program that claims, as part of its
own execution, that other valid proofs exist.

### The Core Idea

A composed guest does not re-run the child program inside the VM. That would be
absurdly expensive -- you would be executing an entire STARK verifier as a RISC-V
program. Instead, the guest makes a much cheaper move: it *registers a claim*
that a valid receipt exists for a specific (image ID, journal) pair, and the
proving infrastructure resolves that claim later using the actual child receipt.

Think of it like a deferred check in a financial audit. The guest writes an IOU:
"I assert that proof X exists." The host holds the actual proof X. During proof
generation, the risc0 recursion pipeline matches each IOU against the
corresponding proof and folds everything into one final receipt. If any IOU
cannot be matched, the proof fails.

The result is a single receipt that cryptographically proves: "this guest ran
correctly, *and* the child proofs it depended on are all valid." The verifier
checks one receipt. The child receipts never need to leave the prover's machine.

### Two Parallel Representations

Composition creates two views of the same dependency set, and they must agree
exactly:

**Guest side (digest-only).** Each call to `zkvm.Verify` computes a
cryptographic digest of the child claim and folds it into a running linked list
called the *assumptions digest*. This digest becomes part of the guest's final
output -- specifically, the `assumptions_digest` half of the `risc0.Output`
tagged struct that `sys_halt` receives. The guest never touches the actual child
receipt bytes. It only knows the digests.

**Host side (concrete receipts).** The caller passes serialized succinct
receipts in the `Assumptions` field of `ExecuteRequest` or `ProveRequest`. The
Rust prover attaches these to the executor environment via
`builder.add_assumption(receipt)`. During proof generation, the recursion
pipeline opens the guest's assumptions list and matches each entry against a
supplied receipt.

If the guest registered three assumptions but the host supplied only two
receipts -- or the digests do not match -- proof generation fails with an
"assumptions mismatch" error. The system is all-or-nothing: every assumption
the guest registers must be resolved by a concrete receipt on the host side.

### What `zkvm.Verify` Actually Does

When guest code calls:

```go
zkvm.Verify(childImageID, childJournal)
```

six things happen in sequence, and each one matters:

1. **Hash the child journal.** The child's public journal bytes are SHA-256
   hashed into a `journalDigest`. This is the same hash the child guest
   produced when it called `Commit` and `Halt`.

2. **Build the child's post-state.** For an unconditional receipt (the common
   case in batch aggregation), the post-execution system state is all zeros --
   there is no memory image to commit to.

3. **Build the child's output digest.** The `risc0.Output` tagged struct
   combines `journalDigest` with a zero assumptions digest. (The child was
   unconditional, so its own assumptions list was empty.)

4. **Build the child's receipt claim digest.** The `risc0.ReceiptClaim` tagged
   struct combines the image ID, the post-state, the output, and two zero
   scalars (exit code and input digest).

5. **Register the assumption.** The guest calls `sys_verify_integrity`, passing
   the claim digest and a zero control root. The host's syscall handler looks up
   a matching receipt in the session's assumption registry. If it finds one, it
   marks it as accessed; if not, execution halts immediately.

6. **Update the running assumptions digest.** The claim digest is wrapped in a
   `risc0.Assumption` tagged struct and prepended to the running cons-cell
   linked list via `addAssumptionDigest`. This list will become the
   `assumptions_digest` in the final `risc0.Output` that `Halt` passes to
   `sys_halt`.

Steps 1 through 4 reconstruct the *exact* digest chain that the Rust guest SDK
would produce for `env::verify(image_id, journal)`. Getting any of these steps
wrong -- a different hash, a different tagged-struct field order, a missing
scalar -- produces a silent digest mismatch that surfaces only as a cryptic
"assumptions mismatch" error during proving.

### The Assumptions Digest: A Linked List of Claims

The running assumptions digest is not an array. It is a hash-based cons-cell
linked list, built from the inside out:

```
empty list = [0, 0, 0, 0, 0, 0, 0, 0]   (all-zero digest)

after Verify(A):
  assumption_A = taggedStruct("risc0.Assumption", [claimDigest_A, zeroControlRoot])
  list = taggedStruct("risc0.Assumptions", [assumption_A, empty_list])

after Verify(B):
  assumption_B = taggedStruct("risc0.Assumption", [claimDigest_B, zeroControlRoot])
  list = taggedStruct("risc0.Assumptions", [assumption_B, list])
```

Each `taggedStruct` call computes `SHA256(SHA256(tag) || digest_0 || ... ||
count_u16_le)`. The result is a single 256-bit digest that commits to the
entire ordered sequence of assumptions.

The order matters. If the guest calls `Verify(A)` then `Verify(B)`, the
assumptions list is `[B, [A, nil]]` (most recent first). The host must supply
receipts that resolve in the same order. The recursion pipeline peels
assumptions from the head of the list, so the last-registered assumption is
resolved first.

### Host-Side Plumbing

On the host side, passing assumptions is straightforward:

```go
result, err := client.Prove(host.ProveRequest{
    GuestBinary: batchGuest,
    Stdin:       witnessBytes,
    Assumptions: []host.AssumptionReceipt{
        leafReceiptA,  // serialized succinct receipt bytes
        leafReceiptB,
    },
}, host.WithReceiptKind(host.ReceiptKindSuccinct))
```

Each `AssumptionReceipt` is a `[]byte` containing the serialized succinct
receipt. The `host` package base64-encodes these into the FFI JSON request,
the Rust side decodes them back into `Receipt` objects, and
`builder.add_assumption(receipt)` attaches each one to the executor
environment.

**Why succinct?** The risc0 recursion pipeline requires assumptions to be
succinct receipts -- not composite receipts. A composite receipt is a bundle
of per-segment proofs that has not yet been compressed. The recursion layer
needs a single constant-size proof per assumption to fold into the composed
result. If you pass a composite receipt as an assumption, the host will reject
it. Always compress leaf receipts to succinct form before using them as
assumptions.

### What Happens During Proving

When the host calls `Prove` with assumptions, the risc0 prover runs the
following pipeline:

1. **Execute the guest.** The RISC-V interpreter runs the guest program. Each
   `sys_verify_integrity` call is intercepted by the host's syscall handler,
   which checks that a matching receipt exists in the session's assumption
   registry.

2. **Segment the trace.** The execution trace is split into segments, just like
   a non-composed proof.

3. **Lift each segment.** Each segment receipt is transformed into a succinct
   receipt using the recursion circuit. This is the same lift step from
   Chapter 1, but now the lifted receipts carry an assumptions list.

4. **Join segments.** The lifted segment receipts are joined pairwise into a
   single succinct receipt. The join circuit merges assumptions from both
   halves.

5. **Resolve assumptions.** For each assumption in the joined receipt, the
   recursion pipeline's `resolve` program takes the conditional receipt and
   the matching child receipt, cryptographically removes the head assumption
   from the list, and produces a new receipt with one fewer assumption.
   Repeat until the assumptions list is empty.

6. **Return an unconditional receipt.** Once all assumptions are resolved, the
   final receipt has an empty assumptions list. It is unconditional -- any
   verifier can check it without needing the child receipts.

The key insight: the child receipts are consumed during proving. They are
prover-side inputs, not part of the final artifact. The verifier never sees
them.

### Debugging Composition Failures

When composition goes wrong, the error messages are often opaque. Here is a
systematic checklist:

1. **Digest reconstruction.** Is `zkvm.Verify` building the exact same claim
   digest as the Rust SDK would? Check that the tagged-struct field order,
   the hash function byte-swap convention, and the scalar encoding all match.

2. **Assumptions digest updates.** Does every `zkvm.Verify` call update the
   running assumptions digest? If the guest calls `Verify` but forgets to
   fold the assumption into the digest, the final `risc0.Output` will not
   match the host's expectations.

3. **Receipt order.** Are the host-side receipts supplied in the right order?
   The recursion pipeline resolves assumptions LIFO (last registered = first
   resolved). If you swap two receipts, the claim digests will not match.

4. **Receipt kind.** Are all assumption receipts succinct? Composite receipts
   cannot serve as assumptions.

5. **Image ID agreement.** Does the image ID in `zkvm.Verify(imageID, ...)`
   match the image ID that was used to produce the child receipt? A single
   byte difference in the guest binary produces a completely different image
   ID.

---

## Chapter 10: Building a Batch Aggregation Guest

Chapters 1 through 7 showed you how to prove one statement. Chapter 9 showed
you how one proof can depend on others. This chapter puts both ideas together
to build a **batch aggregation guest**: a program that takes N existing leaf
proofs, verifies them all inside one guest execution, and produces a single
receipt that covers the entire batch.

The motivating scenario is Bitcoin's post-quantum migration. A wallet holds
hundreds of UTXOs, each controlled by a different derived key. The owner needs
to prove knowledge of the seed that produced every key. Proving each key
separately works, but the verifier would need to check hundreds of receipts.
A batch proof compresses that down to one receipt plus a compact Merkle proof
for any individual key the verifier wants to inspect.

### The Architecture

A batch aggregation system has three layers:

**Leaf proofs.** Each leaf is a standalone proof about one key (or one UTXO, or
one derivation step). In the `bip32-pq-zkp` project, the fastest leaf proof is
the hardened-xpriv lane: it proves one HMAC-SHA512 derivation step in about 2
seconds and produces a 72-byte public claim.

**Batch guest.** A separate guest program reads the leaf journals from stdin
and calls `zkvm.Verify` once per leaf. It never sees the leaf private witnesses
-- only the public journals and the fact that valid proofs exist. After
verifying all leaves, it hashes them into a Merkle root and commits a fixed-
size batch claim to the journal.

**Sparse verification.** The verifier checks the single batch receipt, then
uses an ordinary Merkle inclusion proof to confirm that one specific leaf is
part of the committed batch. No leaf receipts need to be distributed.

### What the Batch Guest Reads

The host builds a private witness that the batch guest reads from stdin:

```
[leaf_claim_kind : u32 LE]       -- which leaf schema (1=taproot, 2=xpriv, ...)
[merkle_hash_kind : u32 LE]      -- which hash for the tree (1=SHA-256)
[leaf_context_digest : 32 bytes]  -- the shared leaf guest image ID
[leaf_count : u32 LE]             -- how many leaves follow
[leaf_journal_0 : N bytes]        -- first leaf's public journal
[leaf_journal_1 : N bytes]        -- second leaf's public journal
...
```

Every leaf journal has the same fixed size (72 bytes for Taproot and
hardened-xpriv, 84 bytes for child batch claims). The guest reads the header,
validates the configuration, then loops over the leaves.

### What the Batch Guest Does

For each leaf, the guest performs exactly one operation:

```go
zkvm.Verify(leafContextDigest, leafJournal)
```

This registers an assumption: "a valid receipt exists for a guest with image
ID `leafContextDigest` that committed exactly `leafJournal` to its journal."
The host will resolve that assumption against the corresponding succinct leaf
receipt during proof generation.

After all leaves are verified, the guest builds a **domain-separated Merkle
tree** over the ordered leaf journals:

```
leaf_hash(i)  = SHA256(0x00 || "bip32-pq-zkp:batch-leaf:v1" || i_le32 || journal)
inner_hash    = SHA256(0x01 || left || right)
```

The `0x00` and `0x01` prefixes prevent second-preimage attacks -- you cannot
forge an inner node that looks like a leaf, or vice versa. The tag string and
index binding prevent reordering leaves or substituting leaves from a different
batch.

The final Merkle root, plus a handful of metadata fields, becomes the **84-byte
batch claim** committed to the journal:

```
[version : u32 LE]                -- claim format version
[flags : u32 LE]                  -- policy bits
[leaf_claim_kind : u32 LE]        -- which leaf schema was batched
[merkle_hash_kind : u32 LE]       -- which hash was used
[leaf_count : u32 LE]             -- how many leaves
[leaf_context_digest : 32 bytes]  -- shared leaf guest image ID
[merkle_root : 32 bytes]          -- root of the leaf Merkle tree
```

This claim is fixed-size regardless of N. A batch of 2 leaves and a batch of
16 leaves produce the same 84 bytes. The fan-out is captured by the Merkle
root, not by enumerating leaves in the journal.

### How the Host Orchestrates a Batch Proof

The host-side flow stitches the pieces together:

1. **Load leaf artifacts.** For each leaf, read its `claim.json` to recover the
   journal hex and image ID.

2. **Verify leaf receipts.** Before trusting a leaf receipt as an assumption,
   the host verifies it against the claimed image ID and journal. This catches
   corrupt or mismatched receipts before they reach the prover.

3. **Enforce succinct.** Only succinct receipts can serve as assumptions. The
   host rejects composite leaf receipts.

4. **Build the witness.** Serialize the batch header and concatenated leaf
   journals into the stdin byte stream.

5. **Prove.** Call `client.Prove` with the batch guest binary, the witness
   stdin, and the leaf receipts as assumptions.

6. **Write artifacts.** Save the batch receipt and a human-readable
   `claim.json` that mirrors the 84-byte journal.

The resulting batch receipt is self-contained. The verifier checks it against
the batch guest's image ID and reads the committed batch claim from the
journal. No leaf receipts are needed.

### Sparse Verification: Disclosing One Leaf

The batch receipt proves that a Merkle root was computed correctly over N valid
leaf proofs. But the verifier usually cares about one specific leaf -- say, "is
*my* UTXO covered by this batch?"

That is where **Merkle inclusion proofs** come in. The host derives a proof for
one leaf index:

```go
proof, root, err := batchclaim.BuildProof(leafJournals, leafIndex, sha256Sum)
```

The proof contains the disclosed leaf journal, the leaf index, and the sibling
hashes from leaf to root -- a standard Merkle branch. The verifier recomputes
the root from the disclosed leaf and siblings, then checks it against the
`merkle_root` in the batch claim.

The key property: the Merkle branch grows logarithmically with N. A batch of
16 leaves needs only 4 sibling hashes. A batch of 1024 would need 10. The
batch receipt itself stays the same size regardless.

### Scaling: What Grows and What Stays Flat

In the current implementation on Apple Silicon with Metal acceleration:

- **Succinct receipt size:** ~223 KB, flat from N=2 to N=16.
- **Composite receipt size:** grows linearly, ~340 KB per additional leaf.
- **Prove time:** scales roughly linearly (~0.7s/leaf composite, ~2s/leaf
  succinct).
- **Claim JSON:** ~755 bytes, essentially flat.
- **Inclusion proof:** grows as O(log N) -- negligible.

For sparse disclosure, compare a batch receipt plus one inclusion proof against
distributing N separate succinct leaf receipts:

| N  | N separate receipts | Batch + inclusion |
|----|---------------------|-------------------|
| 2  | 447 KB              | 225 KB            |
| 4  | 893 KB              | 225 KB            |
| 8  | 1,787 KB            | 225 KB            |
| 16 | 3,573 KB            | 225 KB            |

The batch approach gives nearly flat verifier artifacts while the naive
approach grows linearly.

### Nested Batches and Heterogeneous Parents

The batch guest reuses itself for hierarchical aggregation. A parent batch
treats child batch claims as 84-byte leaves with `leaf_claim_kind = 3`
(`batch_claim_v1`). The parent guest calls `zkvm.Verify` against each child
batch's receipt, hashes the child claims into a new Merkle root, and commits
a parent-level batch claim.

Verification walks the hierarchy level by level: verify the parent receipt,
prove one child is included in the parent root, decode the child claim, prove
one original leaf is included in the child root. Each level adds one Merkle
branch to the verifier's work, but the final receipt size stays flat.

The system also supports **heterogeneous parents** that mix raw leaf proofs
and child batch claims at the same level. A fixed-size 128-byte envelope
wraps each direct child, carrying the child kind, the per-child verify image
ID, and the padded journal. The batch claim's 32-byte context slot becomes a
policy digest instead of a single shared image ID.

For the full details of the `bip32-pq-zkp` batch system, see the
[composition guide](https://github.com/roasbeef/bip32-pq-zkp/blob/main/docs/composition-guide.md)
in the `bip32-pq-zkp` repository.

---

## Where to Go Next

- [go-zkvm-overview.md](go-zkvm-overview.md) -- architectural overview of all
  the components and how they fit together.
- [host-api.md](host-api.md) -- full API reference for the Go host package,
  including FFI internals and memory ownership.
- [recursion-composition.md](recursion-composition.md) -- reference-style
  walkthrough of the recursion data flow and debugging rules.
- [running.md](running.md) -- complete runbook with exact commands for every
  build and validation step.
- [implementation-guide.md](implementation-guide.md) -- repo-level mechanics
  for contributors.
