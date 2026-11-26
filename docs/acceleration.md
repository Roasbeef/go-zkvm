# Accelerated SHA-256 and secp256k1

Guest code pays for every RISC-V instruction it runs. A software SHA-256
block costs a few thousand cycles, and a software secp256k1 scalar
multiplication costs millions. RISC Zero provides circuits for both, and the
`accel` packages expose them to TinyGo guests with APIs that also build and
test natively.

| Package | Inside a guest | Outside a guest |
| --- | --- | --- |
| `accel/sha256` | `sys_sha_buffer` (SHA ecall) | `crypto/sha256` |
| `accel/bigint2` | bigint2 programs (ecall 6) | `math/big` |
| `accel/secp256k1` | built on `accel/bigint2` | built on `accel/bigint2` |

The backend is chosen by the `tinygo.zkvm` build tag, so the same code runs
in both places and native tests cover the algorithms the guest runs.

## `accel/sha256`

`New`, `Sum256`, `Size` and `BlockSize` mirror `crypto/sha256`. Swapping the
import is enough. Input is staged through a word-aligned block buffer, and
`Sum256` reuses one package-level digest, so hashing a stack buffer does not
allocate.

## Heap escapes

TinyGo guests use a leaking allocator: nothing is freed, and every heap
allocation is zeroed. A pointer passed through cgo, returned from a helper,
or converted to an integer makes TinyGo move its object to the heap, so a
hash or curve operation in a loop can spend more cycles allocating than
computing. The accel packages declare the platform syscalls with
`//go:linkname` and `//go:noescape` instead of cgo, never convert caller
pointers to integers, and avoid helpers that return pointers into their
arguments. `tinygo build -print-allocs=<package regexp>` lists the
allocations that remain.

## `accel/bigint2`

Bindings for four of RISC Zero's bigint2 programs, generated into Go word
arrays by `gen_blobs.go` so they are always word aligned:

- `ModMul256`, `ModInv256`
- `ECAdd256`, `ECDouble256` for affine short Weierstrass points

A bigint2 program gets its result from the host and checks a congruence.
The checks do not force a fully reduced result, so every wrapper checks that
the result is below the modulus and panics otherwise. The programs also
cannot handle special cases: callers must not invert zero, add points with
equal x coordinates, or double a point with y = 0. Every input point must be
on the curve.

To refresh the blobs from a RISC Zero checkout:

```sh
cd accel/bigint2 && go run gen_blobs.go ../../../risc0 > blobs.go
```

## `accel/secp256k1`

- `VerifySchnorr(pubKey, msg, sig)`: BIP-340 verification.
- `TweakAddXOnly(internal, tweak)`: lift_x(internal) + tweak*G, the BIP-341
  output key, with its y parity.
- `IsValidXOnly(pubKey)`

The fixed-base multiply uses a precomputed table of 2^i G, so it costs one
point addition per set scalar bit. The variable-base multiply uses a 4-bit
window. Square roots use libsecp256k1's addition chain. The package handles
public data only; nothing here is constant time.

Tests run the official BIP-340 vectors and compare tweaks, square roots and
scalar multiplication against a `math/big` reference.

## Self-test and costs

`make accel-selftest` builds `examples/accel_selftest`, which runs the same
vectors inside the guest and prints cycle counts. On RISC Zero v3.1.0:

| Operation | Cycles |
| --- | ---: |
| SHA-256 of 64 bytes | about 1,800 |
| SHA-256 of 1,000 bytes | about 5,500 |
| BIP-340 verification | about 230,000 |
| x-only tweak | about 115,000 |
| lift_x failure (non-square) | about 40,000 |

For comparison, the software paths in a TinyGo guest cost about 3,500
cycles per SHA-256 block and several million cycles per signature check.
