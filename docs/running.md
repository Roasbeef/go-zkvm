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

If your shell default is a newer Go release, export the GOROOT TinyGo should
use:

```bash
export GO_GOROOT=/path/to/go1.24.4
```

## Build The TinyGo Fork

From `tinygo-zkvm/`:

```bash
LLVM_BUILDDIR=/opt/homebrew/opt/llvm \
CGO_LDFLAGS_EXTRA='-L/opt/homebrew/lib' \
CLANG_EXTRA_LIB_NAMES='clangARCMigrate clangStaticAnalyzerCore clangStaticAnalyzerFrontend clangStaticAnalyzerCheckers' \
make
```

That is the concrete local recipe used on the current Apple Silicon lane.

## Build The risc0 Platform Archive

From `risc0/examples/c-guest/`:

```bash
make platform
```

That produces:

```text
examples/c-guest/guest/out/platform/riscv32im-risc0-zkvm-elf/release/libzkvm_platform.a
```

## Build Sample Guests

From the `go-zkvm` repo root:

```bash
export GO_GOROOT=/path/to/go1.24.4
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

## Execute Without Proving

From `go-guest-host/`:

```bash
cargo run --release -- ../simple.bin --raw-journal --execute-only
```

## Prove And Verify

From `go-guest-host/`:

```bash
cargo run --release -- ../simple.bin --raw-journal
```

The host will:

- compute the image ID
- run the local prover
- verify the receipt
- print the committed journal bytes
- print the receipt proof seal size

For the smallest public-output sample, prove `multiply` with:

```bash
cargo run --release -- ../multiply.bin
```

## Richer Example

`policy_check` is the recommended medium-complexity sample in this repo.

Execute it with the built-in private witness:

```bash
cargo run --release -- ../policy_check.bin --raw-journal --execute-only
```

Prove it:

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

These values come from the current sibling-layout verification pass. Treat the
image IDs as artifact-specific rather than universal constants.

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
- The image ID is tied to the exact built guest artifact. In the current lane,
  moving only the Go guest repo checkout path did not change the image ID when
  the same sibling `risc0`, `tinygo-zkvm`, and `go-zkvm` trees were reused.
- The remaining drift is specifically in the linked `libzkvm_platform.a` build:
  rebuilding `risc0/examples/c-guest` from a different checkout path still
  changes the image ID even when the committed journal output stays the same.
- If you are debugging basic guest behavior, use `--execute-only` first.
- The packed kernel half must come from the same current risc0 lane as the
  archive and host crates.
