//go:build tinygo.zkvm

package bigint2

import "unsafe"

// The bigint2 entry points are declared without cgo so that //go:noescape
// applies. A cgo call makes every pointer argument escape, which in a guest
// whose allocator never frees turns each temporary into a heap allocation.

//go:linkname sysBigint2_3 sys_bigint2_3
//go:noescape
func sysBigint2_3(blob unsafe.Pointer, a1, a2, a3 *uint32)

//go:linkname sysBigint2_4 sys_bigint2_4
//go:noescape
func sysBigint2_4(blob unsafe.Pointer, a1, a2, a3, a4 *uint32)

// Accelerated reports whether this build uses the bigint2 accelerator.
func Accelerated() bool {
	return true
}

func blob(words []uint32) unsafe.Pointer {
	return unsafe.Pointer(&words[0])
}

func modMul256(z, x, y, m *Uint256) {
	sysBigint2_4(blob(modMul256Blob[:]), &x[0], &y[0], &m[0], &z[0])
}

func modInv256(z, x, m *Uint256) {
	sysBigint2_3(blob(modInv256Blob[:]), &x[0], &m[0], &z[0])
}

func ecAdd256(z, p, q *Point, c *Curve) {
	sysBigint2_4(blob(ecAdd256Blob[:]), &p[0], &q[0], &c[0], &z[0])
}

func ecDouble256(z, p *Point, c *Curve) {
	sysBigint2_3(blob(ecDouble256Blob[:]), &p[0], &c[0], &z[0])
}
