# Go FFI API Plan

This document turns the earlier boundary note into the concrete API and ABI
plan for the first `cdylib`-backed Go host integration.

The goal here is not to finish the implementation in prose. The goal is to
freeze the shape of the first usable interface before we write Rust FFI glue
and Go `cgo` wrappers.

## Summary

The first FFI-backed host surface should look like this:

- a new public Go package:
  - `github.com/roasbeef/go-zkvm/host`
- a new Rust shared library:
  - `libgo_zkvm_host`
- core operations:
  - `ComputeImageID`
  - `Execute`
  - `Prove`
  - `Verify`
- core data model:
  - guest input is a packaged R0BF guest binary as `[]byte`
  - private witness input is raw stdin bytes as `[]byte`
  - public output is raw journal bytes plus generic metadata
  - receipts are returned as serialized bytes, not as Go-native decoded Rust
    internals

The package should feel like "prove from Go", while still delegating the real
prover implementation to Rust.

## Design Principles

1. Keep the generic host API generic.
   - `go-zkvm` should expose raw guest execution/proving primitives.
   - demo-specific claim decoding belongs in higher-level repos such as
     `bip32-pq-zkp`.
2. Make the Go API typed and ergonomic.
   - normal Go structs
   - byte slices for guest, witness, journal, and receipt artifacts
   - clear errors and predictable ownership
3. Keep the C ABI minimal.
   - avoid freezing a large forest of `repr(C)` structs too early
   - version the wire format explicitly
4. Keep secrets in memory by default.
   - no CLI args
   - no required temp files
5. Preserve the current validated Rust behavior.
   - image ID computation stays in Rust
   - proving and receipt verification stay in Rust
   - Metal or CPU backend selection still comes from `risc0-zkvm`

## Public Go Package

The new public package should be:

```text
go-zkvm/
└── host/
```

That package is host-side only. It is separate from:

- `zkvm/`
  - guest-side Go package used inside TinyGo guests

This split matters. We should not overload `zkvm/` with host responsibilities.

## Go API Shape

The first API should be client-based, with package-level helpers only if they
are trivial wrappers.

Suggested shape:

```go
package host

type Client struct {
    // unexported ffi handle/config
}

type ClientOption func(*clientConfig)

func New(opts ...ClientOption) (*Client, error)
func (c *Client) Close() error

func (c *Client) ComputeImageID(guest []byte) (string, error)
func (c *Client) Execute(req ExecuteRequest, opts ...RunOption) (*ExecuteResult, error)
func (c *Client) Prove(req ProveRequest, opts ...RunOption) (*ProveResult, error)
func (c *Client) Verify(req VerifyRequest) (*VerifyResult, error)
```

Reasons for `Client` instead of only package-level functions:

- leaves room for future per-client configuration
- gives us a place to hang lifecycle and library-loading behavior
- keeps the API extensible if we later add caching or backend hints

## Core Request / Result Types

The core API should be bytes-first and path-free.

The Go wrapper can offer helper functions that read files, but the main API
should not require the Rust side to do filesystem IO.

Suggested v1 types:

```go
type ExecuteRequest struct {
    GuestBinary []byte
    Stdin       []byte
}

type ExecuteResult struct {
    ImageID     string
    Journal     []byte
    ExitCode    string
    SegmentCount uint32
    SessionRows uint64
}

type ProveRequest struct {
    GuestBinary []byte
    Stdin       []byte
}

type ProveResult struct {
    ImageID         string
    Journal         []byte
    Receipt         []byte
    ReceiptEncoding string
    ProverName      string
    SealBytes       uint64
}

type VerifyRequest struct {
    Receipt         []byte
    ImageID         string
    ExpectedJournal []byte
}

type VerifyResult struct {
    Verified bool
}
```

Notes:

- `GuestBinary` means the final packaged `.bin`, not a raw user ELF.
- `Stdin` is raw private witness bytes exactly as the guest expects them.
- `Verify` should take an explicit `ImageID`, not a guest binary.
  - callers that have the guest bytes can compute the image ID once with
    `ComputeImageID`.
- `ExpectedJournal` is optional.
  - if set, verification should also compare the receipt journal bytes.

## Convenience Helpers

The public package should also offer small path helpers so normal Go callers do
not need to reimplement file reads:

```go
func ReadGuestFile(path string) ([]byte, error)
func ComputeImageIDFile(path string) (string, error)
func ProveFile(path string, stdin []byte, opts ...RunOption) (*ProveResult, error)
```

These helpers should stay thin wrappers around the byte-oriented API.

## Options Pattern

Use Go function options only where the setting is actually generic.

Suggested v1 options:

```go
type RunOption func(*runConfig)

func WithLogger(logger *slog.Logger) RunOption
func WithReceiptSelfVerify(enabled bool) RunOption
func WithExpectedJournal(journal []byte) RunOption
```

Guidance:

- `WithReceiptSelfVerify(true)` should remain the default for `Prove`.
- `WithExpectedJournal` is mainly useful for `Verify`.
- do not put demo-specific flags here
  - for example, `RequireBIP86` belongs in `bip32-pq-zkp`, not generic
    `go-zkvm/host`

## Context Semantics

The v1 API should not pretend that long-running FFI calls are fully
cancellable.

Two viable choices are:

1. omit `context.Context` from v1
2. include `context.Context`, but document it as best-effort only

Recommendation:

- omit `context.Context` from the first FFI surface
- add it only when we have a real interruption strategy

Reason:

- once Go enters a long-running `cgo` call, cancellation semantics are not the
  same as normal Go IO APIs
- pretending otherwise would make the API misleading

## What The Go API Should Return

The generic package should return raw artifacts and metadata, not application
claims.

So `go-zkvm/host` should return:

- `ImageID`
- raw `Journal`
- raw serialized `Receipt`
- `ReceiptEncoding`
- `ProverName`
- `SealBytes`

It should not decode:

- BIP-32 claims
- Taproot claims
- policy-check summaries

Those belong in higher-level packages or demo repos that understand those
schemas.

## Rust Crate Layout

The Rust side should be split so the current CLI host and the new FFI boundary
share the same core logic.

Suggested layout:

```text
go-zkvm/
├── go-guest-host/
│   └── src/main.rs
├── host-core/
│   └── src/lib.rs
└── host-ffi/
    └── src/lib.rs
```

Responsibilities:

- `host-core/`
  - generic Rust logic for:
    - image ID computation
    - execute
    - prove
    - verify
    - journal extraction
    - receipt serialization
- `go-guest-host/`
  - debug/reference CLI
  - should call into `host-core`
- `host-ffi/`
  - `cdylib` wrapper that exposes the stable C ABI

This avoids reimplementing the host flow twice.

## C ABI Plan

The raw C ABI should stay intentionally small.

Recommended exported functions:

```c
uint32_t go_zkvm_abi_version(void);

int32_t go_zkvm_compute_image_id(
    const uint8_t* req_ptr, size_t req_len,
    uint8_t** out_ptr, size_t* out_len);

int32_t go_zkvm_execute(
    const uint8_t* req_ptr, size_t req_len,
    uint8_t** out_ptr, size_t* out_len);

int32_t go_zkvm_prove(
    const uint8_t* req_ptr, size_t req_len,
    uint8_t** out_ptr, size_t* out_len);

int32_t go_zkvm_verify(
    const uint8_t* req_ptr, size_t req_len,
    uint8_t** out_ptr, size_t* out_len);

void go_zkvm_free_buffer(uint8_t* ptr, size_t len);
```

Where:

- request bytes are an ABI-versioned JSON payload
- response bytes are an ABI-versioned JSON payload
- Rust allocates the response buffer
- Go copies it into a Go slice, then calls `go_zkvm_free_buffer`

Why JSON for the first ABI:

- avoids brittle cross-language struct packing
- keeps the C boundary tiny
- makes debugging easier during bring-up
- request/response evolution is simpler

Why this is acceptable despite extra copies:

- proof generation dominates runtime by orders of magnitude
- proof artifacts are already byte-oriented
- the simplification is worth the overhead for v1

If profiling later shows the JSON boundary is meaningfully expensive, then v2
can move to a flatter binary envelope or opaque guest/receipt handles.

## FFI Schema Shape

The JSON payloads should be internal ABI messages, not the public Go API.

Suggested request/response messages:

```text
ComputeImageIDRequest
  abi_version
  guest_binary_base64

ComputeImageIDResponse
  image_id

ExecuteRequest
  abi_version
  guest_binary_base64
  stdin_base64

ExecuteResponse
  image_id
  journal_base64
  exit_code
  segment_count
  session_rows

ProveRequest
  abi_version
  guest_binary_base64
  stdin_base64
  verify_receipt

ProveResponse
  image_id
  journal_base64
  receipt_base64
  receipt_encoding
  prover_name
  seal_bytes

VerifyRequest
  abi_version
  receipt_base64
  image_id
  expected_journal_base64

VerifyResponse
  verified
```

The public Go wrapper should hide this JSON layer entirely.

## Error Model

The Rust FFI layer should never panic across the ABI boundary.

Rules:

- Rust must wrap every exported function in `catch_unwind`
- all failures become structured JSON errors
- exported functions should return a non-zero status code on failure

Suggested Go error type:

```go
type HostError struct {
    Op      string
    Code    string
    Message string
}
```

Suggested Rust-side error codes:

- `invalid_request`
- `invalid_guest_binary`
- `invalid_receipt`
- `image_id_mismatch`
- `journal_mismatch`
- `execute_failed`
- `prove_failed`
- `verify_failed`
- `internal_error`

## Memory Ownership Rules

These rules should be explicit in both Rust and Go code comments.

Inputs:

- Go owns request buffers
- Rust borrows them only for the duration of the call

Outputs:

- Rust allocates response buffers
- Go copies them into Go-managed memory
- Go must call `go_zkvm_free_buffer`

Never:

- call `C.free` on Rust-allocated memory
- hold Rust-returned pointers after calling `go_zkvm_free_buffer`
- let Rust retain Go pointers after the exported function returns

## Backend And Hardware Semantics

The FFI layer should not invent a new proving backend model.

In v1:

- Rust still uses the normal `risc0-zkvm` default executor/prover selection
- Apple Silicon Metal acceleration remains a Rust-side concern
- the Go result should report the selected prover name
- the Go API should not promise explicit Metal or CPU selection yet

That keeps the first release aligned with what is already validated locally.

## Packaging And Build Plan

The first dev-facing build flow should be simple and explicit.

Suggested targets:

```text
make host-ffi
make test-host-ffi
```

`make host-ffi` should:

- build the Rust `cdylib`
- place the shared library where the Go `cgo` wrapper expects it for local dev

The Go package should also include:

- a `//go:build cgo` implementation
- a non-`cgo` stub that returns a clear error explaining that the FFI-backed
  host package requires `cgo`

## First Milestone

The first milestone should be intentionally narrow.

Implement only:

1. `ComputeImageID`
2. `Execute`
3. `Prove`
4. `Verify`

with only:

- packaged guest `.bin` input
- raw stdin witness bytes
- raw journal bytes
- raw serialized receipt bytes

Do not block v1 on:

- generic callback-based journal decoding
- proof cancellation
- remote proving
- guest ELF input
- demo-specific claim helpers
- advanced backend selection

## What Moves Out Of Demo Repos

Once this exists, repos like `bip32-pq-zkp` should be able to treat proving as
normal host library usage:

- build the witness bytes in Go
- call `host.Prove`
- decode the returned journal as the demo claim
- optionally persist the receipt and claim JSON

That keeps `go-zkvm` generic while still making the demo repos smaller and more
ergonomic.

## Recommended Implementation Order

1. extract the generic Rust host flow into `host-core`
2. keep `go-guest-host` working on top of `host-core`
3. add `host-ffi` with the minimal JSON-over-buffer ABI
4. add the Go `host` package with `cgo` wrappers
5. port one existing sample in `go-zkvm` to the new API
6. then port `bip32-pq-zkp`

That sequence gives us a checkpoint after each layer instead of forcing the
whole stack to land at once.
