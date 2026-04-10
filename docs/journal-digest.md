# Journal Digest Mechanism

## Why This Matters

In risc0, journal visibility is tied to the final output digest passed to
`sys_halt`. A guest that writes bytes to the journal but halts with the wrong
digest will not produce the expected public output at the host layer.

This was one of the first important non-obvious issues in the Go integration.

## Current Working Model

The current Go guest path does work. The state of this repo is now:

- journal bytes are written from Go guest code
- committed bytes are hashed as the guest runs
- the final journal digest is wrapped into the expected `risc0.Output` digest
- that digest is passed to `sys_halt`

That is why the host can now recover the committed journal bytes correctly from
the receipt.

## What The Guest Actually Needs To Produce

The guest does not halt with the raw journal SHA-256 digest alone. It halts
with the tagged output digest that risc0 expects for:

```text
risc0.Output(journal_digest, assumptions_digest)
```

In the current Go flow:

- `journal_digest` reflects the committed bytes
- `assumptions_digest` is all-zero for ordinary guests that make no recursive
  assumptions
- composed guests update it as they register assumptions through guest-side
  calls such as `zkvm.Verify(...)`

## Current Implementation Shape

The relevant code lives in:

- `zkvm/zkvm.go`
- `zkvm/sha256_proper.go`
- `zkvm/verify.go`

The flow is:

1. `Commit` or `CommitValue` writes to the journal
2. the same bytes update the running journal hasher
3. `Halt` finalizes the journal hash
4. the final tagged output digest is constructed
5. `sys_halt` receives that digest pointer

For composed guests, there is one extra step:

6. guest-side verification helpers such as `zkvm.Verify(...)` register
   assumptions with the host and fold those assumptions into the running
   `assumptions_digest`

## Why The Platform Archive Does Not Remove This Problem

Linking `libzkvm_platform.a` gives us the upstream syscall-facing layer. It does
not remove the need for the Go guest layer to decide what to commit and how to
finalize the guest-visible output structure.

That is why this logic still lives in `go-zkvm`.

## Practical Debugging Rule

If a guest appears to run correctly but the host shows an empty or unexpected
journal, inspect the output-digest path before assuming the guest computation is
wrong. For composed guests, also check that the assumptions digest is being
updated whenever the guest registers proof assumptions.
