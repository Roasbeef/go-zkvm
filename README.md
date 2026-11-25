# go-zkvm

`go-zkvm` is a TinyGo-first toolkit for writing zero-knowledge guest programs
in Go and proving them with the RISC Zero zkVM. A guest runs inside the VM,
reads private input, computes on it, and commits public output. The proof
guarantees the computation was done correctly without revealing the private
input.

Write a guest:

```go
package main

import "github.com/roasbeef/go-zkvm/zkvm"

func main() {
    // Private witness: only the prover sees these.
    var a, b uint64
    zkvm.ReadValue(&a)
    zkvm.ReadValue(&b)

    // Public claim: the verifier sees the product.
    product := a * b
    zkvm.CommitValue(&product)
    zkvm.Halt(0)
}
```

Prove and verify it from Go:

```go
guest, _ := host.ReadGuestFile("./multiply.bin")

client, _ := host.New()
defer client.Close()

result, _ := client.Prove(host.ProveRequest{
    GuestBinary: guest,
    Stdin:       witnessBytes,
}, host.WithReceiptKind(host.ReceiptKindSuccinct))

verified, _ := client.Verify(host.VerifyRequest{
    Receipt:         result.Receipt,
    ImageID:         result.ImageID,
    ExpectedJournal: result.Journal,
})
```

The goal is not "make one demo work", but "make Go a repeatable guest language
for risc0".

## Architecture

```mermaid
flowchart TD
    subgraph guest [Guest Build Pipeline]
        A[Go guest source] --> B[TinyGo zkvm-platform]
        B --> C[Guest ELF + libzkvm_platform.a]
        C --> D[R0BF packer + v1compat.elf]
        D --> E[Packaged guest .bin]
    end

    subgraph host [Host Side - Go]
        F[Go application] --> G[host.Client]
        G --> Execute
        G --> Prove
        G --> Verify
    end

    subgraph ffi [FFI Boundary - Rust]
        Execute --> L[host-ffi cdylib]
        Prove --> L
        Verify --> L
        L --> M[host-core]
        M --> N[risc0-zkvm prover]
    end

    E -.-> Execute
    E -.-> Prove
    N -.-> F
```

## Data Flow

```mermaid
flowchart LR
    subgraph private [Private Witness]
        S[Seed] --> W[Witness bytes]
        P[Path] --> W
    end

    W -->|stdin| Guest
    Guest -->|journal fd=3| Journal[Public Journal]
    Guest -->|halt digest| Proof[STARK Proof]

    Journal --> Receipt
    Proof --> Receipt

    subgraph public [Public Output]
        Receipt -->|verify| V{Valid?}
        V -->|yes| Claim[Verified Claim]
    end
```

## What This Repo Provides

- `zkvm/`: guest-side Go API: read private input, commit public journal
  output, journal digest finalization, recursive assumption helpers, cycle
  counting
- `host/`: typed Go host API for `ComputeImageID`, `Execute`, `Prove`, and
  `Verify`, backed by the Rust proving engine via FFI
- `examples/`: three sample guests (simple, multiply, policy_check)
- `host-core/` + `host-ffi/`: shared Rust host logic and cdylib boundary
- `go-guest-host/`: Rust reference CLI for debugging and validation
- `convert_to_r0bf.go` / `extract_r0bf.go`: R0BF guest binary packer and
  unpacker
- `docs/`: architecture notes, tutorial, host API reference, and runbooks

## Current Status

The Go guests target the RISC Zero **3.1.0 stable release**. The sibling
`tinygo-zkvm` fork supplies the compiler target. The release script in the
`risc0` fork prepares an upstream checkout with the platform archive build,
v1compat kernel, and host prover and verifier.

`host.Prove` defaults to composite receipts. Pass
`WithReceiptKind(ReceiptKindSuccinct)` to request a compressed proof. A guest
can call `zkvm.Verify` on a child image ID and journal, and the host can supply
the corresponding succinct receipt as an assumption.

Historical notes on the older handwritten syscall path still exist in `docs/`,
but the supported user-facing flow in this repo is the archive-linked one.

## Supported Sample Set

The sample guests exercise the following journal outputs.

| Guest | Expected public output |
| --- | --- |
| `simple` | Empty journal |
| `multiply` | Product `391` for the built-in witness |
| `policy_check` | Three items, approved, subtotal `245`, discount `20`, total `225`, limit `250` |

Use `make verify-samples` to print image IDs and seal sizes for your build.
Regenerate stored receipts, including assumptions used for composition, with
the 3.1.0 prover. Rebuild the Rust host library alongside the Go host package.
The new Go client checks for ABI version 3 when loading the library.

## Intended Repository Layout

The intended sibling layout is:

```text
github.com/roasbeef/
├── risc0-port
├── risc0
├── tinygo-zkvm
├── go-zkvm
└── bip32-pq-zkp
```

`risc0-port` contains the release patch and preparation script. The build reads
its RISC Zero sources from the prepared `risc0` directory.

## Quick Start

### Prerequisites

- Go `1.24.4`
- Rust `1.97.1` on the host and the RISC Zero guest Rust toolchain
- sibling checkouts of:
  - `../risc0`
  - `../tinygo-zkvm`
- on Apple Silicon, a working local Metal environment is recommended for proof
  speed

Follow the [fresh-clone and receipt migration instructions](docs/running.md#prerequisites)
for the pinned sibling revisions and RISC Zero tooling.

Fresh-clone notes:

- in `../tinygo-zkvm`, run `git submodule update --init --recursive`
- in `../tinygo-zkvm`, run `make llvm-source` once before the first
  external-LLVM build so the repo-local Clang/LLD headers are present

If your default `go` in `PATH` is newer than TinyGo currently supports on this
lane, set:

```bash
export GO_GOROOT=/path/to/go1.24.4
```

before running `make`.

### Build Sample Guests

From the repo root:

```bash
make platform-standalone
make simple
make multiply
make policy-check
```

These targets compile with TinyGo target `zkvm-platform`, link against
`libzkvm_platform.a`, and package the final `.bin` guest image.

`make platform-standalone` proxies to the sibling `risc0/examples/c-guest`
target that builds a deterministic platform archive against the pinned
release dependencies. That is now the preferred path when you want stable guest artifacts
across different `risc0` checkout directories.

To rebuild and run the currently supported sample set end to end:

```bash
make verify-samples
```

That target runs the reference CLI in `go-guest-host/` against the published
sample guests. It proves and verifies inline, but does not persist receipt
files by default.

To build and validate the FFI-backed Go host layer:

```bash
make host-ffi
make test-host-ffi
```

### Reference CLI: Execute Without Proving

From `go-guest-host/`:

```bash
cargo run --release -- ../simple.bin --raw-journal --execute-only
```

Use this path when you want the Rust reference CLI directly. For normal Go
integration, prefer the typed `host/` package.

### Reference CLI: Prove And Verify

From `go-guest-host/`:

```bash
cargo run --release -- ../simple.bin --raw-journal
```

The host computes the image ID, runs the configured risc0 prover backend,
verifies the receipt, and prints the committed journal bytes plus the receipt
proof seal size. The current validated lane for this repo is local proving.
This reference CLI validates the proof inline and prints receipt metadata, but
does not write a receipt file unless you add that behavior yourself.

## Go Host API

The host-side Go package is:

```text
github.com/roasbeef/go-zkvm/host
```

Library loading policy:

- `host.WithLibraryPath(...)` is the strongest override
- otherwise `host` checks `GO_ZKVM_HOST_LIBRARY_PATH`
- otherwise it falls back to the sibling-layout build path under
  `host-ffi/target/release/`

If you want proof artifacts on disk from the Go API, persist
`proveResult.Receipt` yourself and write any verifier-facing metadata alongside
it.

Important boundary note:

- the public Go API is typed and byte-oriented
- the Rust `cdylib` underneath uses a small JSON-over-buffer ABI internally
- that JSON layer is only the control envelope between Go and Rust
- it is not part of the guest witness format, journal format, or receipt format

For the full API reference, see `docs/host-api.md`.

## What Had To Change In TinyGo

Getting Go onto the zkVM required more than adding a target JSON:

- a TinyGo target that matches the risc0 guest memory map
- startup/runtime wiring that works for the zkVM environment
- task stack/runtime build-tag fixes for TinyGo's RISC-V runtime selection
- a reliable way to link upstream `libzkvm_platform.a`
- a clean split between the legacy handwritten-syscall path and the newer
  archive-linked path

The short consumer-oriented version is documented here:

- `docs/tinygo-zkvm-target.md`

The sibling `tinygo-zkvm` repo carries the fork-local deep dive for the actual
patches.

## Documentation

Start with:

- `docs/tutorial.md`: guided walkthrough from first guest to real application
- `docs/host-api.md`: full Go host API reference
- `docs/go-zkvm-overview.md`: end-to-end architecture
- `docs/README.md`: reading order and topic map
- `docs/running.md`: build, execute, prove commands
