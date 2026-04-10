# Running go-zkvm

This runbook assumes the sibling layout:

```text
github.com/roasbeef/
├── risc0
├── tinygo-zkvm
└── go-zkvm
```

## Prerequisites

- Go `1.24.4`
- Rust toolchain compatible with the checked-out `risc0`
- a built TinyGo fork in `../tinygo-zkvm`
- a built risc0 platform archive in `../risc0/examples/c-guest`

Fresh-clone setup notes:

- in `../tinygo-zkvm`, run `git submodule update --init --recursive`
- in `../tinygo-zkvm`, run `make llvm-source` once before the first
  external-LLVM build so the repo-local Clang/LLD headers are available
- in `../risc0`, run `git lfs pull` before building the Rust host/prover path

If your shell default is a newer Go release, export the GOROOT TinyGo should
use:

```bash
export GO_GOROOT=/path/to/go1.24.4
```

## Build The TinyGo Fork

From `tinygo-zkvm/`:

```bash
make llvm-source
LLVM_BUILDDIR=/opt/homebrew/opt/llvm \
CGO_LDFLAGS_EXTRA='-L/opt/homebrew/lib' \
CLANG_EXTRA_LIB_NAMES='clangARCMigrate clangStaticAnalyzerCore clangStaticAnalyzerFrontend clangStaticAnalyzerCheckers' \
make
```

That is the concrete local recipe used on the current Apple Silicon lane.
Even when you reuse a system LLVM install, the TinyGo build still needs the
repo-local `llvm-project` source checkout for Clang and LLD headers.

## Build The risc0 Platform Archive

From `risc0/examples/c-guest/`:

```bash
make platform-standalone
```

That produces:

```text
examples/c-guest/guest/out/platform/riscv32im-risc0-zkvm-elf/release/libzkvm_platform.a
```

`platform-standalone` is the preferred path now. It builds the archive through
a temporary standalone Cargo package pinned to the published risc0 git commit,
which avoids the checkout-path sensitivity seen in the older workspace-local
`make platform` flow.

## Build Sample Guests

From the `go-zkvm` repo root:

```bash
export GO_GOROOT=/path/to/go1.24.4
make platform-standalone
make simple
make multiply
make policy-check
```

Each target does three things:

1. compiles the guest ELF with TinyGo target `zkvm-platform`
2. links the guest against `libzkvm_platform.a`
3. packages the ELF with `v1compat.elf`

To rebuild and validate the currently supported sample set in one pass:

```bash
make verify-samples
```

That target uses the Rust reference CLI in `go-guest-host/` for the actual
execute/prove/verify runs. It validates the artifacts inline and prints receipt
metadata, but it does not persist receipt files by default.

## Build The Go Host FFI Layer

From the `go-zkvm` repo root:

```bash
make host-ffi
```

That builds:

```text
host-ffi/target/release/libgo_zkvm_host.dylib
```

on macOS, or the corresponding `.so` on Linux.

To validate the typed Go wrapper against the live Rust prover:

```bash
make test-host-ffi
```

## Reference CLI: Execute Without Proving

From `go-guest-host/`:

```bash
cargo run --release -- ../simple.bin --raw-journal --execute-only
```

Use this path when you want the Rust reference CLI directly. For normal Go
integration, prefer the typed `host/` package documented below.

## Reference CLI: Prove And Verify

From `go-guest-host/`:

```bash
cargo run --release -- ../simple.bin --raw-journal
```

To request a recursively compressed receipt instead of the default composite
receipt:

```bash
cargo run --release -- ../simple.bin --raw-journal --receipt-kind succinct
```

The host will:

- compute the image ID
- run the configured risc0 prover backend
- verify the receipt
- print the committed journal bytes
- print the receipt proof seal size
- print the concrete receipt kind that was returned

The current validated lane for this repo is local proving.

For the smallest public-output sample, prove `multiply` with:

```bash
cargo run --release -- ../multiply.bin
```

## Use The Go Host Package

The supported and preferred host-side Go API is:

```text
github.com/roasbeef/go-zkvm/host
```

The package loads the Rust shared library, checks the ABI version, and then
exposes typed `ComputeImageID`, `Execute`, `Prove`, and `Verify` methods.

For composed guests, `ExecuteRequest` and `ProveRequest` also accept
`Assumptions []host.AssumptionReceipt`. Those receipts are forwarded into the
executor so guest-side calls such as `zkvm.Verify(...)` can resolve them.

Library lookup precedence is:

1. `host.WithLibraryPath(...)`
2. `GO_ZKVM_HOST_LIBRARY_PATH`
3. the sibling-layout fallback under `host-ffi/target/release/`

The fastest built-in validation path is:

```bash
CGO_ENABLED=1 go test ./host -run TestHostFFISimpleGuest -v
```

That test exercises:

- image ID computation
- execute-only
- prove
- verify

using the same live `simple.bin` artifact and the release-built `host-ffi`
library.

If you need a receipt on disk, persist `ProveResult.Receipt` yourself from the
Go API. The reference CLI examples above only print receipt metadata.

The JSON envelope used underneath the `cdylib` boundary is internal only. Guest
stdin remains raw bytes, the journal remains raw bytes, and receipts remain the
normal serialized risc0 receipt bytes.

For ordinary guests, leave `Assumptions` empty. For recursive composition, the
assumption receipts must currently be serialized succinct receipts.

## Richer Example

`policy_check` is the recommended medium-complexity sample in this repo.

Execute it with the built-in private witness through the reference CLI:

```bash
cargo run --release -- ../policy_check.bin --raw-journal --execute-only
```

Prove it through the reference CLI:

```bash
cargo run --release -- ../policy_check.bin --raw-journal
```

Override the witness from the host:

```bash
cargo run --release -- ../policy_check.bin --raw-journal \
  --policy-items=120,45,80 \
  --policy-discount=20 \
  --policy-limit=250
```

The guest commits this public summary:

- item count
- approval bit
- subtotal
- discount
- total
- limit

## Current Validated Sample Outputs

These values come from the current deterministic verification pass using
`make platform-standalone`. Treat the image IDs as artifact-specific constants
for that documented build path.

- `simple`
  - image ID: `9ac42ea490374af40aa6ca499952a133edb38df51a314b47041bf06576494f2e`
  - raw journal: empty
  - proof seal size: `203016` bytes
- `multiply`
  - image ID: `db8cb4b1a0a6045cc3e64f1eb6f2927eadd73f33bbceb261b91da1b3068e10f2`
  - public output: `391`
  - proof seal size: `203016` bytes
- `policy_check`
  - image ID: `78e9677b5db05ea0a2a5de33c54f85d5ba1724364f8f73c150949066753144ac`
  - raw journal: `0300000001000000f5000000000000001400000000000000e100000000000000fa00000000000000`
  - decoded summary:
    - item count `3`
    - approved `true`
    - subtotal `245`
    - discount `20`
    - total `225`
    - limit `250`
  - proof seal size: `203016` bytes

## Notes

- On Apple Silicon, local release proving should take the Metal-backed path by
  default.
- If you want deterministic guest artifacts across different `risc0` checkout
  paths, build the archive with `make platform-standalone` first.
- The image ID is tied to the exact built guest artifact. In the current lane,
  moving only the Go guest repo checkout path did not change the image ID when
  the same sibling `risc0`, `tinygo-zkvm`, and `go-zkvm` trees were reused.
- The older `make platform` path is still useful for local iteration, but it is
  the flow that previously showed checkout-path sensitivity in
  `libzkvm_platform.a`.
- If you are debugging basic guest behavior, use `--execute-only` first.
- The packed kernel half must come from the same current risc0 lane as the
  archive and host crates.
