package main

import "github.com/roasbeef/go-zkvm/zkvm"

func main() {
	// Simple test - just print and exit
	zkvm.Print("Hello from Go zkVM!\n")
	zkvm.Halt(0)
}
