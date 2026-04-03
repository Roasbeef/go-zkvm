# Platform Archive Approach

## Summary

This is no longer a theoretical direction. It is the current recommended build
path.

The original prototype proved that TinyGo guests could run by supplying our own
handwritten syscall assembly. That worked, but it scaled badly. The better
model is to reuse the upstream risc0 guest-side platform archive:

```text
libzkvm_platform.a
```

That archive already contains the guest-side syscall veneer used by the C guest
example in the risc0 tree, and TinyGo can link against it cleanly enough for
our current Go guests.

## Why This Is Better

### Less Handwritten ABI Surface

Instead of carrying our own implementations of every syscall veneer, we reuse
the ones shipped by risc0.

### Better Version Alignment

When upstream risc0 changes a syscall detail, the archive-linked path has a
much better chance of staying correct than a handwritten forked assembly stack.

### More Realistic Long-Term Maintenance

This path is much closer to something other non-Rust guest languages could
reuse.

## What The Archive Gives Us

The archive provides the low-level syscall-facing objects. In practice, that
means the guest ELF can resolve symbols such as:

- `sys_read`
- `sys_write`
- `sys_halt`
- SHA-related helpers exported by the platform crate

The Go side then wraps those symbols in the `zkvm` package.

## What The Archive Does Not Remove

Linking the archive does not mean the Go integration becomes trivial. We still
need:

- a TinyGo target that matches the risc0 guest environment
- the right linker script and memory map
- startup/runtime behavior that works in the zkVM
- Go-side journal/output digest finalization
- a packer for the final R0BF binary

So the archive removes the worst syscall-maintenance burden, but it does not
remove the need for a purpose-built Go integration.

## Current Practical Build Model

The working build recipe is:

1. build `libzkvm_platform.a` from `risc0/examples/c-guest`
2. compile the Go guest with TinyGo target `zkvm-platform`
3. pass the archive path through TinyGo’s `-ldflags`
4. pack the guest ELF with the matching `v1compat.elf`
5. execute or prove via the Rust host

The important TinyGo quirk from this work is that passing the archive as a
single `-extldflags=/abs/path/to/libzkvm_platform.a` value is the reliable
form in this setup.

## Comparison With The Legacy Manual Path

| Aspect | Legacy `zkvm` target | Current `zkvm-platform` target |
|--------|-----------------------|--------------------------------|
| Syscall veneers | handwritten assembly | upstream archive |
| Upstream drift risk | high | lower |
| New syscall coverage | manual | mostly inherited from risc0 |
| Guest runtime work | still required | still required |
| Recommended for new work | no | yes |

## Why Keep The Legacy Path Around At All

The legacy handwritten path is still useful as:

- historical documentation of the guest ABI
- a fallback when investigating archive-linking problems
- a way to understand what the platform archive is abstracting away

But it is no longer the center of the design.
