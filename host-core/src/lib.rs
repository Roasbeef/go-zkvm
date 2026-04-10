use borsh::{to_vec, BorshDeserialize};
use hex::FromHex;
use risc0_zkvm::{
    compute_image_id, default_executor, default_prover, Digest, ExecutorEnv, Prover, ProverOpts,
    Receipt,
};
use thiserror::Error;

pub const RECEIPT_ENCODING_BORSH: &str = "borsh";

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ProveReceiptKind {
    Composite,
    Succinct,
}

impl ProveReceiptKind {
    pub fn as_str(&self) -> &'static str {
        match self {
            Self::Composite => "composite",
            Self::Succinct => "succinct",
        }
    }

    fn prover_opts(&self) -> ProverOpts {
        match self {
            Self::Composite => ProverOpts::composite(),
            Self::Succinct => ProverOpts::succinct(),
        }
    }
}

#[derive(Debug, Clone)]
pub struct ExecuteRequest {
    pub guest_binary: Vec<u8>,
    pub stdin: Vec<u8>,
    pub assumptions: Vec<Vec<u8>>,
}

#[derive(Debug, Clone)]
pub struct ExecuteResult {
    pub image_id: String,
    pub journal: Vec<u8>,
    pub exit_code: String,
    pub segment_count: u32,
    pub session_rows: u64,
}

#[derive(Debug, Clone)]
pub struct ProveRequest {
    pub guest_binary: Vec<u8>,
    pub stdin: Vec<u8>,
    pub assumptions: Vec<Vec<u8>>,
    pub verify_receipt: bool,
    pub receipt_kind: ProveReceiptKind,
}

#[derive(Debug, Clone)]
pub struct ProveResult {
    pub image_id: String,
    pub journal: Vec<u8>,
    pub receipt: Vec<u8>,
    pub receipt_encoding: String,
    pub receipt_kind: String,
    pub prover_name: String,
    pub seal_bytes: u64,
}

#[derive(Debug, Clone)]
pub struct VerifyRequest {
    pub receipt: Vec<u8>,
    pub image_id: String,
    pub expected_journal: Option<Vec<u8>>,
}

#[derive(Debug, Clone)]
pub struct VerifyResult {
    pub verified: bool,
    pub journal: Vec<u8>,
    pub receipt_encoding: String,
    pub receipt_kind: String,
    pub seal_bytes: u64,
}

#[derive(Debug, Error)]
pub enum HostError {
    #[error("invalid request: {0}")]
    InvalidRequest(String),
    #[error("invalid guest binary: {0}")]
    InvalidGuestBinary(String),
    #[error("invalid receipt: {0}")]
    InvalidReceipt(String),
    #[error("invalid image id: {0}")]
    InvalidImageId(String),
    #[error("execute failed: {0}")]
    ExecuteFailed(String),
    #[error("prove failed: {0}")]
    ProveFailed(String),
    #[error("verify failed: {0}")]
    VerifyFailed(String),
    #[error("journal mismatch: expected {expected_hex}, got {actual_hex}")]
    JournalMismatch {
        expected_hex: String,
        actual_hex: String,
    },
    #[error("internal error: {0}")]
    Internal(String),
}

impl HostError {
    pub fn code(&self) -> &'static str {
        match self {
            Self::InvalidRequest(_) => "invalid_request",
            Self::InvalidGuestBinary(_) => "invalid_guest_binary",
            Self::InvalidReceipt(_) => "invalid_receipt",
            Self::InvalidImageId(_) => "invalid_request",
            Self::ExecuteFailed(_) => "execute_failed",
            Self::ProveFailed(_) => "prove_failed",
            Self::VerifyFailed(_) => "verify_failed",
            Self::JournalMismatch { .. } => "journal_mismatch",
            Self::Internal(_) => "internal_error",
        }
    }
}

pub fn compute_image_id_hex(guest_binary: &[u8]) -> Result<String, HostError> {
    let image_id = compute_image_id(guest_binary)
        .map_err(|err| HostError::InvalidGuestBinary(format!("{err:#}")))?;
    Ok(image_id.to_string())
}

pub fn execute(req: ExecuteRequest) -> Result<ExecuteResult, HostError> {
    let image_id = compute_image_id_hex(&req.guest_binary)?;
    let env = build_env_with_assumptions(&req.stdin, &req.assumptions)?;
    let exec = default_executor();
    let session = exec
        .execute(env, &req.guest_binary)
        .map_err(|err| HostError::ExecuteFailed(format!("{err:#}")))?;

    Ok(ExecuteResult {
        image_id,
        journal: session.journal.bytes.to_vec(),
        exit_code: format!("{:?}", session.exit_code),
        segment_count: session.segments.len() as u32,
        session_rows: session.rows() as u64,
    })
}

pub fn prove(req: ProveRequest) -> Result<ProveResult, HostError> {
    let image_id = compute_image_id(&req.guest_binary)
        .map_err(|err| HostError::InvalidGuestBinary(format!("{err:#}")))?;
    let image_id_hex = image_id.to_string();
    let env = build_env_with_assumptions(&req.stdin, &req.assumptions)?;
    let prover = default_prover();
    let prover_name = prover.get_name().to_string();
    let opts = req.receipt_kind.prover_opts();
    let prove_info = prover
        .prove_with_opts(env, &req.guest_binary, &opts)
        .map_err(|err| HostError::ProveFailed(format!("{err:#}")))?;
    let receipt = prove_info.receipt;
    let receipt_kind = receipt_kind_name(&receipt).to_string();

    if req.verify_receipt {
        receipt
            .verify(image_id)
            .map_err(|err| HostError::VerifyFailed(format!("{err:#}")))?;
    }

    let receipt_bytes = to_vec(&receipt)
        .map_err(|err| HostError::Internal(format!("serialize receipt: {err:#}")))?;

    Ok(ProveResult {
        image_id: image_id_hex,
        journal: receipt.journal.bytes.to_vec(),
        receipt: receipt_bytes,
        receipt_encoding: RECEIPT_ENCODING_BORSH.to_string(),
        receipt_kind,
        prover_name,
        seal_bytes: receipt.seal_size() as u64,
    })
}

pub fn verify(req: VerifyRequest) -> Result<VerifyResult, HostError> {
    let image_id = Digest::from_hex(&req.image_id)
        .map_err(|err| HostError::InvalidImageId(format!("{err:#}")))?;
    let receipt = Receipt::try_from_slice(&req.receipt)
        .map_err(|err| HostError::InvalidReceipt(format!("{err:#}")))?;

    receipt
        .verify(image_id)
        .map_err(|err| HostError::VerifyFailed(format!("{err:#}")))?;

    let actual_journal = receipt.journal.bytes.to_vec();
    if let Some(expected_journal) = req.expected_journal {
        if actual_journal != expected_journal {
            return Err(HostError::JournalMismatch {
                expected_hex: hex::encode(expected_journal),
                actual_hex: hex::encode(actual_journal),
            });
        }
    }

    Ok(VerifyResult {
        verified: true,
        journal: actual_journal,
        receipt_encoding: RECEIPT_ENCODING_BORSH.to_string(),
        receipt_kind: receipt_kind_name(&receipt).to_string(),
        seal_bytes: receipt.seal_size() as u64,
    })
}

fn receipt_kind_name(receipt: &Receipt) -> &'static str {
    match &receipt.inner {
        risc0_zkvm::InnerReceipt::Composite(_) => "composite",
        risc0_zkvm::InnerReceipt::Succinct(_) => "succinct",
        risc0_zkvm::InnerReceipt::Groth16(_) => "groth16",
        risc0_zkvm::InnerReceipt::Fake(_) => "fake",
        _ => "unknown",
    }
}

fn build_env_with_assumptions(
    stdin: &[u8], assumptions: &[Vec<u8>],
) -> Result<ExecutorEnv<'static>, HostError> {
    let mut builder = ExecutorEnv::builder();
    if !stdin.is_empty() {
        builder.write_slice(stdin);
    }

    for (idx, assumption_bytes) in assumptions.iter().enumerate() {
        let receipt = Receipt::try_from_slice(assumption_bytes).map_err(|err| {
            HostError::InvalidRequest(format!(
                "decode assumption receipt {idx}: {err:#}"
            ))
        })?;
        builder.add_assumption(receipt).map_err(|err| {
            HostError::InvalidRequest(format!(
                "add assumption receipt {idx}: {err:#}"
            ))
        })?;
    }

    builder
        .build()
        .map_err(|err| HostError::InvalidRequest(format!("build executor env: {err:#}")))
}
