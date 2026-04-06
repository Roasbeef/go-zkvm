// This file implements the FFI boundary between Go and the Rust host prover.
//
// The architecture is: Go application -> cgo -> dlopen/dlsym -> Rust cdylib
// (host-ffi) -> risc0-zkvm prover. At runtime, the Go side dynamically loads
// the Rust shared library using dlopen and resolves 6 exported C functions via
// dlsym. All request/response data crosses the boundary as JSON-encoded byte
// buffers with base64-encoded binary payloads.
//
// Memory ownership rules:
//   - Go owns all request buffers; Rust borrows them only for the call duration.
//   - Rust allocates response buffers; Go copies them into Go-managed memory
//     and then calls go_zkvm_free_buffer to release the Rust allocation.
//   - Never call C.free on Rust-allocated memory.
//   - Never hold Rust-returned pointers after calling go_zkvm_free_buffer.

//go:build cgo

package host

/*
#cgo linux LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdint.h>
#include <stdlib.h>

typedef uint32_t (*go_zkvm_abi_version_fn)(void);
typedef int32_t (*go_zkvm_op_fn)(
    const uint8_t*, size_t, uint8_t**, size_t*);
typedef void (*go_zkvm_free_buffer_fn)(uint8_t*, size_t);

static void* go_zkvm_handle = NULL;
static go_zkvm_abi_version_fn go_zkvm_abi_version_ptr = NULL;
static go_zkvm_op_fn go_zkvm_compute_image_id_ptr = NULL;
static go_zkvm_op_fn go_zkvm_execute_ptr = NULL;
static go_zkvm_op_fn go_zkvm_prove_ptr = NULL;
static go_zkvm_op_fn go_zkvm_verify_ptr = NULL;
static go_zkvm_free_buffer_fn go_zkvm_free_buffer_ptr = NULL;

static const char* go_zkvm_host_load(const char* path) {
    if (go_zkvm_handle != NULL) {
        return NULL;
    }

    go_zkvm_handle = dlopen(path, RTLD_NOW | RTLD_LOCAL);
    if (go_zkvm_handle == NULL) {
        return dlerror();
    }

    go_zkvm_abi_version_ptr =
        (go_zkvm_abi_version_fn)dlsym(
            go_zkvm_handle, "go_zkvm_abi_version");
    go_zkvm_compute_image_id_ptr =
        (go_zkvm_op_fn)dlsym(
            go_zkvm_handle, "go_zkvm_compute_image_id");
    go_zkvm_execute_ptr =
        (go_zkvm_op_fn)dlsym(go_zkvm_handle, "go_zkvm_execute");
    go_zkvm_prove_ptr =
        (go_zkvm_op_fn)dlsym(go_zkvm_handle, "go_zkvm_prove");
    go_zkvm_verify_ptr =
        (go_zkvm_op_fn)dlsym(go_zkvm_handle, "go_zkvm_verify");
    go_zkvm_free_buffer_ptr =
        (go_zkvm_free_buffer_fn)dlsym(
            go_zkvm_handle, "go_zkvm_free_buffer");

    if (go_zkvm_abi_version_ptr == NULL ||
        go_zkvm_compute_image_id_ptr == NULL ||
        go_zkvm_execute_ptr == NULL ||
        go_zkvm_prove_ptr == NULL ||
        go_zkvm_verify_ptr == NULL ||
        go_zkvm_free_buffer_ptr == NULL) {
        return dlerror();
    }

    return NULL;
}

static uint32_t go_zkvm_host_loaded_abi_version(void) {
    if (go_zkvm_abi_version_ptr == NULL) {
        return 0;
    }

    return go_zkvm_abi_version_ptr();
}

static int32_t go_zkvm_host_call_compute_image_id(
        const uint8_t* req_ptr, size_t req_len,
        uint8_t** out_ptr, size_t* out_len) {
    if (go_zkvm_compute_image_id_ptr == NULL) {
        return -1;
    }

    return go_zkvm_compute_image_id_ptr(
        req_ptr, req_len, out_ptr, out_len);
}

static int32_t go_zkvm_host_call_execute(
        const uint8_t* req_ptr, size_t req_len,
        uint8_t** out_ptr, size_t* out_len) {
    if (go_zkvm_execute_ptr == NULL) {
        return -1;
    }

    return go_zkvm_execute_ptr(req_ptr, req_len, out_ptr, out_len);
}

static int32_t go_zkvm_host_call_prove(
        const uint8_t* req_ptr, size_t req_len,
        uint8_t** out_ptr, size_t* out_len) {
    if (go_zkvm_prove_ptr == NULL) {
        return -1;
    }

    return go_zkvm_prove_ptr(req_ptr, req_len, out_ptr, out_len);
}

static int32_t go_zkvm_host_call_verify(
        const uint8_t* req_ptr, size_t req_len,
        uint8_t** out_ptr, size_t* out_len) {
    if (go_zkvm_verify_ptr == NULL) {
        return -1;
    }

    return go_zkvm_verify_ptr(req_ptr, req_len, out_ptr, out_len);
}

static void go_zkvm_host_call_free_buffer(uint8_t* ptr, size_t len) {
    if (go_zkvm_free_buffer_ptr != NULL) {
        go_zkvm_free_buffer_ptr(ptr, len);
    }
}
*/
import "C"

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"unsafe"
)

var (
	loadMu     sync.Mutex
	loadedPath string
)

// The types below are internal ABI messages serialized as JSON across the FFI
// boundary. They are not part of the public Go API. The public Go types are
// in types.go.

// ffiErrorResponse is the JSON error payload returned by the Rust side when
// an FFI call fails (non-zero status code).
type ffiErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// computeImageIDRequest is the FFI request for image ID computation.
type computeImageIDRequest struct {
	ABIVersion        uint32 `json:"abi_version"`
	GuestBinaryBase64 string `json:"guest_binary_base64"`
}

// computeImageIDResponse is the FFI response for image ID computation.
type computeImageIDResponse struct {
	ImageID string `json:"image_id"`
}

// executeJSONRequest is the FFI request for execute-only mode.
type executeJSONRequest struct {
	ABIVersion        uint32 `json:"abi_version"`
	GuestBinaryBase64 string `json:"guest_binary_base64"`
	StdinBase64       string `json:"stdin_base64"`
}

// executeJSONResponse is the FFI response for execute-only mode.
type executeJSONResponse struct {
	ImageID       string `json:"image_id"`
	JournalBase64 string `json:"journal_base64"`
	ExitCode      string `json:"exit_code"`
	SegmentCount  uint32 `json:"segment_count"`
	SessionRows   uint64 `json:"session_rows"`
}

// proveJSONRequest is the FFI request for proof generation.
type proveJSONRequest struct {
	ABIVersion        uint32 `json:"abi_version"`
	GuestBinaryBase64 string `json:"guest_binary_base64"`
	StdinBase64       string `json:"stdin_base64"`
	VerifyReceipt     bool   `json:"verify_receipt"`
}

// proveJSONResponse is the FFI response for proof generation.
type proveJSONResponse struct {
	ImageID         string `json:"image_id"`
	JournalBase64   string `json:"journal_base64"`
	ReceiptBase64   string `json:"receipt_base64"`
	ReceiptEncoding string `json:"receipt_encoding"`
	ProverName      string `json:"prover_name"`
	SealBytes       uint64 `json:"seal_bytes"`
}

// verifyJSONRequest is the FFI request for receipt verification.
type verifyJSONRequest struct {
	ABIVersion             uint32 `json:"abi_version"`
	ReceiptBase64          string `json:"receipt_base64"`
	ImageID                string `json:"image_id"`
	ExpectedJournalPresent bool   `json:"expected_journal_present"`
	ExpectedJournalBase64  string `json:"expected_journal_base64"`
}

// verifyJSONResponse is the FFI response for receipt verification.
type verifyJSONResponse struct {
	Verified        bool   `json:"verified"`
	JournalBase64   string `json:"journal_base64"`
	ReceiptEncoding string `json:"receipt_encoding"`
	SealBytes       uint64 `json:"seal_bytes"`
}

// loadFFILibrary loads the Rust shared library via dlopen and resolves all
// exported function pointers. It is guarded by loadMu and will only load
// once; subsequent calls with the same path are no-ops, while calls with a
// different path return an error. After loading, it checks the ABI version
// reported by the library against the expected abiVersion constant.
func loadFFILibrary(path string) error {
	loadMu.Lock()
	defer loadMu.Unlock()

	if loadedPath != "" {
		if loadedPath != path {
			return fmt.Errorf(
				"host: FFI library already loaded from %q, "+
					"cannot reload from %q",
				loadedPath, path,
			)
		}

		return nil
	}

	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	errMsg := C.go_zkvm_host_load(cPath)
	if errMsg != nil {
		return fmt.Errorf(
			"host: load FFI library %q: %s",
			path, C.GoString(errMsg),
		)
	}

	version := uint32(C.go_zkvm_host_loaded_abi_version())
	if version != abiVersion {
		return fmt.Errorf(
			"host: ABI version mismatch: got %d, want %d",
			version, abiVersion,
		)
	}

	loadedPath = path

	return nil
}

// ComputeImageID computes the image ID for a packaged guest binary.
func (c *Client) ComputeImageID(guest []byte) (string, error) {
	req := computeImageIDRequest{
		ABIVersion:        abiVersion,
		GuestBinaryBase64: base64.StdEncoding.EncodeToString(guest),
	}

	var resp computeImageIDResponse
	if err := callJSON(
		"compute_image_id", ffiCallComputeImageID, req, &resp,
	); err != nil {
		return "", err
	}

	return resp.ImageID, nil
}

// Execute runs the packaged guest without generating a proof.
func (c *Client) Execute(
	req ExecuteRequest, opts ...RunOption,
) (*ExecuteResult, error) {

	cfg := defaultRunConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	logRun(cfg.logger, "execute", len(req.GuestBinary), len(req.Stdin))

	jsonReq := executeJSONRequest{
		ABIVersion: abiVersion,
		GuestBinaryBase64: base64.StdEncoding.EncodeToString(
			req.GuestBinary,
		),
		StdinBase64: base64.StdEncoding.EncodeToString(req.Stdin),
	}

	var resp executeJSONResponse
	if err := callJSON(
		"execute", ffiCallExecute, jsonReq, &resp,
	); err != nil {
		return nil, err
	}

	journal, err := base64.StdEncoding.DecodeString(resp.JournalBase64)
	if err != nil {
		return nil, fmt.Errorf("host: decode execute journal: %w", err)
	}

	return &ExecuteResult{
		ImageID:      resp.ImageID,
		Journal:      journal,
		ExitCode:     resp.ExitCode,
		SegmentCount: resp.SegmentCount,
		SessionRows:  resp.SessionRows,
	}, nil
}

// Prove runs the packaged guest through the local prover and returns the
// resulting serialized receipt.
func (c *Client) Prove(
	req ProveRequest, opts ...RunOption,
) (*ProveResult, error) {

	cfg := defaultRunConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	logRun(cfg.logger, "prove", len(req.GuestBinary), len(req.Stdin))

	jsonReq := proveJSONRequest{
		ABIVersion: abiVersion,
		GuestBinaryBase64: base64.StdEncoding.EncodeToString(
			req.GuestBinary,
		),
		StdinBase64:   base64.StdEncoding.EncodeToString(req.Stdin),
		VerifyReceipt: cfg.receiptSelfVerify,
	}

	var resp proveJSONResponse
	if err := callJSON(
		"prove", ffiCallProve, jsonReq, &resp,
	); err != nil {
		return nil, err
	}

	journal, err := base64.StdEncoding.DecodeString(resp.JournalBase64)
	if err != nil {
		return nil, fmt.Errorf("host: decode prove journal: %w", err)
	}

	receipt, err := base64.StdEncoding.DecodeString(resp.ReceiptBase64)
	if err != nil {
		return nil, fmt.Errorf("host: decode prove receipt: %w", err)
	}

	return &ProveResult{
		ImageID:         resp.ImageID,
		Journal:         journal,
		Receipt:         receipt,
		ReceiptEncoding: resp.ReceiptEncoding,
		ProverName:      resp.ProverName,
		SealBytes:       resp.SealBytes,
	}, nil
}

// Verify checks a serialized receipt against an expected image ID and optional
// expected journal bytes.
func (c *Client) Verify(req VerifyRequest) (*VerifyResult, error) {
	jsonReq := verifyJSONRequest{
		ABIVersion: abiVersion,
		ReceiptBase64: base64.StdEncoding.EncodeToString(
			req.Receipt,
		),
		ImageID:                req.ImageID,
		ExpectedJournalPresent: req.ExpectedJournal != nil,
	}
	if req.ExpectedJournal != nil {
		journalBase64 := base64.StdEncoding.EncodeToString(
			req.ExpectedJournal,
		)
		jsonReq.ExpectedJournalBase64 = journalBase64
	}

	var resp verifyJSONResponse
	if err := callJSON(
		"verify", ffiCallVerify, jsonReq, &resp,
	); err != nil {
		return nil, err
	}

	journal, err := base64.StdEncoding.DecodeString(resp.JournalBase64)
	if err != nil {
		return nil, fmt.Errorf("host: decode verify journal: %w", err)
	}

	return &VerifyResult{
		Verified:        resp.Verified,
		Journal:         journal,
		ReceiptEncoding: resp.ReceiptEncoding,
		SealBytes:       resp.SealBytes,
	}, nil
}

// ffiInvoker is the Go function signature that wraps a C ABI FFI call. Each
// of the ffiCallXxx functions below satisfies this type so they can be passed
// to the generic callJSON helper.
type ffiInvoker func(
	reqPtr *C.uint8_t, reqLen C.size_t, outPtr **C.uint8_t,
	outLen *C.size_t,
) C.int32_t

// callJSON is the generic JSON-over-FFI call pattern. It marshals the
// request to JSON, invokes the FFI function, copies the response bytes into
// Go-managed memory (freeing the Rust allocation), and unmarshals the JSON
// response into the provided result struct.
func callJSON[TReq any, TResp any](
	op string, fn ffiInvoker, req TReq, resp *TResp,
) error {

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("host: marshal %s request: %w", op, err)
	}

	var outPtr *C.uint8_t
	var outLen C.size_t
	status := fn(
		bytesPtr(reqBytes), C.size_t(len(reqBytes)),
		&outPtr, &outLen,
	)
	respBytes := copyAndFree(outPtr, outLen)

	if status != 0 {
		return decodeFFIError(op, respBytes, int32(status))
	}

	if len(respBytes) == 0 {
		return fmt.Errorf("host: %s returned an empty response", op)
	}

	if err := json.Unmarshal(respBytes, resp); err != nil {
		return fmt.Errorf("host: decode %s response: %w", op, err)
	}

	return nil
}

// bytesPtr returns a C pointer to the first byte of the slice, or nil if
// the slice is empty. The caller must keep the slice alive for the duration
// of the C call.
func bytesPtr(b []byte) *C.uint8_t {
	if len(b) == 0 {
		return nil
	}

	return (*C.uint8_t)(unsafe.Pointer(&b[0]))
}

// copyAndFree copies Rust-allocated bytes into a Go-managed slice and then
// frees the Rust allocation via go_zkvm_free_buffer. After this call, the
// Rust pointer must not be used again.
func copyAndFree(ptr *C.uint8_t, n C.size_t) []byte {
	if ptr == nil || n == 0 {
		if ptr != nil {
			C.go_zkvm_host_call_free_buffer(ptr, n)
		}

		return nil
	}

	buf := C.GoBytes(unsafe.Pointer(ptr), C.int(n))
	C.go_zkvm_host_call_free_buffer(ptr, n)

	return buf
}

func decodeFFIError(op string, respBytes []byte, status int32) error {
	if len(respBytes) == 0 {
		return &HostError{
			Op:   op,
			Code: "internal_error",
			Message: fmt.Sprintf(
				"FFI call failed with status %d and "+
					"empty error body",
				status,
			),
		}
	}

	var ffiErr ffiErrorResponse
	if err := json.Unmarshal(respBytes, &ffiErr); err != nil {
		return &HostError{
			Op:   op,
			Code: "internal_error",
			Message: fmt.Sprintf(
				"decode FFI error response: %v", err,
			),
		}
	}

	if ffiErr.Code == "" {
		ffiErr.Code = "internal_error"
	}
	if ffiErr.Message == "" {
		ffiErr.Message = "FFI call failed"
	}

	return &HostError{
		Op:      op,
		Code:    ffiErr.Code,
		Message: ffiErr.Message,
	}
}

func logRun(logger *slog.Logger, op string, guestSize, stdinSize int) {
	if logger == nil {
		return
	}

	logger.Info(
		"calling zkVM host FFI",
		slog.String("op", op),
		slog.Int("guest_bytes", guestSize),
		slog.Int("stdin_bytes", stdinSize),
	)
}

func ffiCallComputeImageID(
	reqPtr *C.uint8_t, reqLen C.size_t, outPtr **C.uint8_t,
	outLen *C.size_t,
) C.int32_t {

	return C.go_zkvm_host_call_compute_image_id(
		reqPtr, reqLen, outPtr, outLen,
	)
}

func ffiCallExecute(
	reqPtr *C.uint8_t, reqLen C.size_t, outPtr **C.uint8_t,
	outLen *C.size_t,
) C.int32_t {

	return C.go_zkvm_host_call_execute(
		reqPtr, reqLen, outPtr, outLen,
	)
}

func ffiCallProve(
	reqPtr *C.uint8_t, reqLen C.size_t, outPtr **C.uint8_t,
	outLen *C.size_t,
) C.int32_t {

	return C.go_zkvm_host_call_prove(
		reqPtr, reqLen, outPtr, outLen,
	)
}

func ffiCallVerify(
	reqPtr *C.uint8_t, reqLen C.size_t, outPtr **C.uint8_t,
	outLen *C.size_t,
) C.int32_t {

	return C.go_zkvm_host_call_verify(
		reqPtr, reqLen, outPtr, outLen,
	)
}
