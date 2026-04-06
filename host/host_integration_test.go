//go:build cgo

package host

import (
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

func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(file), ".."))
}
