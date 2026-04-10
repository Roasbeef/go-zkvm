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

## What `zkvm.Verify(...)` Actually Means

The guest-side composition helper does not run a full child-proof verifier
inside the guest. Instead, it registers a dependency on an exact child claim.

The implementation in `zkvm/verify.go` reconstructs the same digest chain used
by the Rust guest SDK:

1. hash the child journal into a `journalDigest`
2. construct the zero post-state for an unconditional receipt
3. construct `risc0.Output(journalDigest, zeroDigest)`
4. construct the child `risc0.ReceiptClaim`
5. pass that claim digest to `sys_verify_integrity`
6. fold the same assumption into the running `assumptions_digest`

So `zkvm.Verify(imageID, journal)` means:

- “this execution depends on there existing a valid receipt for this exact
  `(imageID, journal)` claim”

It does not mean:

- “the guest directly ran a full verifier for that child proof”

That distinction is what allows risc0's recursion pipeline to resolve
assumptions efficiently later instead of replaying a full verifier inside the
guest program.

## Guest-Side Assumptions Digest

`addAssumptionDigest` in `zkvm/sha256_proper.go` maintains a hashed linked list
of assumptions. Each node is:

- `risc0.Assumption(claim_digest, control_root)`

and the whole list is folded as:

- `risc0.Assumptions(head, tail)`

The final digest of that list becomes the `assumptions_digest` half of:

```text
risc0.Output(journal_digest, assumptions_digest)
```

So the guest-side receipt claim commits not just to the public journal, but
also to the exact ordered set of assumptions the guest registered while it ran.

## Host-Side Assumptions

The host keeps the concrete child receipts separately.

In the Go host path:

- callers pass serialized succinct receipts in `Assumptions`
- `host-core/src/lib.rs` decodes them and calls `builder.add_assumption(...)`

That means there are two parallel representations of the same dependency set:

- guest side:
  - a digest-only assumptions list embedded into the final output claim
- host side:
  - the actual succinct child receipts supplied to the executor/prover

## How Resolution Works

During proof generation, risc0's recursion pipeline reconciles those two views.

At a high level it:

1. opens the conditional receipt's assumptions list
2. takes the head assumption
3. checks that the supplied child receipt's claim digest matches that head
4. checks the `control_root`
5. removes the resolved head and continues with the tail

This is why composition failures often show up as an assumptions mismatch:
the guest-side digest chain and the host-side supplied receipts no longer
describe the same set of child claims.

The important practical consequence is:

- the final composed receipt no longer needs to ship the child receipts
- once the assumptions are resolved, the verifier usually only checks the
  final receipt and whatever public inclusion artifacts the application uses

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
