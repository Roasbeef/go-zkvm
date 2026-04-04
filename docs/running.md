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

## Experimental Low-Level Smoke

`platform_smoke` is intentionally not part of the default sample set.

It remains in the repo as a low-level CGo/platform experiment, but it is not
currently the supported end-to-end path for consumers of `go-zkvm`.

## Notes

- On Apple Silicon, local release proving should take the Metal-backed path by
  default.
- The image ID is tied to the exact built guest artifact. In the current lane,
  absolute build paths from the linked `zkvm-platform` archive are still
  embedded in the guest ELF, so rebuilding the same source tree in a different
  directory can change the image ID even when the journal output stays the same.
- If you are debugging basic guest behavior, use `--execute-only` first.
- The packed kernel half must come from the same current risc0 lane as the
  archive and host crates.
