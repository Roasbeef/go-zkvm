// Package main demonstrates private-input, public-output guest computation.
// The host provides two secret factors (a and b) via stdin. The guest
// validates them, computes the product, and commits only the product to the
// public journal. A verifier sees the product but never learns the factors.
package main

import "github.com/roasbeef/go-zkvm/zkvm"

func main() {
	// These factors are private witness data; only the product is public.
	var a, b uint64
	zkvm.ReadValue(&a)
	zkvm.ReadValue(&b)

	// Validate: both factors must be non-trivial (not 0 or 1).
	if a == 0 || b == 0 {
		zkvm.Debug("Error: Zero factor\n")
		zkvm.Halt(1)
	}
	if a == 1 || b == 1 {
		zkvm.Debug("Error: Trivial factors\n")
		zkvm.Halt(1)
	}

	product := a * b

	// Guard against silent overflow.
	if product/a != b {
		zkvm.Debug("Error: Integer overflow\n")
		zkvm.Halt(1)
	}

	// The product is the only public output. The verifier sees this value
	// in the receipt journal but cannot recover a or b.
	zkvm.CommitValue(&product)

	zkvm.Print("Successfully computed product\n")
	zkvm.Halt(0)
}
