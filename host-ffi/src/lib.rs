use std::{
    panic::{catch_unwind, AssertUnwindSafe},
    slice,
};

use base64::{engine::general_purpose::STANDARD, Engine as _};
use host_core::{
    compute_image_id_hex, execute as host_execute, prove as host_prove, verify as host_verify,
    ExecuteRequest, HostError, ProveReceiptKind, ProveRequest, VerifyRequest,
};
use serde::{de::DeserializeOwned, Deserialize, Serialize};

const ABI_VERSION: u32 = 1;

#[derive(Debug, Serialize)]
struct ErrorResponse {
    code: String,
    message: String,
}

#[derive(Debug, Deserialize)]
struct ComputeImageIdRequest {
    abi_version: u32,
    guest_binary_base64: String,
}

#[derive(Debug, Serialize)]
struct ComputeImageIdResponse {
    image_id: String,
}

#[derive(Debug, Deserialize)]
struct ExecuteJsonRequest {
    abi_version: u32,
    guest_binary_base64: String,
    stdin_base64: String,
}

#[derive(Debug, Serialize)]
struct ExecuteJsonResponse {
    image_id: String,
    journal_base64: String,
    exit_code: String,
    segment_count: u32,
    session_rows: u64,
}

#[derive(Debug, Deserialize)]
struct ProveJsonRequest {
    abi_version: u32,
    guest_binary_base64: String,
    stdin_base64: String,
    verify_receipt: bool,
    receipt_kind: String,
}

#[derive(Debug, Serialize)]
struct ProveJsonResponse {
    image_id: String,
    journal_base64: String,
    receipt_base64: String,
    receipt_encoding: String,
    receipt_kind: String,
    prover_name: String,
    seal_bytes: u64,
}

#[derive(Debug, Deserialize)]
struct VerifyJsonRequest {
    abi_version: u32,
    receipt_base64: String,
    image_id: String,
    expected_journal_present: bool,
    expected_journal_base64: String,
}

#[derive(Debug, Serialize)]
struct VerifyJsonResponse {
    verified: bool,
    journal_base64: String,
    receipt_encoding: String,
    receipt_kind: String,
    seal_bytes: u64,
}

#[no_mangle]
pub extern "C" fn go_zkvm_abi_version() -> u32 {
    ABI_VERSION
}

#[no_mangle]
pub extern "C" fn go_zkvm_compute_image_id(
    req_ptr: *const u8,
    req_len: usize,
    out_ptr: *mut *mut u8,
    out_len: *mut usize,
) -> i32 {
    invoke(req_ptr, req_len, out_ptr, out_len, |bytes| {
        let req: ComputeImageIdRequest = parse_request(bytes)?;
        validate_abi(req.abi_version)?;

        let guest_binary = decode_base64("guest_binary_base64", &req.guest_binary_base64)?;
        let image_id = compute_image_id_hex(&guest_binary).map_err(error_from_host)?;

        Ok(ComputeImageIdResponse { image_id })
    })
}

#[no_mangle]
pub extern "C" fn go_zkvm_execute(
    req_ptr: *const u8,
    req_len: usize,
    out_ptr: *mut *mut u8,
    out_len: *mut usize,
) -> i32 {
    invoke(req_ptr, req_len, out_ptr, out_len, |bytes| {
        let req: ExecuteJsonRequest = parse_request(bytes)?;
        validate_abi(req.abi_version)?;

        let guest_binary = decode_base64("guest_binary_base64", &req.guest_binary_base64)?;
        let stdin = decode_base64("stdin_base64", &req.stdin_base64)?;
        let result = host_execute(ExecuteRequest {
            guest_binary,
            stdin,
        })
        .map_err(error_from_host)?;

        Ok(ExecuteJsonResponse {
            image_id: result.image_id,
            journal_base64: encode_base64(&result.journal),
            exit_code: result.exit_code,
            segment_count: result.segment_count,
            session_rows: result.session_rows,
        })
    })
}

#[no_mangle]
pub extern "C" fn go_zkvm_prove(
    req_ptr: *const u8,
    req_len: usize,
    out_ptr: *mut *mut u8,
    out_len: *mut usize,
) -> i32 {
    invoke(req_ptr, req_len, out_ptr, out_len, |bytes| {
        let req: ProveJsonRequest = parse_request(bytes)?;
        validate_abi(req.abi_version)?;

        let guest_binary = decode_base64("guest_binary_base64", &req.guest_binary_base64)?;
        let stdin = decode_base64("stdin_base64", &req.stdin_base64)?;
        let receipt_kind = parse_prove_receipt_kind(&req.receipt_kind)?;
        let result = host_prove(ProveRequest {
            guest_binary,
            stdin,
            verify_receipt: req.verify_receipt,
            receipt_kind,
        })
        .map_err(error_from_host)?;

        Ok(ProveJsonResponse {
            image_id: result.image_id,
            journal_base64: encode_base64(&result.journal),
            receipt_base64: encode_base64(&result.receipt),
            receipt_encoding: result.receipt_encoding,
            receipt_kind: result.receipt_kind,
            prover_name: result.prover_name,
            seal_bytes: result.seal_bytes,
        })
    })
}

#[no_mangle]
pub extern "C" fn go_zkvm_verify(
    req_ptr: *const u8,
    req_len: usize,
    out_ptr: *mut *mut u8,
    out_len: *mut usize,
) -> i32 {
    invoke(req_ptr, req_len, out_ptr, out_len, |bytes| {
        let req: VerifyJsonRequest = parse_request(bytes)?;
        validate_abi(req.abi_version)?;

        let receipt = decode_base64("receipt_base64", &req.receipt_base64)?;
        let expected_journal = if req.expected_journal_present {
            Some(decode_base64(
                "expected_journal_base64",
                &req.expected_journal_base64,
            )?)
        } else {
            None
        };

        let result = host_verify(VerifyRequest {
            receipt,
            image_id: req.image_id,
            expected_journal,
        })
        .map_err(error_from_host)?;

        Ok(VerifyJsonResponse {
            verified: result.verified,
            journal_base64: encode_base64(&result.journal),
            receipt_encoding: result.receipt_encoding,
            receipt_kind: result.receipt_kind,
            seal_bytes: result.seal_bytes,
        })
    })
}

#[no_mangle]
pub extern "C" fn go_zkvm_free_buffer(ptr: *mut u8, len: usize) {
    if ptr.is_null() {
        return;
    }

    unsafe {
        let slice = std::ptr::slice_from_raw_parts_mut(ptr, len);
        drop(Box::from_raw(slice));
    }
}

fn invoke<T, F>(
    req_ptr: *const u8,
    req_len: usize,
    out_ptr: *mut *mut u8,
    out_len: *mut usize,
    handler: F,
) -> i32
where
    T: Serialize,
    F: FnOnce(&[u8]) -> Result<T, ErrorResponse>,
{
    if out_ptr.is_null() || out_len.is_null() {
        return 1;
    }

    unsafe {
        *out_ptr = std::ptr::null_mut();
        *out_len = 0;
    }

    let result = catch_unwind(AssertUnwindSafe(|| {
        let req_bytes = read_input(req_ptr, req_len)?;
        let response = handler(req_bytes)?;
        let payload = serde_json::to_vec(&response)
            .map_err(|err| internal_error(format!("serialize response: {err}")))?;
        Ok::<Vec<u8>, ErrorResponse>(payload)
    }));

    let (status, payload) = match result {
        Ok(Ok(payload)) => (0_i32, payload),
        Ok(Err(err)) => (1_i32, serialize_error(err)),
        Err(panic) => {
            let message = if let Some(msg) = panic.downcast_ref::<&str>() {
                (*msg).to_string()
            } else if let Some(msg) = panic.downcast_ref::<String>() {
                msg.clone()
            } else {
                "panic in FFI boundary".to_string()
            };
            (
                2_i32,
                serialize_error(internal_error(format!("panic in FFI boundary: {message}"))),
            )
        }
    };

    unsafe {
        let boxed = payload.into_boxed_slice();
        *out_len = boxed.len();
        *out_ptr = Box::into_raw(boxed) as *mut u8;
    }

    status
}

fn read_input<'a>(ptr: *const u8, len: usize) -> Result<&'a [u8], ErrorResponse> {
    if ptr.is_null() {
        if len == 0 {
            return Ok(&[]);
        }
        return Err(invalid_request(
            "request pointer was null with non-zero length",
        ));
    }

    Ok(unsafe { slice::from_raw_parts(ptr, len) })
}

fn parse_request<T: DeserializeOwned>(bytes: &[u8]) -> Result<T, ErrorResponse> {
    serde_json::from_slice(bytes)
        .map_err(|err| invalid_request(format!("decode request JSON: {err}")))
}

fn validate_abi(abi_version: u32) -> Result<(), ErrorResponse> {
    if abi_version != ABI_VERSION {
        return Err(invalid_request(format!(
            "unsupported ABI version {abi_version}; expected {ABI_VERSION}"
        )));
    }

    Ok(())
}

fn decode_base64(field: &str, value: &str) -> Result<Vec<u8>, ErrorResponse> {
    STANDARD
        .decode(value)
        .map_err(|err| invalid_request(format!("decode {field}: {err}")))
}

fn encode_base64(bytes: &[u8]) -> String {
    STANDARD.encode(bytes)
}

fn parse_prove_receipt_kind(value: &str) -> Result<ProveReceiptKind, ErrorResponse> {
    match value {
        "composite" => Ok(ProveReceiptKind::Composite),
        "succinct" => Ok(ProveReceiptKind::Succinct),
        other => Err(invalid_request(format!(
            "unsupported receipt_kind `{other}`; expected composite or succinct"
        ))),
    }
}

fn error_from_host(err: HostError) -> ErrorResponse {
    ErrorResponse {
        code: err.code().to_string(),
        message: err.to_string(),
    }
}

fn invalid_request(message: impl Into<String>) -> ErrorResponse {
    ErrorResponse {
        code: "invalid_request".to_string(),
        message: message.into(),
    }
}

fn internal_error(message: impl Into<String>) -> ErrorResponse {
    ErrorResponse {
        code: "internal_error".to_string(),
        message: message.into(),
    }
}

fn serialize_error(err: ErrorResponse) -> Vec<u8> {
    serde_json::to_vec(&err).unwrap_or_else(|fallback_err| {
        format!(
            r#"{{"code":"internal_error","message":"failed to serialize error response: {fallback_err}"}}"#
        )
        .into_bytes()
    })
}
