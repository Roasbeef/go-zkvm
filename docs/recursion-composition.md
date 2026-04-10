# Recursion And Composition Walkthrough

## Why This Exists

The guest-side API makes composition look simple:

- guest code calls `zkvm.Verify(...)`
- host code supplies `Assumptions`

But there are several moving parts underneath that need to agree exactly:

- the child claim digest the guest reconstructs
- the assumptions digest folded into the guest's final `risc0.Output`
- the concrete succinct receipts the host provides
- the recursion pipeline that resolves those receipts against the guest's
  assumptions list

This note explains that end-to-end flow.

## The Main Idea

RISC Zero composition does not work by running a full child-proof verifier
inside the guest.

Instead, the guest registers assumptions of the form:

- “there exists a valid receipt for this exact child claim”

Then the host supplies the actual receipts, and the recursion pipeline resolves
them while producing the final composed proof.

So there are always two representations of the same dependency set:

- guest side:
  - a digest-only assumptions list
- host side:
  - concrete succinct child receipts

If those two views do not match exactly, proving fails.

## Guest-Side Flow

The guest-side helper lives in:

- `zkvm/verify.go`

When guest code calls:

```go
zkvm.Verify(imageID, journal)
```

the helper reconstructs the same child claim digest the Rust guest SDK would
build for an unconditional receipt:

1. hash the child journal into a `journalDigest`
2. construct a zero post-state for the unconditional child receipt
3. construct `risc0.Output(journalDigest, zeroDigest)`
4. construct `risc0.ReceiptClaim`
5. pass the resulting digest to `sys_verify_integrity`
6. fold that same assumption into the running assumptions digest

So `zkvm.Verify(...)` means:

- “this guest depends on an exact child claim”

It does not mean:

- “this guest ran the entire child verifier internally”

## `sys_verify_integrity`

The syscall itself is only the registration point.

The guest passes:

- the child claim digest
- the child control root

For the batch lane today we use unconditional child receipts, so the control
root is zero.

The important point is that `sys_verify_integrity` is not where recursive
compression happens. It just records the dependency so the rest of the proving
stack can resolve it later.

## Assumptions Digest

The Go guest keeps a running assumptions digest in:

- `zkvm/sha256_proper.go`

Each `zkvm.Verify(...)` call builds one:

- `risc0.Assumption(claim_digest, control_root)`

Then prepends it to the running list:

- `risc0.Assumptions(head, tail)`

That digest becomes the `assumptions_digest` half of the final:

```text
risc0.Output(journal_digest, assumptions_digest)
```

This is why the guest must update the assumptions digest every time it
registers a child dependency. If it fails to do that, the final receipt claim
will not match what the host is trying to resolve.

## Host-Side Flow

The host-side composition boundary is:

- `host/` in Go
- `host-core/` in Rust

The caller passes child receipts as:

- `Assumptions []AssumptionReceipt`

Those receipts are decoded and attached to the executor/prover environment via
`builder.add_assumption(...)`.

So on the host side, composition is concrete:

- actual serialized succinct receipts are made available to the proving stack

This is the counterpart to the guest-side digest-only assumptions list.

## Why Assumptions Must Be Succinct

The current host path expects composed child receipts to already be in a form
the recursion layer can resolve efficiently.

In practice for this repo, that means:

- leaf receipts supplied as assumptions are succinct receipts

That is why the batch lane first proves leaf receipts, then passes those
succinct receipts upward into the aggregation guest.

## Resolution In The Recursion Pipeline

During prove-time recursion, risc0 opens the conditional receipt's assumptions
list and matches it against the host-supplied receipts.

At a high level the recursion code:

1. opens the assumptions list
2. takes the head assumption
3. checks that the supplied child receipt's claim digest matches
4. checks the control root
5. removes the resolved head
6. continues with the tail

If the guest-side assumptions digest and the host-side supplied receipts do not
describe the same claims in the same order, the proof cannot be resolved.

That is the source of the familiar “assumptions mismatch” style failure when a
composed guest is wired incorrectly.

## What The Final Verifier Sees

Once assumptions are resolved, the final composed receipt is self-contained for
the external verifier.

The verifier normally checks:

- the final receipt
- the expected image ID
- optionally the expected public journal

The verifier does not need the child receipts anymore after successful
composition. Those receipts were prover-side inputs used to resolve the final
claim, not part of the final verifier artifact.

## Practical Debugging Rules

When composition fails, inspect these layers in order:

1. guest claim reconstruction
   - is `zkvm.Verify(...)` reconstructing the exact child claim digest?
2. assumptions digest updates
   - is every guest-side verification folded into the running digest?
3. host assumptions list
   - are the same receipts supplied in the same order?
4. child receipt kind
   - are you supplying succinct receipts, not ordinary composite ones?
5. final receipt kind
   - if you want automatic recursive resolution, use a proving mode that
     resolves assumptions

## Where To Read Next

- `host-api.md`
  - how the Go host surface exposes `Assumptions`
- `journal-digest.md`
  - how the final output digest combines journal and assumptions
- `tutorial.md`
  - broader end-to-end walkthrough
