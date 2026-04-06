// Package main demonstrates a structured witness with validation, derived
// computation, and a multi-field public summary. The host provides a list of
// private item values, a discount, and a spending limit. The guest validates
// all inputs, computes the totals, and commits a public summary that says
// whether the purchase was approved -- without revealing individual item
// values.
package main

import "github.com/roasbeef/go-zkvm/zkvm"

// maxPolicyItems caps the number of line items to prevent unbounded iteration
// inside the guest.
const maxPolicyItems = 16

func main() {
	var itemCount uint32
	zkvm.ReadValue(&itemCount)

	if itemCount == 0 || itemCount > maxPolicyItems {
		zkvm.Debug("Error: invalid item count\n")
		zkvm.Halt(1)
	}

	var subtotal uint64
	for i := uint32(0); i < itemCount; i++ {
		var item uint64
		zkvm.ReadValue(&item)
		if item == 0 {
			zkvm.Debug("Error: item value must be non-zero\n")
			zkvm.Halt(1)
		}

		nextSubtotal := subtotal + item
		if nextSubtotal < subtotal {
			zkvm.Debug("Error: subtotal overflow\n")
			zkvm.Halt(1)
		}
		subtotal = nextSubtotal
	}

	var discount uint64
	var limit uint64
	zkvm.ReadValue(&discount)
	zkvm.ReadValue(&limit)

	if discount > subtotal {
		zkvm.Debug("Error: discount exceeds subtotal\n")
		zkvm.Halt(1)
	}

	total := subtotal - discount
	var approved uint32
	if total <= limit {
		approved = 1
	}

	zkvm.CommitValue(&itemCount)
	zkvm.CommitValue(&approved)
	zkvm.CommitValue(&subtotal)
	zkvm.CommitValue(&discount)
	zkvm.CommitValue(&total)
	zkvm.CommitValue(&limit)
	zkvm.Halt(0)
}
