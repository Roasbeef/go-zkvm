package main

import "github.com/roasbeef/go-zkvm/zkvm"

func main() {
	// Read input values from host
	var a, b uint64
	zkvm.ReadValue(&a)
	zkvm.ReadValue(&b)

	// Verify neither is 1 (nontrivial factors) and neither is 0
	if a == 0 || b == 0 {
		zkvm.Debug("Error: Zero factor\n")
		zkvm.Halt(1)
	}
	if a == 1 || b == 1 {
		zkvm.Debug("Error: Trivial factors\n")
		zkvm.Halt(1)
	}

	// Compute the product
	product := a * b

	// Check for overflow
	if product/a != b {
		zkvm.Debug("Error: Integer overflow\n")
		zkvm.Halt(1)
	}

	// Commit the product to the journal (public output)
	zkvm.CommitValue(&product)

	// Debug output (private)
	zkvm.Print("Successfully computed product\n")

	// Exit successfully
	zkvm.Halt(0)
}
