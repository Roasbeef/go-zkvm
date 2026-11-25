// This file validates the full FFI round trip: load the Rust shared library,
// compute an image ID, execute a guest, generate a proof, and verify the
// receipt. It requires both the host-ffi shared library and the simple guest
// binary to be pre-built.

//go:build cgo

package host

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestHostFFISimpleGuest(t *testing.T) {
	repoRoot := repoRoot(t)
	guestPath := filepath.Join(repoRoot, "simple.bin")
	libPath := defaultLibraryPath()

	if _, err := os.Stat(libPath); err != nil {
		t.Skipf("host ffi library not built at %s: %v", libPath, err)
	}
	if _, err := os.Stat(guestPath); err != nil {
		t.Skipf(
			"simple guest binary not built at %s: %v",
			guestPath, err,
		)
	}

	guestBinary, err := ReadGuestFile(guestPath)
	if err != nil {
		t.Fatalf("read guest binary: %v", err)
	}

	client, err := New(WithLibraryPath(libPath))
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()

	imageID, err := client.ComputeImageID(guestBinary)
	if err != nil {
		t.Fatalf("compute image id: %v", err)
	}
	if imageID == "" {
		t.Fatal("compute image id returned an empty string")
	}

	execResult, err := client.Execute(
		ExecuteRequest{GuestBinary: guestBinary},
	)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if execResult.ImageID != imageID {
		t.Fatalf(
			"execute image id mismatch: got %s want %s",
			execResult.ImageID, imageID,
		)
	}
	if len(execResult.Journal) != 0 {
		t.Fatalf(
			"simple guest journal should be empty, got %d bytes",
			len(execResult.Journal),
		)
	}

	proveResult, err := client.Prove(ProveRequest{GuestBinary: guestBinary})
	if err != nil {
		t.Fatalf("prove: %v", err)
	}
	if proveResult.ImageID != imageID {
		t.Fatalf(
			"prove image id mismatch: got %s want %s",
			proveResult.ImageID, imageID,
		)
	}
	if len(proveResult.Journal) != 0 {
		t.Fatalf(
			"simple guest proof journal should be "+
				"empty, got %d bytes",
			len(proveResult.Journal),
		)
	}
	if len(proveResult.Receipt) == 0 {
		t.Fatal("prove returned an empty receipt")
	}

	verifyResult, err := client.Verify(VerifyRequest{
		Receipt:         proveResult.Receipt,
		ImageID:         imageID,
		ExpectedJournal: []byte{},
	})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !verifyResult.Verified {
		t.Fatal("verify returned false")
	}
	if len(verifyResult.Journal) != 0 {
		t.Fatalf(
			"verify journal should be empty, got %d bytes",
			len(verifyResult.Journal),
		)
	}
	if verifyResult.ReceiptEncoding != "borsh" {
		t.Fatalf(
			"unexpected verify receipt encoding: %s",
			verifyResult.ReceiptEncoding,
		)
	}
	if verifyResult.SealBytes == 0 {
		t.Fatal("verify returned zero seal bytes")
	}
}

func TestHostFFIComposition(t *testing.T) {
	root := repoRoot(t)
	leafPath := filepath.Join(root, "simple.bin")
	parentPath := filepath.Join(root, "verify_simple.bin")
	for _, path := range []string{defaultLibraryPath(), leafPath, parentPath} {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Skipf("missing %s; build with make test-host-ffi", path)
		} else if err != nil {
			t.Fatal(err)
		}
	}

	client, err := New(WithLibraryPath(defaultLibraryPath()))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	leafBinary, err := ReadGuestFile(leafPath)
	if err != nil {
		t.Fatal(err)
	}
	parentBinary, err := ReadGuestFile(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	parentID, err := client.ComputeImageID(parentBinary)
	if err != nil {
		t.Fatal(err)
	}

	leaf, err := client.Prove(
		ProveRequest{GuestBinary: leafBinary},
		WithReceiptKind(ReceiptKindSuccinct),
	)
	if err != nil {
		t.Fatalf("prove leaf: %v", err)
	}
	if leaf.ReceiptKind != ReceiptKindSuccinct {
		t.Fatalf("expected succinct leaf receipt, got %s", leaf.ReceiptKind)
	}
	imageID, err := hex.DecodeString(leaf.ImageID)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := client.Prove(ProveRequest{
		GuestBinary: parentBinary,
		Stdin:       imageID,
		Assumptions: []AssumptionReceipt{leaf.Receipt},
	})
	if err != nil {
		t.Fatalf("prove parent: %v", err)
	}
	if parent.ReceiptKind != ReceiptKindComposite {
		t.Fatalf("expected composite parent receipt, got %s",
			parent.ReceiptKind)
	}
	verified, err := client.Verify(VerifyRequest{
		Receipt:         parent.Receipt,
		ImageID:         parentID,
		ExpectedJournal: imageID,
	})
	if err != nil {
		t.Fatalf("verify parent: %v", err)
	}
	if !verified.Verified || !bytes.Equal(verified.Journal, imageID) {
		t.Fatalf("unexpected parent result: verified=%v journal=%x",
			verified.Verified, verified.Journal)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(file), ".."))
}
