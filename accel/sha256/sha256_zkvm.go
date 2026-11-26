//go:build tinygo.zkvm

package sha256

/*
#include <stdint.h>
void sys_sha_buffer(uint32_t* out_state, uint32_t* in_state, uint8_t* buf,
	uint32_t count);
*/
import "C"

import (
	"encoding/binary"
	"hash"
	"unsafe"
)

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
		C.sys_sha_buffer(
			(*C.uint32_t)(unsafe.Pointer(&d.state[0])),
			(*C.uint32_t)(unsafe.Pointer(&d.state[0])),
			(*C.uint8_t)(p), C.uint32_t(c),
		)
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

	// Whole blocks from a word-aligned input go to the accelerator
	// directly. Unaligned input is staged one block at a time.
	if blocks := len(p) / BlockSize; blocks > 0 {
		base := unsafe.Pointer(unsafe.SliceData(p))
		if uintptr(base)&3 == 0 {
			d.compress(base, blocks)
		} else {
			for i := 0; i < blocks; i++ {
				copy(buf[:], p[i*BlockSize:])
				d.compress(unsafe.Pointer(&d.block[0]), 1)
			}
		}
		p = p[blocks*BlockSize:]
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

// checkSum pads and finishes the digest.
func (d *digest) checkSum() [Size]byte {
	bits := d.len << 3
	buf := d.blockBytes()
	buf[d.nx] = 0x80
	for i := d.nx + 1; i < BlockSize; i++ {
		buf[i] = 0
	}
	if d.nx >= BlockSize-8 {
		d.compress(unsafe.Pointer(&d.block[0]), 1)
		for i := range buf {
			buf[i] = 0
		}
	}
	binary.BigEndian.PutUint64(buf[BlockSize-8:], bits)
	d.compress(unsafe.Pointer(&d.block[0]), 1)

	var out [Size]byte
	copy(out[:], (*[Size]byte)(unsafe.Pointer(&d.state[0]))[:])
	return out
}

// Sum256 returns the SHA-256 checksum of the data.
func Sum256(data []byte) [Size]byte {
	var d digest
	d.Reset()
	d.Write(data)
	return d.checkSum()
}
