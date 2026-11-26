//go:build tinygo.zkvm

package bigint2

/*
#include <stdint.h>
void sys_bigint2_3(const uint8_t* blob, const uint32_t* a1,
	const uint32_t* a2, const uint32_t* a3);
void sys_bigint2_4(const uint8_t* blob, const uint32_t* a1,
	const uint32_t* a2, const uint32_t* a3, const uint32_t* a4);
*/
import "C"

import "unsafe"

// Accelerated reports whether this build uses the bigint2 accelerator.
func Accelerated() bool {
	return true
}

func blob(words []uint32) *C.uint8_t {
	return (*C.uint8_t)(unsafe.Pointer(&words[0]))
}

func ptr(words []uint32) *C.uint32_t {
	return (*C.uint32_t)(unsafe.Pointer(&words[0]))
}

func modMul256(z, x, y, m *Uint256) {
	C.sys_bigint2_4(blob(modMul256Blob[:]), ptr(x[:]), ptr(y[:]),
		ptr(m[:]), ptr(z[:]))
}

func modInv256(z, x, m *Uint256) {
	C.sys_bigint2_3(blob(modInv256Blob[:]), ptr(x[:]), ptr(m[:]),
		ptr(z[:]))
}

func ecAdd256(z, p, q *Point, c *Curve) {
	C.sys_bigint2_4(blob(ecAdd256Blob[:]), ptr(p[:]), ptr(q[:]),
		ptr(c[:]), ptr(z[:]))
}

func ecDouble256(z, p *Point, c *Curve) {
	C.sys_bigint2_3(blob(ecDouble256Blob[:]), ptr(p[:]), ptr(c[:]),
		ptr(z[:]))
}
