use anyhow::{Context, Result, anyhow, bail};
use ed25519_dalek::{Signature, Signer, SigningKey, Verifier, VerifyingKey};
use libloading::{Library, Symbol};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::ffi::OsString;
use std::fs;
use std::path::{Path, PathBuf};
use std::process::Command;

const MEASUREMENT_DOMAIN: &[u8] = b"TEE_SIM_GUEST_V1";
const GUEST_NAME: &str = "adder";
const GUEST_PACKAGE: &str = "adder-guest";
const GUEST_LIBRARY_BASENAME: &str = "libadder_guest.so";
const QUOTE_VERSION: u32 = 1;

const ENCLAVE_SIGNING_KEY: [u8; 32] = [
    0x10, 0x32, 0x54, 0x76, 0x98, 0xba, 0xdc, 0xfe, 0x13, 0x57, 0x9b, 0xdf, 0x24, 0x68, 0xac, 0xe0,
    0x35, 0x79, 0xbd, 0xf1, 0x46, 0x8a, 0xce, 0x02, 0x57, 0x9d, 0xe3, 0x19, 0x6f, 0xb5, 0xfb, 0x21,
];

type TeeMain = unsafe extern "C" fn(*const u8, usize, *mut u8, usize) -> u32;

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct RegisteredGuest {
    pub guest_name: String,
    pub artifact_path: String,
    pub measurement: String,
}

impl RegisteredGuest {
    fn new(guest_name: String, artifact_path: String, measurement: String) -> Self {
        Self {
            guest_name,
            artifact_path,
            measurement,
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct QuotePayload {
    pub version: u32,
    pub guest_name: String,
    pub measurement: String,
    pub input_hash: String,
    pub output_hash: String,
    pub nonce: String,
}

impl QuotePayload {
    fn from_quote(quote: &Quote) -> Self {
        Self {
            version: quote.version,
            guest_name: quote.guest_name.clone(),
            measurement: quote.measurement.clone(),
            input_hash: quote.input_hash.clone(),
            output_hash: quote.output_hash.clone(),
            nonce: quote.nonce.clone(),
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct Quote {
    pub version: u32,
    pub guest_name: String,
    pub measurement: String,
    pub input_hash: String,
    pub output_hash: String,
    pub nonce: String,
    pub signature: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RunArtifacts {
    pub measurement: String,
    pub sum: i64,
    pub quote: Quote,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct VerificationOutcome {
    pub verified: bool,
    pub guest_name: String,
    pub measurement: String,
}

#[derive(Debug, Clone)]
struct PathPolicy {
    home_dir: Option<PathBuf>,
}

impl PathPolicy {
    fn detect() -> Self {
        Self {
            home_dir: std::env::var_os("HOME").map(PathBuf::from),
        }
    }

    fn sanitize(&self, path: &Path) -> String {
        if let Some(home_dir) = &self.home_dir {
            if let Ok(stripped) = path.strip_prefix(home_dir) {
                return if stripped.as_os_str().is_empty() {
                    "~".to_owned()
                } else {
                    format!("~/{}", stripped.display())
                };
            }
        }
        path.display().to_string()
    }

    fn expand(&self, raw_path: &str) -> PathBuf {
        match raw_path {
            "~" => self.home_dir.clone().unwrap_or_else(|| PathBuf::from("~")),
            _ if raw_path.starts_with("~/") => {
                let suffix = raw_path.trim_start_matches("~/");
                match &self.home_dir {
                    Some(home_dir) => home_dir.join(suffix),
                    None => PathBuf::from(raw_path),
                }
            }
            _ => PathBuf::from(raw_path),
        }
    }
}

#[derive(Debug, Clone)]
struct TeeEnvironment {
    workspace_root: PathBuf,
    path_policy: PathPolicy,
    guest_package: &'static str,
    guest_name: &'static str,
    guest_library_basename: &'static str,
}

impl TeeEnvironment {
    fn detect() -> Self {
        Self {
            workspace_root: PathBuf::from(env!("CARGO_MANIFEST_DIR")),
            path_policy: PathPolicy::detect(),
            guest_package: GUEST_PACKAGE,
            guest_name: GUEST_NAME,
            guest_library_basename: GUEST_LIBRARY_BASENAME,
        }
    }

    fn state_dir(&self) -> PathBuf {
        self.workspace_root.join("state")
    }

    fn default_state_path(&self) -> PathBuf {
        self.state_dir().join("registered.json")
    }

    fn guest_artifact_path(&self) -> PathBuf {
        self.workspace_root
            .join("target")
            .join("release")
            .join(self.guest_library_basename)
    }

    fn build_guest(&self) -> Result<PathBuf> {
        let status = Command::new("cargo")
            .current_dir(&self.workspace_root)
            .args([
                OsString::from("rustc"),
                OsString::from("--package"),
                OsString::from(self.guest_package),
                OsString::from("--release"),
                OsString::from("--"),
                OsString::from("-C"),
                OsString::from("panic=abort"),
            ])
            .status()
            .context("failed to invoke cargo to build the guest artifact")?;

        if !status.success() {
            bail!("guest build failed with status {status}");
        }

        let artifact = self.guest_artifact_path();
        if !artifact.exists() {
            bail!(
                "guest artifact missing at {}",
                self.path_policy.sanitize(&artifact)
            );
        }

        Ok(artifact)
    }

    fn load_json<T>(&self, path: &Path, noun: &str) -> Result<T>
    where
        T: for<'de> Deserialize<'de>,
    {
        let bytes = fs::read(path).with_context(|| {
            format!("failed to read {noun} {}", self.path_policy.sanitize(path))
        })?;
        serde_json::from_slice(&bytes)
            .with_context(|| format!("invalid {noun} {}", self.path_policy.sanitize(path)))
    }

    fn write_json<T>(&self, path: &Path, value: &T, noun: &str) -> Result<()>
    where
        T: Serialize,
    {
        if let Some(parent) = path.parent() {
            fs::create_dir_all(parent).with_context(|| {
                format!(
                    "failed to create directory {}",
                    self.path_policy.sanitize(parent)
                )
            })?;
        }
        let serialized = serde_json::to_vec_pretty(value)?;
        fs::write(path, serialized).with_context(|| {
            format!("failed to write {noun} {}", self.path_policy.sanitize(path))
        })?;
        Ok(())
    }

    fn sanitize_path(&self, path: &Path) -> String {
        self.path_policy.sanitize(path)
    }

    fn expand_user_path(&self, raw_path: &str) -> PathBuf {
        self.path_policy.expand(raw_path)
    }
}

#[derive(Debug, Clone)]
struct MeasurementEngine {
    domain: &'static [u8],
}

impl MeasurementEngine {
    fn new(domain: &'static [u8]) -> Self {
        Self { domain }
    }

    fn compute(&self, bytes: &[u8]) -> String {
        let mut hasher = Sha256::new();
        hasher.update(self.domain);
        hasher.update(bytes);
        hex::encode(hasher.finalize())
    }

    fn compute_file(&self, path: &Path, env: &TeeEnvironment) -> Result<String> {
        let bytes = fs::read(path).with_context(|| {
            format!("failed to read guest artifact {}", env.sanitize_path(path))
        })?;
        Ok(self.compute(&bytes))
    }
}

#[derive(Debug, Clone)]
struct QuoteAuthority {
    signing_key: SigningKey,
    quote_version: u32,
}

impl QuoteAuthority {
    fn new(signing_key_seed: [u8; 32], quote_version: u32) -> Self {
        Self {
            signing_key: SigningKey::from_bytes(&signing_key_seed),
            quote_version,
        }
    }

    fn verifying_key(&self) -> VerifyingKey {
        self.signing_key.verifying_key()
    }

    fn issue_quote(
        &self,
        guest_name: String,
        measurement: String,
        input_bytes: &[u8],
        output_bytes: &[u8],
        nonce: String,
    ) -> Result<Quote> {
        let payload = QuotePayload {
            version: self.quote_version,
            guest_name,
            measurement,
            input_hash: hash_hex(input_bytes),
            output_hash: hash_hex(output_bytes),
            nonce,
        };
        let signature = self
            .signing_key
            .sign(self.canonical_payload(&payload)?.as_bytes());

        Ok(Quote {
            version: payload.version,
            guest_name: payload.guest_name,
            measurement: payload.measurement,
            input_hash: payload.input_hash,
            output_hash: payload.output_hash,
            nonce: payload.nonce,
            signature: hex::encode(signature.to_bytes()),
        })
    }

    fn canonical_payload(&self, payload: &QuotePayload) -> Result<String> {
        serde_json::to_string(payload).map_err(Into::into)
    }
}

#[derive(Debug, Clone)]
pub struct QuoteVerifierEngine {
    verifying_key: VerifyingKey,
    quote_version: u32,
}

impl QuoteVerifierEngine {
    fn new(verifying_key: VerifyingKey, quote_version: u32) -> Self {
        Self {
            verifying_key,
            quote_version,
        }
    }

    pub fn verify(
        &self,
        quote: &Quote,
        expected_measurement: &str,
        expected_nonce: &str,
        expected_guest_name: Option<&str>,
    ) -> Result<VerificationOutcome> {
        if quote.version != self.quote_version {
            bail!("unsupported quote version {}", quote.version);
        }
        if quote.measurement != expected_measurement {
            bail!(
                "measurement mismatch: expected {expected_measurement}, got {}",
                quote.measurement
            );
        }
        if quote.nonce != expected_nonce {
            bail!(
                "nonce mismatch: expected {expected_nonce}, got {}",
                quote.nonce
            );
        }
        if let Some(name) = expected_guest_name {
            if quote.guest_name != name {
                bail!("guest mismatch: expected {name}, got {}", quote.guest_name);
            }
        }

        let signature_bytes = hex::decode(&quote.signature).context("invalid hex signature")?;
        let signature =
            Signature::from_slice(&signature_bytes).context("invalid signature bytes")?;
        self.verifying_key
            .verify(&self.canonical_payload(quote)?.as_bytes(), &signature)
            .context("signature verification failed")?;

        Ok(VerificationOutcome {
            verified: true,
            guest_name: quote.guest_name.clone(),
            measurement: quote.measurement.clone(),
        })
    }

    fn canonical_payload(&self, quote: &Quote) -> Result<String> {
        serde_json::to_string(&QuotePayload::from_quote(quote)).map_err(Into::into)
    }
}

#[derive(Debug, Clone)]
struct ElfGuestRunner;

impl ElfGuestRunner {
    fn invoke(&self, artifact: &Path, a: i64, b: i64, env: &TeeEnvironment) -> Result<i64> {
        let input = encode_input(a, b);
        let mut output = [0_u8; 8];

        let library = unsafe { Library::new(artifact) }
            .with_context(|| format!("failed to load ELF guest {}", env.sanitize_path(artifact)))?;
        let tee_main: Symbol<'_, TeeMain> =
            unsafe { library.get(b"tee_main") }.context("guest does not export tee_main")?;

        let written = unsafe {
            tee_main(
                input.as_ptr(),
                input.len(),
                output.as_mut_ptr(),
                output.len(),
            )
        };
        if written != output.len() as u32 {
            bail!("guest returned invalid output length {written}");
        }

        Ok(i64::from_le_bytes(output))
    }
}

#[derive(Debug, Clone)]
pub struct TeeRuntime {
    env: TeeEnvironment,
    measurement_engine: MeasurementEngine,
    quote_authority: QuoteAuthority,
    quote_verifier: QuoteVerifierEngine,
    guest_runner: ElfGuestRunner,
}

impl TeeRuntime {
    pub fn new() -> Self {
        let env = TeeEnvironment::detect();
        let quote_authority = QuoteAuthority::new(ENCLAVE_SIGNING_KEY, QUOTE_VERSION);
        let quote_verifier =
            QuoteVerifierEngine::new(quote_authority.verifying_key(), QUOTE_VERSION);
        Self {
            env,
            measurement_engine: MeasurementEngine::new(MEASUREMENT_DOMAIN),
            quote_authority,
            quote_verifier,
            guest_runner: ElfGuestRunner,
        }
    }

    pub fn default_state_path(&self) -> PathBuf {
        self.env.default_state_path()
    }

    pub fn register_guest(&self, state_path: &Path) -> Result<RegisteredGuest> {
        let artifact = self.env.build_guest()?;
        let measurement = self.measurement_engine.compute_file(&artifact, &self.env)?;

        let registration = RegisteredGuest::new(
            self.env.guest_name.to_owned(),
            self.env.sanitize_path(&artifact),
            measurement,
        );

        self.persist_registration(state_path, &registration)?;
        Ok(registration)
    }

    pub fn load_registration(&self, state_path: &Path) -> Result<RegisteredGuest> {
        self.env.load_json(state_path, "registration file")
    }

    pub fn persist_registration(
        &self,
        state_path: &Path,
        registration: &RegisteredGuest,
    ) -> Result<()> {
        self.env
            .write_json(state_path, registration, "registration file")
    }

    pub fn run_guest(
        &self,
        state_path: &Path,
        a: i64,
        b: i64,
        nonce: String,
    ) -> Result<RunArtifacts> {
        let registration = self.load_registration(state_path)?;
        let artifact = self.env.build_guest()?;
        let measurement = self.measurement_engine.compute_file(&artifact, &self.env)?;

        if measurement != registration.measurement {
            bail!(
                "measurement mismatch: registered {}, current {}",
                registration.measurement,
                measurement
            );
        }

        let _registered_artifact = self.env.expand_user_path(&registration.artifact_path);
        let sum = self.guest_runner.invoke(&artifact, a, b, &self.env)?;
        let input_bytes = encode_input(a, b);
        let output_bytes = encode_output(sum);
        let quote = self.quote_authority.issue_quote(
            registration.guest_name,
            measurement.clone(),
            &input_bytes,
            &output_bytes,
            nonce,
        )?;

        Ok(RunArtifacts {
            measurement,
            sum,
            quote,
        })
    }

    pub fn write_quote(&self, path: &Path, quote: &Quote) -> Result<()> {
        self.env.write_json(path, quote, "quote file")
    }

    pub fn load_quote(&self, path: &Path) -> Result<Quote> {
        self.env.load_json(path, "quote file")
    }

    pub fn verify_quote(
        &self,
        quote: &Quote,
        expected_measurement: &str,
        expected_nonce: &str,
        expected_guest_name: Option<&str>,
    ) -> Result<VerificationOutcome> {
        self.quote_verifier.verify(
            quote,
            expected_measurement,
            expected_nonce,
            expected_guest_name,
        )
    }
}

impl Default for TeeRuntime {
    fn default() -> Self {
        Self::new()
    }
}

pub fn default_state_path() -> PathBuf {
    TeeRuntime::new().default_state_path()
}

pub fn ensure_quote_matches_sum(quote: &Quote, sum: i64) -> Result<()> {
    let expected_output_hash = hash_hex(&encode_output(sum));
    if quote.output_hash != expected_output_hash {
        return Err(anyhow!(
            "quote output hash mismatch: expected {expected_output_hash}, got {}",
            quote.output_hash
        ));
    }
    Ok(())
}

pub fn encode_input(a: i64, b: i64) -> Vec<u8> {
    let mut bytes = Vec::with_capacity(16);
    bytes.extend_from_slice(&a.to_le_bytes());
    bytes.extend_from_slice(&b.to_le_bytes());
    bytes
}

pub fn encode_output(sum: i64) -> Vec<u8> {
    sum.to_le_bytes().to_vec()
}

fn hash_hex(bytes: &[u8]) -> String {
    hex::encode(Sha256::digest(bytes))
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::tempdir;

    #[test]
    fn measurement_changes_when_bytes_change() {
        let engine = MeasurementEngine::new(MEASUREMENT_DOMAIN);
        let original = engine.compute(b"adder-elf");
        let modified = engine.compute(b"adder-elf!");
        assert_ne!(original, modified);
    }

    #[test]
    fn quote_verification_detects_tampering() {
        let authority = QuoteAuthority::new(ENCLAVE_SIGNING_KEY, QUOTE_VERSION);
        let verifier = QuoteVerifierEngine::new(authority.verifying_key(), QUOTE_VERSION);

        let quote = authority
            .issue_quote(
                GUEST_NAME.to_owned(),
                "abcd".repeat(16),
                &encode_input(2, 3),
                &encode_output(5),
                "nonce-1".to_owned(),
            )
            .expect("quote should be created");

        verifier
            .verify(&quote, &quote.measurement, "nonce-1", Some(GUEST_NAME))
            .expect("valid quote should verify");

        let mut tampered = quote.clone();
        tampered.output_hash = "00".repeat(32);
        let err = verifier
            .verify(
                &tampered,
                &tampered.measurement,
                "nonce-1",
                Some(GUEST_NAME),
            )
            .expect_err("tampering must fail");
        assert!(err.to_string().contains("signature verification failed"));
    }

    #[test]
    fn end_to_end_adder_flow_and_measurement_enforcement() {
        let runtime = TeeRuntime::new();
        let temp = tempdir().expect("tempdir should be created");
        let state_path = temp.path().join("registered.json");

        let registered = runtime
            .register_guest(&state_path)
            .expect("guest should register");
        assert!(registered.artifact_path.starts_with("~/"));

        let run = runtime
            .run_guest(&state_path, 7, 11, "nonce-123".to_owned())
            .expect("guest should execute");
        assert_eq!(run.sum, 18);
        assert_eq!(run.measurement, registered.measurement);
        ensure_quote_matches_sum(&run.quote, run.sum).expect("quote should reflect sum");

        runtime
            .verify_quote(
                &run.quote,
                &registered.measurement,
                "nonce-123",
                Some(GUEST_NAME),
            )
            .expect("quote should verify");

        let mut tampered_registration = registered.clone();
        tampered_registration.measurement = "ff".repeat(32);
        let mismatched_state_path = temp.path().join("mismatched.json");
        runtime
            .persist_registration(&mismatched_state_path, &tampered_registration)
            .expect("tampered registration should be written");

        let err = runtime
            .run_guest(&mismatched_state_path, 1, 2, "nonce-456".to_owned())
            .expect_err("mismatched measurement must be rejected");
        assert!(err.to_string().contains("measurement mismatch"));
    }
}
