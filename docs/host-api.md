# Host Package API Reference

## Overview

Package `host` is a Go wrapper around a Rust FFI boundary that delegates to the
risc0 STARK prover. It lets Go applications execute guest programs inside a zkVM,
generate zero-knowledge proofs, and verify receipts -- all without writing Rust.

The package operates under a clear privacy model. The guest binary's **stdin**
carries private witness data: it is visible to the prover but never appears in
the proof. The **journal** is the public claim: it is committed during execution
and bound into the receipt so that any verifier can read it. A correct proof
demonstrates that the guest program, when run on the private witness, produced
exactly the journal bytes committed in the receipt.

Import path:

```go
import "github.com/roasbeef/go-zkvm/host"
```

---

## Client Lifecycle

### Creating a Client

```go
func New(opts ...ClientOption) (*Client, error)
```

`New` constructs a `Client` and immediately loads the Rust shared library via
`dlopen`. The library is loaded exactly once per process; subsequent `New` calls
that resolve to the same path reuse the already-loaded handle. If a second call
resolves to a *different* path, `New` returns an error.

Library resolution follows a strict precedence order:

1. **Explicit option** -- `WithLibraryPath(path)` passed to `New`. This is the
   strongest override and is evaluated first.
2. **Environment variable** -- `GO_ZKVM_HOST_LIBRARY_PATH`. Checked when no
   explicit path is provided.
3. **Sibling-layout fallback** -- the package uses `runtime.Caller` to locate
   its own source file, then looks for
   `../host-ffi/target/release/libgo_zkvm_host.{dylib,so}` relative to that
   location. This works for the normal checked-out `go-zkvm` repo layout,
   where `host-ffi/` lives inside the same repository as the `host/` package.

The resolved path must be non-empty; otherwise `New` returns an error.

### Closing a Client

```go
func (c *Client) Close() error
```

`Close` releases the `Client`. The current implementation performs no teardown
work (the shared library remains loaded for the process lifetime), but callers
should still call `Close` -- or use `defer client.Close()` -- to remain
compatible with future versions that may release resources.

---

## Core Operations

### ComputeImageID

```go
func (c *Client) ComputeImageID(guest []byte) (string, error)
```

Computes the deterministic image ID for a packaged guest binary. The image ID is
a cryptographic hash of the guest ELF loaded into the zkVM memory image. Two
identical guest binaries always produce the same image ID regardless of the host
machine.

**Parameters:**

- `guest` -- the raw bytes of the packaged guest `.bin` artifact.

**Returns:**

- A hex-encoded image ID string.
- A `*HostError` if the guest binary is malformed or the FFI call fails.

### Execute

```go
func (c *Client) Execute(req ExecuteRequest, opts ...RunOption) (*ExecuteResult, error)
```

Runs the guest program to completion *without generating a proof*. This is
useful for development iteration, debugging guest logic, and pre-flight
validation before committing to a full proving pass. Execution is
significantly faster than proving because it skips the STARK arithmetic.

**Parameters:**

- `req` -- an `ExecuteRequest` specifying the guest binary and private stdin.
- `opts` -- zero or more `RunOption` values (e.g. `WithLogger`).

**Returns:**

- An `*ExecuteResult` containing the image ID, committed journal, exit code,
  segment count, and session row count.
- A `*HostError` on failure.

### Prove

```go
func (c *Client) Prove(req ProveRequest, opts ...RunOption) (*ProveResult, error)
```

Runs the guest program and generates a STARK proof. The result includes a
serialized receipt that any party can verify without access to the private
witness. On Apple Silicon hosts, the current validated local proving lane may
use Metal GPU acceleration transparently.

By default, `Prove` performs a Rust-side self-verification of the generated
receipt before returning. This catches prover bugs early at the cost of a small
amount of extra time. Disable this with `WithReceiptSelfVerify(false)`.

`Prove` defaults to `ReceiptKindComposite`. Callers can request a recursively
compressed STARK receipt instead with `WithReceiptKind(ReceiptKindSuccinct)`.
That usually reduces receipt size significantly, but it adds extra proving work
up front.

**Parameters:**

- `req` -- a `ProveRequest` specifying the guest binary and private stdin.
- `opts` -- zero or more `RunOption` values.

**Returns:**

- A `*ProveResult` containing the image ID, journal, serialized receipt,
  receipt encoding name, prover backend name, and seal size in bytes.
- A `*HostError` on failure.

### Verify

```go
func (c *Client) Verify(req VerifyRequest) (*VerifyResult, error)
```

Verifies a serialized receipt against an expected image ID. Optionally checks
that the committed journal matches expected bytes. Verification is fast and
requires no access to the original witness data.

**Parameters:**

- `req` -- a `VerifyRequest` specifying the receipt bytes, expected image ID,
  and optional expected journal.

**Returns:**

- A `*VerifyResult` reporting whether verification succeeded, the verified
  journal, receipt encoding, and seal size.
- A `*HostError` if the receipt is malformed, the image ID does not match, or
  the journal comparison fails.

---

## Request and Result Types

### ExecuteRequest

```go
type ExecuteRequest struct {
    // GuestBinary is the packaged guest .bin artifact to execute.
    GuestBinary []byte
    // Stdin is the raw private witness stream fed into guest stdin.
    Stdin []byte
}
```

| Field         | Type     | Description |
|---------------|----------|-------------|
| `GuestBinary` | `[]byte` | The compiled guest program bytes. Required. |
| `Stdin`       | `[]byte` | Private witness data delivered to the guest via stdin. May be nil for guests that take no input. |

### ExecuteResult

```go
type ExecuteResult struct {
    ImageID      string
    Journal      []byte
    ExitCode     string
    SegmentCount uint32
    SessionRows  uint64
}
```

| Field          | Type     | Description |
|----------------|----------|-------------|
| `ImageID`      | `string` | Hex-encoded image ID of the loaded guest. |
| `Journal`      | `[]byte` | Raw bytes committed to the public journal by the guest. |
| `ExitCode`     | `string` | Exit summary reported by the executor (e.g. `"halted"`). |
| `SegmentCount` | `uint32` | Number of zkVM segments the execution was split into. |
| `SessionRows`  | `uint64` | Total row count across all segments in the session. Useful for estimating proof cost. |

### ProveRequest

```go
type ProveRequest struct {
    // GuestBinary is the packaged guest .bin artifact to prove.
    GuestBinary []byte
    // Stdin is the raw private witness stream fed into guest stdin.
    Stdin []byte
}
```

| Field         | Type     | Description |
|---------------|----------|-------------|
| `GuestBinary` | `[]byte` | The compiled guest program bytes. Required. |
| `Stdin`       | `[]byte` | Private witness data. May be nil. |

### ProveResult

```go
type ProveResult struct {
    ImageID         string
    Journal         []byte
    Receipt         []byte
    ReceiptEncoding string
    ReceiptKind     ReceiptKind
    ProverName      string
    SealBytes       uint64
}
```

| Field             | Type     | Description |
|-------------------|----------|-------------|
| `ImageID`         | `string` | Hex-encoded image ID of the loaded guest. |
| `Journal`         | `[]byte` | Raw bytes committed to the public journal. |
| `Receipt`         | `[]byte` | Serialized risc0 receipt. Pass this to `Verify`. |
| `ReceiptEncoding` | `string` | Name of the encoding used for the receipt (currently `"borsh"` on the documented lane). |
| `ReceiptKind`     | `ReceiptKind` | Concrete receipt representation returned by the prover, such as `composite` or `succinct`. |
| `ProverName`      | `string` | Identifies the proving backend that was selected (e.g. `"local"`). |
| `SealBytes`       | `uint64` | Size of the proof seal portion in bytes. |

### VerifyRequest

```go
type VerifyRequest struct {
    // Receipt is the serialized receipt bytes to verify.
    Receipt []byte
    // ImageID is the expected guest image ID.
    ImageID string
    // ExpectedJournal optionally checks the committed journal bytes too.
    ExpectedJournal []byte
}
```

| Field             | Type     | Description |
|-------------------|----------|-------------|
| `Receipt`         | `[]byte` | The serialized receipt to verify. Required. |
| `ImageID`         | `string` | Expected image ID. Verification fails if the receipt's image ID differs. Required. |
| `ExpectedJournal` | `[]byte` | If non-nil, verification also checks that the receipt's committed journal matches these bytes exactly. |

### VerifyResult

```go
type VerifyResult struct {
    Verified        bool
    Journal         []byte
    ReceiptEncoding string
    ReceiptKind     ReceiptKind
    SealBytes       uint64
}
```

| Field             | Type     | Description |
|-------------------|----------|-------------|
| `Verified`        | `bool`   | `true` if the receipt passed all checks. |
| `Journal`         | `[]byte` | The journal extracted from the verified receipt. |
| `ReceiptEncoding` | `string` | Encoding of the receipt that was verified. |
| `ReceiptKind`     | `ReceiptKind` | Concrete receipt representation that was verified. |
| `SealBytes`       | `uint64` | Size of the proof seal in bytes. |

---

## Convenience Helpers

These package-level functions create a temporary `Client` internally and close it
after the call returns. They are convenient for one-shot operations where you do
not need to reuse the client across multiple calls.

### ReadGuestFile

```go
func ReadGuestFile(path string) ([]byte, error)
```

Reads a packaged guest `.bin` artifact from disk. This is a thin wrapper around
`os.ReadFile` provided for symmetry with the other `*File` helpers.

### ComputeImageIDFile

```go
func ComputeImageIDFile(path string) (string, error)
```

Reads a guest binary from `path`, creates a temporary `Client`, computes the
image ID, and returns it. Equivalent to:

```go
guest, _ := host.ReadGuestFile(path)
client, _ := host.New()
defer client.Close()
id, _ := client.ComputeImageID(guest)
```

### ExecuteFile

```go
func ExecuteFile(path string, stdin []byte, opts ...RunOption) (*ExecuteResult, error)
```

Reads a guest binary from `path` and executes it with the provided stdin bytes.
Accepts the same `RunOption` values as `Client.Execute`.

### ProveFile

```go
func ProveFile(path string, stdin []byte, opts ...RunOption) (*ProveResult, error)
```

Reads a guest binary from `path` and proves it with the provided stdin bytes.
Accepts the same `RunOption` values as `Client.Prove`.

---

## Options

The package uses two distinct option types to separate construction-time
configuration from per-call behavior.

### ClientOption

`ClientOption` values are passed to `New` and affect the client for its entire
lifetime.

#### WithLibraryPath

```go
func WithLibraryPath(path string) ClientOption
```

Overrides the shared-library path used by `New`. This takes precedence over the
`GO_ZKVM_HOST_LIBRARY_PATH` environment variable and the sibling-layout
fallback.

### RunOption

`RunOption` values are passed to `Execute` and `Prove` and affect only that
single call.

#### WithLogger

```go
func WithLogger(logger *slog.Logger) RunOption
```

Attaches a `slog.Logger` that receives a structured info-level message at the
start of the FFI call. The message includes the operation name, guest binary
size, and stdin size. Useful for tracing proof generation in larger systems.

#### WithReceiptSelfVerify

```go
func WithReceiptSelfVerify(enabled bool) RunOption
```

Controls whether `Prove` asks the Rust side to verify the receipt immediately
after generating it. Defaults to `true`. Set to `false` to skip the self-check
when you plan to verify separately or need to shave a few seconds off the
proving path.

#### WithReceiptKind

```go
func WithReceiptKind(kind ReceiptKind) RunOption
```

Selects the minimum receipt compression level requested for `Prove`.
`ReceiptKindComposite` is the default. `ReceiptKindSuccinct` asks the Rust
prover to return a recursively compressed STARK receipt instead. Verification
works the same way for both; the receipt kind is reported back in
`ProveResult` and `VerifyResult`.

---

## Error Model

All errors returned by the host package are `*HostError` values (or standard
`error` values for pre-FFI failures like missing library paths).

```go
type HostError struct {
    Op      string  // The host operation that failed.
    Code    string  // Stable machine-readable error code.
    Message string  // Human-readable failure detail.
}
```

The `Error()` method formats as `"op (code): message"` when a code is present,
or `"op: message"` otherwise.

### Error Codes

| Code                    | Meaning |
|-------------------------|---------|
| `invalid_request`       | The JSON request was malformed or missing required fields. |
| `invalid_guest_binary`  | The guest binary could not be loaded into the zkVM. |
| `invalid_receipt`       | The receipt bytes could not be deserialized. |
| `image_id_mismatch`     | The receipt's image ID does not match the expected value. |
| `journal_mismatch`      | The receipt's journal does not match the expected bytes. |
| `execute_failed`        | Guest execution terminated with an error. |
| `prove_failed`          | Proof generation failed. |
| `verify_failed`         | Receipt verification failed. |
| `internal_error`        | An unexpected error occurred in the Rust host or FFI layer. Also used as a fallback when the Rust side returns an empty error code. |

You can type-assert errors to inspect the code programmatically:

```go
result, err := client.Prove(req)
if err != nil {
    var hostErr *host.HostError
    if errors.As(err, &hostErr) {
        fmt.Printf("code=%s message=%s\n", hostErr.Code, hostErr.Message)
    }
}
```

---

## Memory and Ownership

The FFI boundary follows a strict ownership protocol:

1. **Go owns request buffers.** The Go side marshals a JSON request into a
   `[]byte`, passes a pointer and length to the C shim, and the Rust side reads
   from that buffer synchronously. Go may free or reuse the buffer after the FFI
   call returns.

2. **Rust allocates response buffers.** The Rust side allocates a response buffer
   and writes its pointer and length into out-parameters provided by the C shim.

3. **Go copies and frees.** Immediately after the FFI call returns, Go copies the
   response bytes into a Go-managed `[]byte` using `C.GoBytes`, then calls
   `go_zkvm_free_buffer` to return the memory to the Rust allocator.

Never call `C.free` on a Rust-allocated buffer. The Rust allocator and the C
allocator are separate; freeing with the wrong allocator is undefined behavior.
The `go_zkvm_free_buffer` function is the only correct way to release
Rust-allocated memory.

---

## FFI Boundary

The host package uses a JSON-over-buffer ABI to communicate with the Rust shared
library. Each operation follows the same pattern:

1. Go marshals a request struct to JSON. Binary payloads (guest binary, stdin,
   receipt) are base64-encoded within the JSON.
2. Go passes the JSON bytes as a `(uint8_t*, size_t)` pair to a C shim function.
3. The C shim forwards the call to a Rust-exported function resolved via
   `dlsym`.
4. Rust deserializes the JSON, performs the operation, serializes the response
   (or error) to JSON, allocates a buffer, and writes the result.
5. Go copies the response buffer and frees it via `go_zkvm_free_buffer`.
6. Go unmarshals the JSON response into a typed Go struct.

### Exported C Functions

The Rust shared library exports six symbols, resolved at runtime via
`dlopen`/`dlsym`:

| Symbol                     | Purpose |
|----------------------------|---------|
| `go_zkvm_abi_version`      | Returns the ABI version number (currently `1`). |
| `go_zkvm_compute_image_id` | Computes the image ID for a guest binary. |
| `go_zkvm_execute`          | Executes a guest without proof generation. |
| `go_zkvm_prove`            | Executes a guest and generates a STARK proof. |
| `go_zkvm_verify`           | Verifies a receipt against an image ID. |
| `go_zkvm_free_buffer`      | Frees a Rust-allocated response buffer. |

All operation functions share the same C signature:

```c
int32_t fn(const uint8_t* req_ptr, size_t req_len,
           uint8_t** out_ptr, size_t* out_len);
```

A return value of `0` indicates success; any other value indicates failure, and
the output buffer contains a JSON-encoded error response.

### Why JSON

JSON avoids brittle struct packing across the Go/Rust boundary. It is trivial to
debug with `printf`, easy to extend with new fields without breaking ABI
compatibility, and the serialization overhead is negligible because proof
generation dominates wall-clock time by orders of magnitude. Base64 encoding of
binary payloads adds roughly 33% size overhead, which is similarly irrelevant
next to the cost of STARK arithmetic.

### Runtime Loading

The library is loaded via `dlopen` at runtime rather than linked at compile time.
This means the Rust shared library does not need to be present when compiling the
Go code -- only when running it. The `dlopen` call happens inside `New()` and is
guarded by a `sync.Mutex` to ensure it executes exactly once.

---

## Build Requirements

### cgo

The host package requires cgo. When cgo is disabled (e.g.
`CGO_ENABLED=0`), the package compiles with a stub implementation
(`ffi_nocgo.go`) where every method returns a clear error:

```
host: the FFI-backed host package requires cgo
```

### Building the Rust Shared Library

The Rust cdylib must be built before running any Go code that uses the host
package. The typical build command is:

```bash
make host-ffi
```

This produces `libgo_zkvm_host.dylib` (macOS) or `libgo_zkvm_host.so` (Linux)
under `host-ffi/target/release/`. The build requires a sibling checkout of the
risc0 repository for the Rust prover dependencies.

### Linux Link Flags

On Linux, the package automatically adds `-ldl` to the linker flags (via a
`#cgo linux LDFLAGS: -ldl` directive) so that `dlopen` and `dlsym` are
available.

---

## Complete Worked Example

```go
package main

import (
    "fmt"
    "log"

    "github.com/roasbeef/go-zkvm/host"
)

func main() {
    guest, err := host.ReadGuestFile("./my-guest.bin")
    if err != nil {
        log.Fatal(err)
    }

    client, err := host.New()
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    // Execute first to check the guest runs correctly.
    execResult, err := client.Execute(host.ExecuteRequest{
        GuestBinary: guest,
        Stdin:       witnessBytes,
    })
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("image ID: %s\n", execResult.ImageID)
    fmt.Printf("journal:  %x\n", execResult.Journal)

    // Generate a proof.
    proveResult, err := client.Prove(host.ProveRequest{
        GuestBinary: guest,
        Stdin:       witnessBytes,
    })
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("seal: %d bytes\n", proveResult.SealBytes)

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

This program reads a guest binary, runs it once in execute-only mode to inspect
the output, then generates a full STARK proof and verifies the resulting receipt.
The `witnessBytes` variable (assumed to be defined elsewhere) carries the private
input data that the guest reads from stdin.
