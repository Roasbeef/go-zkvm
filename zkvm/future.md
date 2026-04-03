# Future Improvements

## Direct Go Assembly Approach

Instead of using CGO, we could implement zkVM system calls directly in Go assembly:

```asm
// zkVM system calls implemented in Go assembly for RISC-V
// These match the RISC Zero zkVM ABI

#include "textflag.h"

// func sysWrite(fd uint32, buf unsafe.Pointer, len int)
TEXT ·sysWrite(SB), NOSPLIT, $0-16
    MOVW fd+0(FP), A0      // load fd into a0
    MOVW buf+4(FP), A1     // load buf pointer into a1
    MOVW len+8(FP), A2     // load len into a2
    MOVW $2, T0            // software ecall
    MOVW $2, A7            // SYS_IO
    ECALL
    RET

// func sysHalt(exitCode uint8)
TEXT ·sysHalt(SB), NOSPLIT, $0-1
    MOVBU exitCode+0(FP), A0  // load exit code into a0
    MOVW $0, T0               // halt ecall
    ECALL
    RET

// func sysRead(fd uint32, buf unsafe.Pointer, len uint32)
TEXT ·sysRead(SB), NOSPLIT, $0-12
    MOVW fd+0(FP), A0      // load fd into a0
    MOVW buf+4(FP), A1     // load buf pointer into a1
    MOVW len+8(FP), A2     // load len into a2
    MOVW $2, T0            // software ecall
    MOVW $2, A7            // SYS_IO
    ECALL
    RET

// func sysCommit(buf unsafe.Pointer, len uint32)
TEXT ·sysCommit(SB), NOSPLIT, $0-8
    MOVW buf+0(FP), A0     // load buf pointer into a0
    MOVW len+4(FP), A1     // load len into a1
    MOVW $2, T0            // software ecall
    MOVW $1, A7            // SYS_OUTPUT (commit to journal)
    ECALL
    RET

// func sysCycleCount() uint32
TEXT ·sysCycleCount(SB), NOSPLIT, $0-4
    MOVW $2, T0            // software ecall
    MOVW $4, A7            // SYS_CYCLE_COUNT
    ECALL
    MOVW A0, ret+0(FP)     // return result in a0
    RET
```

This approach would eliminate the CGO dependency and might provide better performance. However, it requires more testing with TinyGo's assembly support for RISC-V targets.