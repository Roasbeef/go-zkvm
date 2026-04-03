package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// R0BF header structure based on observed format in multiply.bin
type R0BFHeader struct {
	Magic    [4]byte // "R0BF"
	Version  uint32  // 1
	Field1   uint32  // 16
	Field2   uint32  // 1
	Field3   uint32  // 8
	Padding1 [3]byte // 0x00, 0x00, 0x05
	Version2 [5]byte // "1.0.0"
	ElfSize  uint32  // Size of embedded ELF
}

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintf(os.Stderr, "Usage: %s <user.elf> <kernel.elf> <output.bin>\n", os.Args[0])
		os.Exit(1)
	}

	userPath := os.Args[1]
	kernelPath := os.Args[2]
	outputPath := os.Args[3]

	// Read the user ELF file
	userElfData, err := os.ReadFile(userPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading user ELF file: %v\n", err)
		os.Exit(1)
	}

	// Verify it's an ELF file
	if len(userElfData) < 4 || string(userElfData[:4]) != "\x7fELF" {
		fmt.Fprintf(os.Stderr, "Error: User file is not an ELF file\n")
		os.Exit(1)
	}

	// Read the kernel ELF file
	kernelElfData, err := os.ReadFile(kernelPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading kernel ELF file: %v\n", err)
		os.Exit(1)
	}

	// Verify it's an ELF file
	if len(kernelElfData) < 4 || string(kernelElfData[:4]) != "\x7fELF" {
		fmt.Fprintf(os.Stderr, "Error: Kernel file is not an ELF file\n")
		os.Exit(1)
	}

	// Create output file
	outFile, err := os.Create(outputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output file: %v\n", err)
		os.Exit(1)
	}
	defer outFile.Close()

	// Write R0BF header
	var buf bytes.Buffer

	// Magic "R0BF"
	buf.WriteString("R0BF")

	// Binary format version = 1
	binary.Write(&buf, binary.LittleEndian, uint32(1))

	// Header length = 16 bytes
	binary.Write(&buf, binary.LittleEndian, uint32(16))

	// Header content (16 bytes total):
	// - Number of KV pairs (4 bytes) = 1
	binary.Write(&buf, binary.LittleEndian, uint32(1))

	// - Length of KV pair (4 bytes) = 8
	binary.Write(&buf, binary.LittleEndian, uint32(8))

	// - KV pair data (8 bytes) - this appears to be ABI version info
	// Based on the multiply.bin: 00 00 05 31 2e 30 2e 30
	buf.Write([]byte{0x00, 0x00, 0x05, 0x31, 0x2e, 0x30, 0x2e, 0x30})

	// Write user ELF size
	binary.Write(&buf, binary.LittleEndian, uint32(len(userElfData)))

	// Write header to file
	if _, err := io.Copy(outFile, &buf); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing header: %v\n", err)
		os.Exit(1)
	}

	// Write user ELF data
	if _, err := outFile.Write(userElfData); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing user ELF data: %v\n", err)
		os.Exit(1)
	}

	// Write kernel ELF data
	if _, err := outFile.Write(kernelElfData); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing kernel ELF data: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully converted to %s\n", outputPath)
	fmt.Printf("R0BF file size: %d bytes\n", 4+4+4+16+4+len(userElfData)+len(kernelElfData))
	fmt.Printf("  User ELF: %d bytes\n", len(userElfData))
	fmt.Printf("  Kernel ELF: %d bytes\n", len(kernelElfData))
}
