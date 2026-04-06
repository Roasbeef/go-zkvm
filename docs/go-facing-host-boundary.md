# Go-Facing Host Boundary Design

This document records the original boundary-design reasoning and the three
options that were evaluated. The repo now ships the **Option 2 (FFI)** path:

- `host/` -- typed Go API for execute, prove, and verify
- `host-core/` -- shared Rust host logic
- `host-ffi/` -- Rust `cdylib` exposing the stable C ABI
- `go-guest-host/` -- reference Rust CLI on top of the same shared Rust logic

For the concrete v1 FFI API and ABI contract, see `go-ffi-api-plan.md`.
For the full host API reference, see `host-api.md`.

## Short Answer

If we wrap the current Rust host behind a Go CLI, FFI, or service boundary,
then Go callers can request execution, proving, and verification from Go code
without writing Rust themselves.

That does **not** mean the prover has become pure Go.

It means:

- guest code is still Go/TinyGo
- the proving engine is still the Rust `risc0-zkvm` stack
- Go gets a stable API boundary on top of that engine

So the developer experience can become "prove from Go", while the actual
cryptographic prover implementation remains Rust.

## Current Architecture

Today the working path is:

```text
Go guest source
    │
    ▼
TinyGo zkVM target
    │
    ▼
Guest ELF linked with libzkvm_platform.a
    │
    ▼
R0BF packer + v1compat.elf
    │
    ▼
Rust host (execute / prove / verify)
```

The current host responsibilities are:

- load the packaged guest binary
- compute the image ID
- build the `ExecutorEnv`
- write private witness bytes
- execute or prove
- verify the receipt
- decode journal output

That control plane now lives in:

- `host/`
- `host-core/`
- `host-ffi/`
- `go-guest-host/`

and higher-level repos such as `bip32-pq-zkp` can build their own Go-facing
commands on top of `github.com/roasbeef/go-zkvm/host`.

## Goal

Expose a Go-facing API so that a Go application can do things like:

```go
receipt, claim, err := host.Prove(ctx, host.ProveRequest{
    GuestBinary: "bip32-platform-latest.bin",
    Stdin:       witnessBytes,
})
```

without forcing the application author to manually invoke Rust commands.

## Non-Goals

This design does **not** attempt to:

- reimplement the risc0 prover in Go
- remove Rust from the proving implementation
- remove the need for the risc0 Rust crates at build or runtime

Those would be much larger projects.

## Why This Still Helps

A Go-facing boundary would still be valuable because it would:

- make Go feel like a first-class host language for the common workflow
- let Go apps construct witnesses and orchestrate proofs natively
- keep the proving implementation aligned with upstream risc0
- avoid reimplementing receipt and prover internals in Go

## Boundary Options

There are three realistic ways to expose the Rust host to Go.

### Option 1: CLI Boundary

Rust provides a normal executable, and Go shells out to it.

```text
Go app
  │
  ▼
exec.Command(...)
  │
  ▼
Rust host binary
  │
  ▼
risc0 prover
```

Typical request/response flow:

- Go writes witness bytes to a temp file or stdin pipe
- Go invokes a Rust binary with structured flags or JSON input
- Rust writes receipt and claim artifacts
- Go reads those artifacts back

Pros:

- simplest to implement
- easy to debug manually
- no cgo requirement
- no ABI-stability problem between Go and Rust
- easiest distribution model for the current repo set

Cons:

- process spawning overhead
- temp-file / stdin / stdout plumbing
- less ergonomic than an in-process library API

### Option 2: FFI Boundary

Rust exposes a `cdylib` with a C ABI, and Go calls it through cgo.

```text
Go app
  │
  ▼
cgo bindings
  │
  ▼
Rust shared library
  │
  ▼
risc0 prover
```

Pros:

- in-process API
- no extra child process
- can pass buffers directly

Cons:

- requires cgo
- cross-platform shared library packaging is more painful
- ABI design becomes our problem
- Rust panics and allocator ownership must be contained carefully
- Metal/CUDA/runtime loader issues become trickier to support

### Option 3: Service Boundary

Rust provides a long-running local daemon with a gRPC/HTTP/Unix-socket API.

```text
Go app
  │
  ▼
client package
  │
  ▼
local proof daemon
  │
  ▼
risc0 prover
```

Pros:

- good for repeated proofs
- avoids per-call cold-start costs
- good place to centralize caching, rate limiting, and hardware selection

Cons:

- heaviest operational model
- process lifecycle and IPC become part of the product
- overkill for the current repo state

## Recommendation (Historical)

The original recommended order was:

1. CLI boundary first
2. FFI boundary later, only if the CLI path proves too limiting
3. service boundary only if repeated local proving becomes a major use case

In practice, the FFI path (Option 2) was implemented directly because the
in-process API was worth the cgo requirement for the intended use cases. The
CLI boundary still exists as `go-guest-host/` for debugging and reference.

For the concrete Option 2 plan that was implemented, see `go-ffi-api-plan.md`.

## Implemented Layout

The actual layout that shipped follows the Option 2 (FFI) path:

```text
go-zkvm/
├── host/              # Go API (cgo wrapper)
│   ├── types.go       # Client, request/response structs, options
│   ├── ffi_cgo.go     # dlopen/dlsym + JSON-over-buffer FFI calls
│   └── ffi_nocgo.go   # non-cgo stub with clear error message
├── host-core/         # Shared Rust host logic
│   └── src/lib.rs     # execute, prove, verify implementations
├── host-ffi/          # Rust cdylib boundary
│   └── src/lib.rs     # 6 exported C ABI functions
└── go-guest-host/     # Reference Rust CLI (debug/parity)
    └── src/main.rs
```

The Go `host` package is the primary consumer surface. It exposes typed
request/response structs, byte-oriented APIs (no file paths required), and
structured `HostError` values with machine-readable codes.

Higher-level repos such as `bip32-pq-zkp` build their own CLI commands on top
of `github.com/roasbeef/go-zkvm/host`.

## What This Would Mean For `zkvm`

This would **not** mean the existing guest package `zkvm/` suddenly becomes a
host-side proving library by itself.

Instead:

- `zkvm/`
  - stays the guest-side Go API
- new `host/`
  - becomes the Go-facing host/prover wrapper

That separation is important because guest concerns and proving concerns are
completely different layers.

## Verification Story

The same boundary should cover both proving and verifying.

That means the Go-facing layer should support:

- `Execute`
  - run guest without proof
- `Prove`
  - produce receipt + metadata
- `Verify`
  - verify an existing receipt against an expected image ID and expected public
    claim material

If we only wrap proving and leave verification elsewhere, the public API will
feel incomplete.

## Relation To `examples/c-guest`

Fixing the upstream `c-guest` example is still worthwhile, but it does not
change this recommendation.

Even if `examples/c-guest` is fully fixed, it still represents:

- non-Rust guest
- Rust host/prover

That is the same host architecture class as our current Go lane.

So a fixed `c-guest` helps as an upstream reference example, but it does not
eliminate the need for a Go-facing host boundary if we want Go applications to
drive proving directly.

## Implementation Steps (Completed)

The steps that were followed:

1. Extracted shared host logic from `go-guest-host/` into `host-core/`.
2. Kept `go-guest-host/` working on top of `host-core/`.
3. Added `host-ffi/` with the minimal JSON-over-buffer C ABI.
4. Added the Go `host/` package with cgo wrappers and `dlopen`/`dlsym`.
5. Ported `bip32-pq-zkp` to consume `github.com/roasbeef/go-zkvm/host`.

## Bottom Line

The practical near-term goal is:

- **Go-facing proving API**

not:

- **pure-Go proving engine**

That is the right engineering boundary for this project because it improves Go
usability while preserving the validated upstream risc0 proving stack.
