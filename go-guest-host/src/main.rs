use host_core::{
    compute_image_id_hex, execute as host_execute, prove as host_prove, ExecuteRequest,
    ProveRequest,
};
use std::{env, fs};

const BIP32_HARDENED_KEY_START: u32 = 0x8000_0000;
const BIP86_PURPOSE: u32 = BIP32_HARDENED_KEY_START + 86;
const BIP86_PATH_LEN: usize = 5;
const WITNESS_FLAG_REQUIRE_BIP86: u32 = 1;
const DEFAULT_BIP32_SEED: [u8; 16] = [
    0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
];
const DEFAULT_BIP32_PATH: [u32; 5] = [
    BIP32_HARDENED_KEY_START + 86,
    BIP32_HARDENED_KEY_START,
    BIP32_HARDENED_KEY_START,
    0,
    0,
];
const DEFAULT_POLICY_ITEMS: [u64; 3] = [120, 45, 80];
const DEFAULT_POLICY_DISCOUNT: u64 = 20;
const DEFAULT_POLICY_LIMIT: u64 = 250;
const POLICY_SUMMARY_LEN: usize = 40;

fn hex(bytes: &[u8]) -> String {
    let mut out = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        use std::fmt::Write;
        let _ = write!(&mut out, "{byte:02x}");
    }
    out
}

fn decode_hex(hex_str: &str) -> Result<Vec<u8>, String> {
    let trimmed = hex_str.strip_prefix("0x").unwrap_or(hex_str);
    if trimmed.len() % 2 != 0 {
        return Err("hex input must have even length".to_string());
    }

    let mut out = Vec::with_capacity(trimmed.len() / 2);
    let bytes = trimmed.as_bytes();
    let mut i = 0;
    while i < bytes.len() {
        let hi = (bytes[i] as char)
            .to_digit(16)
            .ok_or_else(|| format!("invalid hex character `{}`", bytes[i] as char))?;
        let lo = (bytes[i + 1] as char)
            .to_digit(16)
            .ok_or_else(|| format!("invalid hex character `{}`", bytes[i + 1] as char))?;
        out.push(((hi << 4) | lo) as u8);
        i += 2;
    }

    Ok(out)
}

fn parse_u64_csv(spec: &str) -> Result<Vec<u64>, String> {
    let trimmed = spec.trim();
    if trimmed.is_empty() {
        return Err("csv input must not be empty".to_string());
    }

    trimmed
        .split(',')
        .map(|value| {
            let item = value.trim();
            item.parse::<u64>()
                .map_err(|_| format!("invalid u64 value `{item}`"))
        })
        .collect()
}

fn parse_bip32_path(path: &str) -> Result<Vec<u32>, String> {
    let trimmed = path.trim();
    if trimmed.is_empty() {
        return Ok(Vec::new());
    }

    let path_body = trimmed.strip_prefix("m/").unwrap_or(trimmed);
    if path_body.is_empty() {
        return Ok(Vec::new());
    }

    path_body
        .split('/')
        .map(|component| {
            let hardened =
                component.ends_with('\'') || component.ends_with('h') || component.ends_with('H');
            let digits = if hardened {
                &component[..component.len() - 1]
            } else {
                component
            };
            let value = digits
                .parse::<u32>()
                .map_err(|_| format!("invalid path component `{component}`"))?;
            if hardened {
                value
                    .checked_add(BIP32_HARDENED_KEY_START)
                    .ok_or_else(|| format!("hardened path component overflow `{component}`"))
            } else {
                Ok(value)
            }
        })
        .collect()
}

fn validate_bip86_path(path: &[u32]) -> Result<(), String> {
    if path.len() != BIP86_PATH_LEN {
        return Err(format!(
            "BIP-86 path must have exactly {BIP86_PATH_LEN} components"
        ));
    }
    if path[0] != BIP86_PURPOSE {
        return Err("BIP-86 path must start with m/86'".to_string());
    }
    if path[1] < BIP32_HARDENED_KEY_START || path[2] < BIP32_HARDENED_KEY_START {
        return Err("BIP-86 coin_type and account must be hardened".to_string());
    }
    if path[3] >= BIP32_HARDENED_KEY_START || path[4] >= BIP32_HARDENED_KEY_START {
        return Err("BIP-86 change and index must be unhardened".to_string());
    }
    if path[3] > 1 {
        return Err("BIP-86 change must be 0 or 1".to_string());
    }

    Ok(())
}

fn load_policy_witness(
    items_spec: Option<&str>,
    discount: Option<u64>,
    limit: Option<u64>,
) -> (Vec<u64>, u64, u64, bool) {
    let using_defaults = items_spec.is_none() && discount.is_none() && limit.is_none();

    let items = match items_spec {
        Some(spec) => parse_u64_csv(spec).expect("invalid --policy-items value"),
        None => DEFAULT_POLICY_ITEMS.to_vec(),
    };

    assert!(!items.is_empty(), "policy guest requires at least one item");

    let discount = discount.unwrap_or(DEFAULT_POLICY_DISCOUNT);
    let limit = limit.unwrap_or(DEFAULT_POLICY_LIMIT);

    (items, discount, limit, using_defaults)
}

fn load_bip32_witness(
    seed_hex: Option<&str>,
    path_spec: Option<&str>,
    use_test_vector: bool,
    require_bip86: bool,
) -> (Vec<u8>, Vec<u32>, u32, bool) {
    let (seed, path, using_test_vector) = match (seed_hex, path_spec, use_test_vector) {
        (Some(_), Some(_), true) => {
            panic!("--use-test-vector cannot be combined with --seed-hex/--path")
        }
        (Some(_), None, _) => panic!("--path is required when --seed-hex is set"),
        (None, Some(_), _) => panic!("--seed-hex is required when --path is set"),
        (Some(seed_hex), Some(path_spec), false) => (
            decode_hex(seed_hex).expect("invalid --seed-hex value"),
            parse_bip32_path(path_spec).expect("invalid --path value"),
            false,
        ),
        (None, None, true) => (
            DEFAULT_BIP32_SEED.to_vec(),
            DEFAULT_BIP32_PATH.to_vec(),
            true,
        ),
        (None, None, false) => {
            panic!("bip32 guest requires --seed-hex and --path, or --use-test-vector")
        }
    };

    if require_bip86 {
        validate_bip86_path(&path).expect("invalid BIP-86 path");
    }

    let mut witness_flags = 0_u32;
    if require_bip86 {
        witness_flags |= WITNESS_FLAG_REQUIRE_BIP86;
    }

    (seed, path, witness_flags, using_test_vector)
}

fn is_bip32_guest(guest_path: &str) -> bool {
    guest_path.contains("bip32")
}

fn is_policy_guest(guest_path: &str) -> bool {
    guest_path.contains("policy_check")
}

fn decode_u32_le(bytes: &[u8], offset: usize) -> Result<u32, String> {
    let slice = bytes
        .get(offset..offset + 4)
        .ok_or_else(|| format!("journal too short to read u32 at offset {offset}"))?;
    let mut buf = [0_u8; 4];
    buf.copy_from_slice(slice);
    Ok(u32::from_le_bytes(buf))
}

fn decode_u64_le(bytes: &[u8], offset: usize) -> Result<u64, String> {
    let slice = bytes
        .get(offset..offset + 8)
        .ok_or_else(|| format!("journal too short to read u64 at offset {offset}"))?;
    let mut buf = [0_u8; 8];
    buf.copy_from_slice(slice);
    Ok(u64::from_le_bytes(buf))
}

fn print_policy_summary(bytes: &[u8]) {
    assert_eq!(
        bytes.len(),
        POLICY_SUMMARY_LEN,
        "policy summary journal must be {POLICY_SUMMARY_LEN} bytes"
    );

    let item_count = decode_u32_le(bytes, 0).expect("invalid item_count");
    let approved = decode_u32_le(bytes, 4).expect("invalid approved flag") != 0;
    let subtotal = decode_u64_le(bytes, 8).expect("invalid subtotal");
    let discount = decode_u64_le(bytes, 16).expect("invalid discount");
    let total = decode_u64_le(bytes, 24).expect("invalid total");
    let limit = decode_u64_le(bytes, 32).expect("invalid limit");

    println!("✓ Policy summary:");
    println!("  Item count: {}", item_count);
    println!("  Approved: {}", approved);
    println!("  Subtotal: {}", subtotal);
    println!("  Discount: {}", discount);
    println!("  Total: {}", total);
    println!("  Limit: {}", limit);
}

fn append_u32_le(out: &mut Vec<u8>, value: u32) {
    out.extend_from_slice(&value.to_le_bytes());
}

fn append_u64_le(out: &mut Vec<u8>, value: u64) {
    out.extend_from_slice(&value.to_le_bytes());
}

fn append_u32_slice(out: &mut Vec<u8>, values: &[u32]) {
    for value in values {
        append_u32_le(out, *value);
    }
}

fn append_u64_slice(out: &mut Vec<u8>, values: &[u64]) {
    for value in values {
        append_u64_le(out, *value);
    }
}

fn run() {
    let mut guest_path = String::from("../multiply.bin");
    let mut raw_journal = false;
    let mut execute_only = false;
    let mut seed_hex: Option<String> = None;
    let mut path_spec: Option<String> = None;
    let mut use_test_vector = false;
    let mut require_bip86 = false;
    let mut policy_items: Option<String> = None;
    let mut policy_discount: Option<u64> = None;
    let mut policy_limit: Option<u64> = None;

    let args: Vec<String> = env::args().skip(1).collect();
    let mut index = 0;
    while index < args.len() {
        let arg = &args[index];
        match arg.as_str() {
            "--raw-journal" => raw_journal = true,
            "--execute-only" => execute_only = true,
            "--use-test-vector" => use_test_vector = true,
            "--require-bip86" => require_bip86 = true,
            "--help" | "-h" => {
                println!(
                    "usage: cargo run -- [guest.bin] [--raw-journal] [--execute-only] [--seed-hex HEX --path PATH | --use-test-vector] [--require-bip86] [--policy-items CSV] [--policy-discount N] [--policy-limit N]"
                );
                return;
            }
            "--seed-hex" => {
                index += 1;
                seed_hex = Some(
                    args.get(index)
                        .cloned()
                        .expect("--seed-hex requires a value"),
                );
            }
            "--path" => {
                index += 1;
                path_spec = Some(args.get(index).cloned().expect("--path requires a value"));
            }
            "--policy-items" => {
                index += 1;
                policy_items = Some(
                    args.get(index)
                        .cloned()
                        .expect("--policy-items requires a value"),
                );
            }
            "--policy-discount" => {
                index += 1;
                policy_discount = Some(
                    args.get(index)
                        .expect("--policy-discount requires a value")
                        .parse::<u64>()
                        .expect("invalid --policy-discount value"),
                );
            }
            "--policy-limit" => {
                index += 1;
                policy_limit = Some(
                    args.get(index)
                        .expect("--policy-limit requires a value")
                        .parse::<u64>()
                        .expect("invalid --policy-limit value"),
                );
            }
            _ if arg.starts_with("--seed-hex=") => {
                seed_hex = Some(arg["--seed-hex=".len()..].to_string());
            }
            _ if arg.starts_with("--path=") => {
                path_spec = Some(arg["--path=".len()..].to_string());
            }
            _ if arg.starts_with("--policy-items=") => {
                policy_items = Some(arg["--policy-items=".len()..].to_string());
            }
            _ if arg.starts_with("--policy-discount=") => {
                policy_discount = Some(
                    arg["--policy-discount=".len()..]
                        .parse::<u64>()
                        .expect("invalid --policy-discount value"),
                );
            }
            _ if arg.starts_with("--policy-limit=") => {
                policy_limit = Some(
                    arg["--policy-limit=".len()..]
                        .parse::<u64>()
                        .expect("invalid --policy-limit value"),
                );
            }
            _ => guest_path = arg.clone(),
        }
        index += 1;
    }

    tracing_subscriber::fmt()
        .with_env_filter(tracing_subscriber::EnvFilter::from_default_env())
        .init();

    println!("=== Go Guest Host ===\n");

    let guest_binary = fs::read(&guest_path).expect("Failed to read guest binary");
    let image_id = compute_image_id_hex(&guest_binary).expect("Failed to compute image ID");

    println!(
        "✓ Loaded guest binary `{}`: {} bytes",
        guest_path,
        guest_binary.len()
    );
    println!("✓ Image ID: {}", image_id);

    let stdin = if is_bip32_guest(&guest_path) {
        assert!(raw_journal, "bip32 guest requires --raw-journal");
        let (seed, path, witness_flags, using_test_vector) = load_bip32_witness(
            seed_hex.as_deref(),
            path_spec.as_deref(),
            use_test_vector,
            require_bip86,
        );

        let witness_desc = if require_bip86 {
            "private BIP-32 witness with BIP-86 policy"
        } else {
            "private BIP-32 witness"
        };
        if using_test_vector {
            println!("✓ Sending {witness_desc} (built-in test vector)");
        } else {
            println!("✓ Sending {witness_desc}");
        }

        let mut stdin = Vec::new();
        append_u32_le(&mut stdin, witness_flags);
        append_u32_le(&mut stdin, seed.len() as u32);
        stdin.extend_from_slice(seed.as_slice());
        append_u32_le(&mut stdin, path.len() as u32);
        append_u32_slice(&mut stdin, path.as_slice());
        stdin
    } else if is_policy_guest(&guest_path) {
        let (items, discount, limit, using_defaults) =
            load_policy_witness(policy_items.as_deref(), policy_discount, policy_limit);
        let item_count = items.len() as u32;

        if using_defaults {
            println!("✓ Sending private policy witness (built-in sample)");
        } else {
            println!("✓ Sending private policy witness");
        }

        let mut stdin = Vec::new();
        append_u32_le(&mut stdin, item_count);
        append_u64_slice(&mut stdin, items.as_slice());
        append_u64_le(&mut stdin, discount);
        append_u64_le(&mut stdin, limit);
        stdin
    } else if raw_journal {
        println!("✓ Running in raw-journal mode (no host inputs)");
        Vec::new()
    } else {
        let a: u64 = 17;
        let b: u64 = 23;

        println!("✓ Sending inputs to guest: {} * {}", a, b);

        let mut stdin = Vec::new();
        append_u64_le(&mut stdin, a);
        append_u64_le(&mut stdin, b);
        stdin
    };

    if execute_only {
        println!("✓ Executing guest program without proving...\n");

        let session = host_execute(ExecuteRequest {
            guest_binary,
            stdin,
        })
        .expect("Execution failed");
        println!("✓ Execution successful!");

        if is_policy_guest(&guest_path) {
            if raw_journal {
                println!("✓ Raw journal hex: {}", hex(&session.journal));
            }
            print_policy_summary(&session.journal);
        } else if raw_journal {
            println!("✓ Raw journal hex: {}", hex(&session.journal));
        } else {
            eprintln!(
                "✗ Non-raw decode mode is only implemented for execute-only raw-journal runs"
            );
            std::process::exit(1);
        }

        println!("Session info:");
        println!("  Exit code: {}", session.exit_code);
        println!("  Journal size: {} bytes", session.journal.len());
        println!("  Segments: {}", session.segment_count);
        println!("  Rows: {}", session.session_rows);
        return;
    }

    println!("✓ Proving guest execution...\n");

    let prove_result = host_prove(ProveRequest {
        guest_binary,
        stdin,
        verify_receipt: true,
    })
    .expect("Proving failed");
    println!("✓ Using prover backend: {}", prove_result.prover_name);
    println!("✓ Receipt verified against image ID");

    if is_policy_guest(&guest_path) {
        if raw_journal {
            println!("✓ Raw journal hex: {}", hex(&prove_result.journal));
        }
        print_policy_summary(&prove_result.journal);
    } else if raw_journal {
        println!("✓ Raw journal hex: {}", hex(&prove_result.journal));
    } else {
        let product =
            decode_u64_le(&prove_result.journal, 0).expect("Failed to decode journal output");
        let expected = 17_u64 * 23_u64;

        println!("✓ Guest computed product: {}", product);
        assert_eq!(product, expected, "Product mismatch!");
    }

    println!("Receipt info:");
    println!("  Journal size: {} bytes", prove_result.journal.len());
    println!("  Proof seal size: {} bytes", prove_result.seal_bytes);
    println!("\n✅ Go guest prove+verify PASSED!");
}

fn main() {
    std::thread::Builder::new()
        .name("go-guest-host".to_string())
        .stack_size(64 * 1024 * 1024)
        .spawn(run)
        .expect("Failed to spawn host thread")
        .join()
        .expect("Host thread panicked");
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn validates_bip86_path_shape() {
        let path = vec![
            BIP86_PURPOSE,
            BIP32_HARDENED_KEY_START,
            BIP32_HARDENED_KEY_START + 7,
            1,
            42,
        ];
        assert!(validate_bip86_path(&path).is_ok());
    }

    #[test]
    fn rejects_non_bip86_path_shape() {
        let path = vec![
            BIP32_HARDENED_KEY_START + 84,
            BIP32_HARDENED_KEY_START,
            BIP32_HARDENED_KEY_START,
            0,
            0,
        ];
        assert!(validate_bip86_path(&path).is_err());
    }
}
