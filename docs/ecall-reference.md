# RISC Zero zkVM Ecall Quick Reference

This is the machine-level reference that sits under both the legacy handwritten
path and the current archive-linked path.

## Hardware Ecalls

Execute `ecall` with `t0` selecting the operation:

| `t0` | Operation | Registers | Description |
|------|-----------|-----------|-------------|
| `0` | `HALT` | `a0=exit_code`, `a1=output_digest_ptr` | stop execution |
| `2` | `SOFTWARE` | `a0-a1=from_host`, `a2=name`, `a3-a7=params` | named syscall dispatch |
| `3` | `SHA` | `a0=out_state`, `a1=in_state`, `a2-a4=blocks/count` | SHA-256 compression |
| `4` | `KECCAK` | varies | Keccak support |

## SOFTWARE Ecalls

These are named calls routed through the platform layer.

### `SYS_READ`

```assembly
mv a0, buffer
mv a1, word_count
la a2, sys_read_name
mv a3, 0
mv a4, nbytes
li t0, 2
ecall
```

### `SYS_WRITE`

```assembly
li a0, 0
li a1, 0
la a2, sys_write_name
mv a3, fd
mv a4, buffer
mv a5, length
li t0, 2
ecall
```

### `SYS_CYCLE_COUNT`

```assembly
li a0, 0
li a1, 0
la a2, sys_cycle_count_name
li t0, 2
ecall
```

Current upstream returns a `u64` cycle count. That detail matters for
archive-linked correctness.

## Common Name Strings

```assembly
risc0_zkvm_platform::syscall::nr::SYS_READ
risc0_zkvm_platform::syscall::nr::SYS_WRITE
risc0_zkvm_platform::syscall::nr::SYS_CYCLE_COUNT
```

## File Descriptors

- `0`: stdin, private witness input from the host
- `1`: stdout, private guest output
- `2`: stderr, debug output
- `3`: journal, public output that must be reflected in the final output digest
