# TinyGo zkVM Target Notes

This document explains the minimum set of TinyGo-side changes required to make
Go guests work on risc0.

The full fork-local detail belongs in the sibling `tinygo-zkvm` repo. This file
is the consumer-oriented summary for `go-zkvm`.

## Why A TinyGo Fork Is Needed

Plain upstream TinyGo does not know how to produce a guest that matches risc0’s
execution environment. A working guest needed changes in four areas:

1. target definition
2. runtime/startup behavior
3. linker layout
4. archive-linking compatibility

## Two TinyGo Targets

The fork carries two relevant targets:

- `zkvm`
  - legacy handwritten-syscall target
- `zkvm-platform`
  - current recommended target that links upstream `libzkvm_platform.a`

New work should use `zkvm-platform`.

## Required TinyGo-Side Changes

### Target Specs

The fork adds zkVM target specs with:

- `llvm-target = riscv32-unknown-none`
- `-march=rv32im`
- `-mabi=ilp32`
- the risc0 guest linker script
- the build tag `tinygo.zkvm`

### Runtime Selection

The fork adds a dedicated zkVM runtime path so TinyGo selects the right runtime
behavior without pretending the environment is normal bare metal.

Important follow-on fixes included:

- scheduler/task-stack selection for zkVM builds
- disabling the wrong baremetal/interrupt runtime variants for this target
- correct cycle-count typing for current upstream risc0

### Startup / Exit

The startup path needs to:

- initialize the correct stack/global pointers
- transfer control into TinyGo runtime `main`
- halt correctly if guest `main` returns

That is slightly different from normal embedded-target assumptions.

### Linker Layout

The linker script is critical. The current working layout aligns with the risc0
guest memory map and places:

- the entry word where `v1compat` expects it
- `.text`, `.data`, and `.bss` in guest RAM
- heap start after the loaded image
- stack top at the expected guest address

This was one of the key updates needed when moving from the older lane to the
current upstream risc0 lane.

### Archive Linking

The target also has to allow linking the upstream platform archive cleanly. In
practice that meant:

- a separate `zkvm-platform` target without the handwritten syscall assembly
- reliable propagation of `-extldflags=/abs/path/to/libzkvm_platform.a`
- a TinyGo build environment that works with the available LLVM toolchain

## Why This Matters For `go-zkvm` Users

From a consumer standpoint, the TinyGo fork is what turns Go source into a
guest ELF that the rest of the pipeline can package and prove.

Without these target/runtime/linker changes, `go-zkvm` would just be a wrapper
package with nowhere valid to run.
