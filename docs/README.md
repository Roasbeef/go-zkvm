# go-zkvm Docs

This folder explains the working non-Rust guest stack, not just the final
commands. The main distinction to keep in mind while reading is:

- the old prototype path used handwritten syscall assembly
- the current recommended path links upstream `libzkvm_platform.a`

Most of the repo is now organized around the second model.

For consumer-facing usage, prefer the repo `README.md`, `running.md`, and the
Go `host/` package. `go-guest-host/` is still documented here, but as the
reference Rust CLI rather than the primary host integration surface.

## Recommended Reading Order

1. `../README.md`
   - repo purpose, status, and top-level layout
2. `tutorial.md`
   - guided walkthrough from first guest to real application (start here if you
     want to build something)
3. `go-zkvm-overview.md`
   - end-to-end architecture and the recommended flow
4. `host-api.md`
   - full Go host API reference: client lifecycle, operations, options, errors,
     worked examples
5. `tinygo-zkvm-target.md`
   - what had to change in TinyGo to make this work
6. `implementation-guide.md`
   - guest-side Go package, host harness, and packer details
7. `syscall-architecture.md`
   - where syscalls come from in the legacy and archive-linked models
8. `journal-digest.md`
    - why journal output needs a final output digest and how the Go guest does
      it
9. `recursion-composition.md`
    - standalone walkthrough of `zkvm.Verify(...)`, assumptions digests, and
      how the host-side supplied receipts are resolved by recursion
10. `running.md`
    - practical build, execute, prove, and FFI validation commands, plus the
      current validated sample outputs
11. `code-format.md`
    - the local Go formatting and commenting conventions used for host-side code

## Topic Map

- Tutorial:
  - `tutorial.md`
- Architecture:
  - `go-zkvm-overview.md`
  - `tinygo-zkvm-target.md`
  - `syscall-architecture.md`
- Host API:
  - `host-api.md`
- Guest and host implementation:
  - `implementation-guide.md`
  - `ecall-reference.md`
- Packaging and receipt-visible output:
  - `journal-digest.md`
  - `recursion-composition.md`
- Runbooks:
  - `running.md`
  - `code-format.md`
