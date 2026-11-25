// The integration test supplies the image ID and receipt for the simple
// guest, whose journal is empty. The parent commits the child image ID.
package main

import "github.com/roasbeef/go-zkvm/zkvm"

func main() {
	var imageID [32]byte
	zkvm.Read(imageID[:])
	zkvm.Verify(imageID, nil)
	zkvm.Commit(imageID[:])
	zkvm.Halt(0)
}
