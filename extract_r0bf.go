package main

import (
	"encoding/binary"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <input.bin>\n", os.Args[0])
		os.Exit(1)
	}

	inputPath := os.Args[1]

	// Read the R0BF file
	data, err := os.ReadFile(inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}

	// Check magic
	if string(data[:4]) != "R0BF" {
		fmt.Fprintf(os.Stderr, "Not an R0BF file\n")
		os.Exit(1)
	}

	// Read format version
	version := binary.LittleEndian.Uint32(data[4:8])
	fmt.Printf("Format version: %d\n", version)

	// Read header length
	headerLen := binary.LittleEndian.Uint32(data[8:12])
	fmt.Printf("Header length: %d bytes\n", headerLen)

	// Skip header
	offset := 12 + int(headerLen)

	// Read user ELF length
	userLen := binary.LittleEndian.Uint32(data[offset : offset+4])
	fmt.Printf("User ELF length: %d bytes\n", userLen)
	offset += 4

	// Extract user ELF
	userElf := data[offset : offset+int(userLen)]
	offset += int(userLen)

	// Rest is kernel ELF
	kernelElf := data[offset:]
	fmt.Printf("Kernel ELF length: %d bytes\n", len(kernelElf))

	// Save the ELFs
	if err := os.WriteFile("extracted_user.elf", userElf, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing user ELF: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile("extracted_kernel.elf", kernelElf, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing kernel ELF: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Successfully extracted user and kernel ELFs")
}
