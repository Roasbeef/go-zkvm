// Package main is the simplest possible Go zkVM guest. It demonstrates the
// minimal guest lifecycle: print a private debug message and halt. No private
// input is read and nothing is committed to the public journal.
package main

import "github.com/roasbeef/go-zkvm/zkvm"

func main() {
	zkvm.Print("Hello from Go zkVM!\n")
	zkvm.Halt(0)
}
