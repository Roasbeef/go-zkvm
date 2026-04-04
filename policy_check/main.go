package main

import "github.com/roasbeef/go-zkvm/zkvm"

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
