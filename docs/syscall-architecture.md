# RISC Zero zkVM Syscall Architecture

## Overview

The risc0 zkVM exposes services through RISC-V `ecall` instructions. For Go
guests, the important question is not just “what ecalls exist?” but “which
layer is responsible for issuing them?”

There are now two answers in this project:

- legacy handwritten syscall assembly
- current archive-linked syscall veneers from `libzkvm_platform.a`

## Hardware Ecalls

The zkVM recognizes specific operations based on `t0`:

| `t0` | Operation | Purpose |
|------|-----------|---------|
| `0` | `HALT` | terminate execution |
| `2` | `SOFTWARE` | dispatch a named syscall |
| `3` | `SHA` | SHA-256 compression |
| `4` | `KECCAK` | Keccak support |

Those are the machine-level primitives. Everything else builds on top of them.

## SOFTWARE Ecall Dispatch

I/O-oriented operations such as read and write are usually routed through the
named SOFTWARE path.

At the register level, that looks like:

```assembly
la a2, syscall_name
li a3, 1
mv a4, buffer_ptr
mv a5, length
li t0, 2
ecall
```

where `syscall_name` is a string such as:

```text
risc0_zkvm_platform::syscall::nr::SYS_WRITE
```

## Legacy Model: Handwritten Syscall Veneers

The first working TinyGo integration used its own assembly for functions such
as:

- `sys_read`
- `sys_write`
- `sys_halt`
- `sys_commit`
- `sys_sha_buffer`

That code lived in the TinyGo fork and directly encoded the ecall ABI expected
by risc0.

This path proved the concept, but it also meant we owned every ABI detail.

## Current Model: Archive-Linked Syscall Veneers

The newer path links against upstream `libzkvm_platform.a`.

That changes the picture to:

```text
Go guest code
    ↓
Go wrapper package (`zkvm/`)
    ↓
C ABI symbols from libzkvm_platform.a
    ↓
hardware / SOFTWARE ecalls
```

This is a better maintenance model because the low-level syscall veneer comes
from risc0 itself rather than a handwritten fork.

## Important Boundary: What Still Lives On The Go Side

Even on the archive-linked path, not everything is “inside the archive”.

The Go side still owns:

- the typed guest API
- witness decoding helpers
- journal hashing/finalization logic
- any higher-level proof-claim conventions used by a guest

So the archive solves the syscall veneer problem, but not the entire non-Rust
guest problem.

## Why The `v1compat` Kernel Matters

The packaged kernel half:

- provides the guest execution environment
- handles SOFTWARE dispatch internally
- must match the current host/prover lane

But the kernel is not a substitute for the guest-side syscall veneer. That is
why the archive link step matters.

## Version-Skew Hazard

One of the concrete lessons from this work is that syscall ABI details can
change over time. A real example from the latest lane:

- current `sys_cycle_count` returns `u64`
- the older Go/TinyGo assumptions still treated it as `u32`

That kind of drift is exactly why the archive-linked model is preferable.
