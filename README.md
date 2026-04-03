# go-zkvm

`go-zkvm` is a TinyGo-first toolkit for building RISC Zero zkVM guests in Go.
It packages the non-Rust guest flow into a reusable set of pieces:

- a guest-side Go API in `zkvm/`
- a TinyGo build flow that targets the zkVM memory map
- an R0BF packer for combining a user ELF with `v1compat.elf`
- a Rust host harness for execution, proving, and receipt verification

The goal is not “make one demo work”, but “make Go a repeatable guest language
for risc0”.

## Current Status

The working path in this repo is the current upstream-aligned lane:

- TinyGo is rebased on upstream `v0.40.1` in the sibling `tinygo-zkvm` repo
- risc0 is rebased on current `main` in the sibling `risc0` repo
- guest builds link against upstream `libzkvm_platform.a`
- Go guests execute, prove, and verify locally
- Apple Silicon proving is confirmed to use the Metal-backed prover path

The older handwritten syscall path is still documented because it explains the
constraints of non-Rust guests, but it is not the recommended integration model
anymore.

## Repo Contents

- `zkvm/`
  - guest-side Go API for host input, private stdout/stderr, public journal
    output, cycle counting, and journal digest finalization
- `simple/`
  - smallest “hello world” guest
- `multiply/`
  - minimal witness-in, public-result-out example
- `platform_smoke/`
  - low-level smoke test for the platform archive integration
- `go-guest-host/`
  - Rust host that loads `.bin` guests, writes private witness data, and
    executes or proves them
- `convert_to_r0bf.go`
  - packs a TinyGo ELF together with the risc0 `v1compat` kernel ELF
- `extract_r0bf.go`
  - unpacks an R0BF guest binary back into user/kernel ELF halves
- `docs/`
  - architecture notes, syscall notes, implementation details, and runbooks

## Intended Repository Layout

The intended sibling layout is:

```text
github.com/roasbeef/
├── risc0
├── tinygo-zkvm
├── go-zkvm
└── bip32-pq-zkp
```

`go-zkvm` depends on `risc0` and `tinygo-zkvm` being checked out next to it.

## Recommended Flow

The recommended build path is:

1. build the TinyGo fork with the zkVM target support
2. build the risc0 platform archive from `examples/c-guest`
3. compile the Go guest with TinyGo target `zkvm-platform`
4. pack the guest ELF with `v1compat.elf`
5. execute or prove it with the Rust host harness

That is the path that produced the current working local proofs.

## Quick Start

### Prerequisites

- Go `1.24.4`
- Rust toolchain compatible with the checked-out `risc0` tree
- sibling checkouts of:
  - `../risc0`
  - `../tinygo-zkvm`
- on Apple Silicon, a working local Metal environment is recommended for proof
  speed

If your default `go` in `PATH` is newer than TinyGo currently supports on this
lane, set:

```bash
export GO_GOROOT=/path/to/go1.24.4
```

before running `make`.

### Build Sample Guests

From the repo root:

```bash
make simple
make multiply
make platform-smoke
```

These targets compile with TinyGo target `zkvm-platform`, link against
`libzkvm_platform.a`, and package the final `.bin` guest image.

### Execute Without Proving

From `go-guest-host/`:

```bash
cargo run --release -- ../simple.bin --raw-journal --execute-only
```

### Prove And Verify

From `go-guest-host/`:

```bash
cargo run --release -- ../simple.bin --raw-journal
```

The host computes the image ID, runs the local prover, verifies the receipt,
and prints the committed journal bytes.

## Multiply Example

`multiply` is the smallest useful zkVM programming example in this repo. It
reads two private values from the host, multiplies them inside the guest, and
commits only the public result.

```go
package main

import "github.com/roasbeef/go-zkvm/zkvm"

func main() {
	var a uint64
	var b uint64

	zkvm.ReadValue(&a)
	zkvm.ReadValue(&b)

	product := a * b
	zkvm.CommitValue(&product)
	zkvm.Halt(0)
}
```

That is the core model for Go guests:

1. read private witness data from the host
2. compute inside the guest
3. commit only the public claim material
4. halt with the final output digest

## What Had To Change In TinyGo

Getting Go onto the zkVM required more than adding a target JSON:

- a TinyGo target that matches the risc0 guest memory map
- startup/runtime wiring that works for the zkVM environment
- task stack/runtime build-tag fixes for TinyGo’s RISC-V runtime selection
- a reliable way to link upstream `libzkvm_platform.a`
- a clean split between the legacy handwritten-syscall path and the newer
  archive-linked path

The short consumer-oriented version is documented here:

- `docs/tinygo-zkvm-target.md`

The sibling `tinygo-zkvm` repo carries the fork-local deep dive for the actual
patches.

## Documentation

Start with:

- `docs/README.md`
- `docs/go-zkvm-overview.md`
- `docs/implementation-guide.md`
- `docs/running.md`
