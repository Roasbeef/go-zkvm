# Implementation Guide

This document explains the moving parts inside `go-zkvm` itself. For TinyGo
fork internals, read `tinygo-zkvm-target.md` first.

## Core Pieces In This Repo

### `zkvm/`

This is the guest-facing API layer. It provides:

- raw byte input/output
- typed input/output helpers
- journal commit helpers
- debug/private output helpers
- cycle count access
- final guest halt handling

In practice, the two most important files are:

- `zkvm/zkvm.go`
  - the public guest API
- `zkvm/sha256_proper.go`
  - the journal/output digest machinery

### `go-guest-host/`

This is the Rust side of the integration. It is responsible for:

- reading the packaged guest binary
- computing the image ID
- creating `ExecutorEnv`
- writing private witness bytes to guest stdin
- executing or proving
- verifying the receipt

Without this layer, private witness data has nowhere to live.

### `convert_to_r0bf.go`

This tool combines:

- the user ELF produced by TinyGo
- the risc0 `v1compat.elf`

into the guest binary that the host and prover actually load.

## Guest Programming Model

Go guests should stay small and explicit. The intended shape is:

```go
package main

import "github.com/roasbeef/go-zkvm/zkvm"

func main() {
	var input uint32
	zkvm.ReadValue(&input)

	result := input * 2
	zkvm.CommitValue(&result)
	zkvm.Halt(0)
}
```

That maps directly onto the proof model:

- host input is private witness data
- guest computation is constrained by the zkVM
- journal commits are public claim material

## Why The Rust Host Exists

For toy programs, it can look like an extra layer. It is not optional for any
serious proof flow.

The host is where we:

- keep the witness private
- choose between execute-only and prove mode
- compute or pin the expected image ID for the exact built guest artifact
- verify the receipt with the same risc0 stack others will use

For the BIP-32/Taproot demo, this is the layer that writes the private seed and
path into guest stdin and verifies the resulting receipt locally.

Today, that “exact built artifact” wording matters: the linked
`zkvm-platform` archive still leaves absolute build paths in the guest ELF, so
rebuilding the same source tree in a different directory can change the image
ID even when the public journal output is identical.

## Journal Finalization

One of the key details in the Go guest stack is that “commit to fd=3” is not
the full story. The guest must eventually halt with the output digest expected
by risc0.

The current package handles that by:

1. hashing committed journal bytes as they are written
2. finalizing the journal hash with the zkVM SHA machinery
3. building the final `risc0.Output` tagged digest
4. passing that digest to `sys_halt`

This is why the `zkvm` package is still meaningful even when the lower-level
syscall symbols come from `libzkvm_platform.a`.

## Binary Packaging

The build output for a Go guest goes through two stages:

1. TinyGo emits a guest ELF
2. `convert_to_r0bf.go` packs that ELF with `v1compat.elf`

At a high level, the final binary looks like:

```text
[R0BF header][user ELF bytes][kernel ELF bytes]
```

The Rust host then treats that packaged file as the guest image.

## Current Recommended Extension Strategy

If a new Go guest needs more behavior, prefer this order:

1. add the capability at the Go guest API level if it is just a wrapper
2. reuse upstream `libzkvm_platform.a` behavior where possible
3. only add TinyGo fork/runtime changes when the target or runtime truly needs
   them
4. only fall back to handwritten syscall assembly when the archive-linked path
   cannot express what is needed

That keeps the maintenance burden down and keeps us close to upstream risc0.

## Debugging Tips

### Guest Packaging And Layout

```bash
go run ./extract_r0bf.go ./simple.bin
```

This is useful when you need to inspect whether the expected user/kernel halves
made it into the final binary.

### Execute Without Proving First

```bash
cd go-guest-host
cargo run --release -- ../simple.bin --raw-journal --execute-only
```

If execute-only fails, proving will not clarify much.

### Use Trace Logs When The Host Side Is The Question

```bash
RUST_LOG=info cargo run --release -- ../simple.bin --raw-journal
```

### Disassemble The Guest ELF When Layout Or Symbol Resolution Looks Wrong

```bash
llvm-objdump -d simple.elf | less
```

## Common Failure Modes

### Empty Or Wrong Journal

Usually means one of:

- the guest did not commit what you thought it did
- the journal digest/final halt path is wrong
- the host is decoding the wrong guest binary

### ABI Drift Across risc0 Versions

The most concrete version-skew issue we hit on the latest lane was
`sys_cycle_count`: current upstream returns `u64`, not `u32`.

### Kernel / User ELF Mismatch

Packing a guest ELF with an older `v1compat.elf` can fail even when the guest
build itself looks correct. The packaged kernel half has to come from the same
current risc0 lane that produced the platform archive and host crates.
