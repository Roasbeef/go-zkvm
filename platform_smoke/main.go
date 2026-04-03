package main

/*
#include <stdint.h>
typedef struct sha256_state sha256_state;
sha256_state* init_sha256(void);
void env_commit(sha256_state* hasher, const uint8_t* bytes_ptr, uint32_t len);
void env_exit(sha256_state* hasher, uint8_t exit_code);
*/
import "C"

import "unsafe"

func main() {
	msg := [4]byte{1, 2, 3, 4}
	hasher := C.init_sha256()
	C.env_commit(hasher, (*C.uint8_t)(unsafe.Pointer(&msg[0])), C.uint32_t(len(msg)))
	C.env_exit(hasher, 0)
}
