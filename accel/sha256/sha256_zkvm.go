//go:build tinygo.zkvm

package sha256

import (
	"encoding/binary"
	"hash"
	"unsafe"
)

// sysShaBuffer is the platform's sys_sha_buffer. It is declared without cgo
// so that //go:noescape applies: a cgo call makes every pointer argument
// escape, and in a guest whose allocator never frees, that turns each
// stack buffer handed to the accelerator into a fresh heap allocation.
//
//go:linkname sysShaBuffer sys_sha_buffer
//go:noescape
func sysShaBuffer(outState, inState *uint32, buf unsafe.Pointer, count uint32)

// maxBlocksPerCall bounds a single accelerator call. The platform caps a
// buffer call at 1000 blocks.
const maxBlocksPerCall = 1000

// initState holds H0..H7 with each word's bytes reversed, which is the
// memory order the accelerator uses.
var initState = [8]uint32{
	0x67e6096a, 0x85ae67bb, 0x72f36e3c, 0x3af54fa5,
	0x7f520e51, 0x8c68059b, 0xabd9831f, 0x19cde05b,
}

// digest is an accelerated SHA-256 state. The block buffer is a word array
// so the accelerator always sees a word-aligned block.
type digest struct {
	state [8]uint32
	block [BlockSize / 4]uint32
	nx    int
	len   uint64
}

// New returns a new hash.Hash computing the SHA-256 checksum.
func New() hash.Hash {
	d := new(digest)
	d.Reset()
	return d
}

// Accelerated reports whether this build uses the zkVM accelerator.
func Accelerated() bool {
	return true
}

// Reset resets the digest to its initial state.
func (d *digest) Reset() {
	d.state = initState
	d.nx = 0
	d.len = 0
}

// Size returns the checksum size.
func (d *digest) Size() int { return Size }

// BlockSize returns the hash block size.
func (d *digest) BlockSize() int { return BlockSize }

// blockBytes views the block buffer as bytes.
func (d *digest) blockBytes() *[BlockSize]byte {
	return (*[BlockSize]byte)(unsafe.Pointer(&d.block[0]))
}

// compress runs the accelerator over n whole blocks at p, which must be
// word aligned.
func (d *digest) compress(p unsafe.Pointer, n int) {
	for n > 0 {
		c := n
		if c > maxBlocksPerCall {
			c = maxBlocksPerCall
		}
		sysShaBuffer(&d.state[0], &d.state[0], p, uint32(c))
		p = unsafe.Add(p, c*BlockSize)
		n -= c
	}
}

// Write absorbs p into the digest. It never returns an error.
func (d *digest) Write(p []byte) (int, error) {
	n := len(p)
	d.len += uint64(n)
	buf := d.blockBytes()
	if d.nx > 0 {
		c := copy(buf[d.nx:], p)
		d.nx += c
		p = p[c:]
		if d.nx < BlockSize {
			return n, nil
		}
		d.compress(unsafe.Pointer(&d.block[0]), 1)
		d.nx = 0
	}

	// Every block is staged through the word-aligned block buffer. Handing
	// the caller's bytes to the accelerator directly would need an
	// alignment check, and converting the pointer to an integer makes
	// TinyGo treat the caller's buffer as escaping, which turns every
	// stack buffer that is hashed into a heap allocation.
	for len(p) >= BlockSize {
		copy(buf[:], p[:BlockSize])
		d.compress(unsafe.Pointer(&d.block[0]), 1)
		p = p[BlockSize:]
	}
	d.nx = copy(buf[:], p)

	return n, nil
}

// Sum appends the checksum of the data written so far to b without
// changing the digest.
func (d *digest) Sum(b []byte) []byte {
	d0 := *d
	sum := d0.checkSum()
	return append(b, sum[:]...)
}

// checkSum pads and finishes the digest. Padding writes whole words where
// it can, because byte loops and the platform memset are slow in a guest.
func (d *digest) checkSum() [Size]byte {
	bits := d.len << 3
	buf := d.blockBytes()
	buf[d.nx] = 0x80
	i := d.nx + 1
	for ; i < BlockSize && i&3 != 0; i++ {
		buf[i] = 0
	}
	for w := i / 4; w < BlockSize/4; w++ {
		d.block[w] = 0
	}
	if d.nx >= BlockSize-8 {
		d.compress(unsafe.Pointer(&d.block[0]), 1)
		for w := range d.block {
			d.block[w] = 0
		}
	}
	binary.BigEndian.PutUint64(buf[BlockSize-8:], bits)
	d.compress(unsafe.Pointer(&d.block[0]), 1)

	var out [Size]byte
	copy(out[:], (*[Size]byte)(unsafe.Pointer(&d.state[0]))[:])
	return out
}

// sumScratch is the digest Sum256 uses. A stack digest would escape to the
// heap through the accelerator call, and the guest allocator never frees,
// so every hash would leak and zero a fresh digest. Sharing one is safe
// because guests are single threaded and Sum256 never yields.
var sumScratch digest

// Sum256 returns the SHA-256 checksum of the data.
func Sum256(data []byte) [Size]byte {
	d := &sumScratch
	d.Reset()
	d.Write(data)
	return d.checkSum()
}
