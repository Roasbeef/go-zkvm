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
make platform-smoke
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

## Notes

- On Apple Silicon, local release proving should take the Metal-backed path by
  default.
- If you are debugging basic guest behavior, use `--execute-only` first.
- The packed kernel half must come from the same current risc0 lane as the
  archive and host crates.
