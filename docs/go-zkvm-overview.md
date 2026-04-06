# Go zkVM Integration Overview

## Problem Statement

Rust is the first-class guest language in risc0. Go is not. That means a Go
guest needs its own answer for four things that Rust normally gets “for free”:

1. a target that matches the zkVM memory map
2. access to the guest syscall layer
3. a way to package the user ELF with the kernel ELF
4. a host that can feed private witness data and verify receipts

This repo supplies those pieces.

## The Current Recommended Architecture

```text
Go guest source
    │
    ▼
TinyGo fork with zkVM target support
    │
    ▼
Guest ELF linked with upstream libzkvm_platform.a
    │
    ▼
convert_to_r0bf.go + v1compat.elf
    │
    ▼
R0BF guest binary
    │
    ▼
Host layer:
  - primary Go `host` package via `host-ffi`
  - optional Rust `go-guest-host` reference CLI
```

## Component Roles

### TinyGo Fork

The sibling `tinygo-zkvm` fork contributes the compilation target and runtime
shape needed to produce a valid risc0 guest ELF:

- target JSON files for zkVM builds
- linker script aligned with the risc0 guest memory map
- runtime build-tag selection for the zkVM environment
- startup/runtime behavior that halts correctly on guest exit
- support for archive-linked builds via `libzkvm_platform.a`

### `zkvm/` Go Package

The Go package in this repo is the guest-facing API. It wraps the lower-level
syscall surface in a Go-friendly interface:

```go
zkvm.ReadValue(&input)
zkvm.CommitValue(&result)
zkvm.Debug("trace\n")
zkvm.Halt(0)
```

Even on the archive-linked path, this package still matters because it owns the
Go-level guest API and the journal finalization logic used by the current
guests.

### `convert_to_r0bf.go`

TinyGo produces a user ELF. risc0 execution expects a combined binary that
contains:

- the user ELF
- the matching `v1compat` kernel ELF

`convert_to_r0bf.go` packs those halves into the R0BF format used by the
current workflow.

### Host Surfaces

This repo now exposes the proving-side control plane in two forms:

- `host/`
  - typed Go API for `ComputeImageID`, `Execute`, `Prove`, and `Verify`
  - the primary consumer-facing host surface
  - library lookup is explicit: `WithLibraryPath(...)` first, then
    `GO_ZKVM_HOST_LIBRARY_PATH`, then the sibling-layout fallback
- `host-core/` + `host-ffi/`
  - shared Rust host logic plus the `cdylib` boundary used by the Go package
- `go-guest-host/`
  - Rust reference CLI over the same shared Rust logic
  - retained for debugging, parity checks, and the built-in sample validation
    target

Those host surfaces are responsible for:

- loads the guest binary
- computes the image ID
- writes private witness data into guest stdin
- runs execute-only or prove mode
- verifies the receipt
- prints the committed journal bytes

For any real private-input proof, one of these host layers is required.

## Legacy Path vs Current Path

### Legacy Prototype

The first working prototype used handwritten syscall assembly from the TinyGo
side:

```text
Go guest → TinyGo → custom sys_zkvm.S → zkVM
```

That path was important because it proved Go guests were possible at all, but
it had obvious maintenance problems:

- syscall coverage had to be implemented by hand
- ABI drift was easy to miss
- keeping pace with newer risc0 releases was harder than it needed to be

### Current Path

The newer, better model is:

```text
Go guest → TinyGo → link libzkvm_platform.a → zkVM
```

This keeps the syscall veneer aligned with upstream risc0 and dramatically
reduces the amount of handwritten assembly we need to carry.

## Key Technical Lessons

### 1. Non-Rust Guests Still Need A Real Guest ABI Story

Adding a target JSON was not enough. The TinyGo runtime, startup flow, and
linker layout all had to line up with risc0 expectations.

### 2. Journal Output Is Not Just “Write To fd=3”

The host-visible journal depends on the output digest passed to `sys_halt`.
That was one of the first non-obvious issues in the prototype and remains an
important part of the Go guest layer.

### 3. The Host Harness Is Part Of The Design

As soon as the witness is private, a host program is not optional. Something
must:

- keep the witness off the public journal
- build the `ExecutorEnv`
- drive execution/proving
- verify the receipt against the expected image ID

In the current repo state, that host requirement is now satisfied either by the
FFI-backed Go package or by the Rust reference CLI. The proving engine itself
is still the Rust `risc0-zkvm` stack in both cases.

## Where To Go Next

- `tutorial.md`
  - guided walkthrough from first guest to a real proof application
- `host-api.md`
  - full Go host API reference: client lifecycle, operations, errors, examples
- `tinygo-zkvm-target.md`
  - TinyGo fork changes and why they were required
- `implementation-guide.md`
  - practical repo-level mechanics
- `running.md`
  - exact commands
