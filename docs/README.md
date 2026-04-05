# go-zkvm Docs

This folder explains the working non-Rust guest stack, not just the final
commands. The main distinction to keep in mind while reading is:

- the old prototype path used handwritten syscall assembly
- the current recommended path links upstream `libzkvm_platform.a`

Most of the repo is now organized around the second model.

## Recommended Reading Order

1. `../README.md`
   - repo purpose, status, and top-level layout
2. `go-zkvm-overview.md`
   - end-to-end architecture and the recommended flow
3. `go-facing-host-boundary.md`
   - what it would take to expose proving to Go callers without pretending the
     prover itself is pure Go
4. `go-ffi-api-plan.md`
   - the concrete Option 2 FFI API and ABI plan before implementation
5. `tinygo-zkvm-target.md`
   - what had to change in TinyGo to make this work
6. `implementation-guide.md`
   - guest-side Go package, host harness, and packer details
7. `syscall-architecture.md`
   - where syscalls come from in the legacy and archive-linked models
8. `journal-digest.md`
   - why journal output needs a final output digest and how the Go guest does it
9. `running.md`
   - practical build, execute, and prove commands, plus the current validated
     sample outputs

## Topic Map

- Architecture:
  - `go-zkvm-overview.md`
  - `go-facing-host-boundary.md`
  - `go-ffi-api-plan.md`
  - `tinygo-zkvm-target.md`
  - `syscall-architecture.md`
- Guest and host implementation:
  - `implementation-guide.md`
  - `ecall-reference.md`
- Packaging and receipt-visible output:
  - `journal-digest.md`
  - `static-library-approach.md`
- Runbooks:
  - `running.md`
