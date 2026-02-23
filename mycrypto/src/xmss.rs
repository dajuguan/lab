/* eXtended Merkle Signature Scheme (XMSS) implementation in Rust.
This module provides a basic implementation of the XMSS signature scheme, which is a hash-based signature.
It's not secure for production use and is intended for educational purposes only.
The (height, Winternitz w) are important parameters that affect the security and performance of the scheme.
- The height determines the number of signatures that can be generated (2^height),
- the Winternitz parameter w determines the WOTS parameters, the larger the w (bits is log2(w)):
    - the smaller the WOTS signatures
    - it changes the signing/verification per-chunk chain split (sign uses x, verify uses w - 1 - x), which is balanced by the checksum digits
    - with checksum digits, total chain budget is parameter-constrained; larger w still usually reduces parallelism because there are fewer independent chunks.
 */

use num_bigint::BigUint;
use p3_field::Field;
use p3_field::PrimeCharacteristicRing;
use p3_field::PrimeField64;
use p3_koala_bear::{KoalaBear, Poseidon2KoalaBear, default_koalabear_poseidon2_16};
use p3_symmetric::Permutation;
use rand::Rng;
use rayon::prelude::*;
use std::borrow::Cow;
use std::time::Instant;

pub const MESSAGE_LEN: usize = 32; // message length in bytes
pub const MESSAGE_LEN_FE: usize = 5; // message field elements
pub const TREE_HEIGHT: usize = 10; // height of the Merkle tree
pub const DEFAULT_WINTERNITZ_W: usize = 4; // Winternitz parameter (base-w) for WOTS

type HashFe<FF> = [FF; MESSAGE_LEN_FE];

pub trait XmssHashOps {
    type Field: Field + PrimeCharacteristicRing + PrimeField64 + Copy + Eq + Send + Sync;

    fn winternitz_w(&self) -> usize; // Winternitz parameter w for WOTS, bits is log2(w)
    fn hash_one_in_place(&self, input: &mut HashFe<Self::Field>);
    fn hash_pair_in_place(&self, left: &mut HashFe<Self::Field>, right: &HashFe<Self::Field>);

    fn keygen<R: Rng>(
        &self,
        rng: &mut R,
        height: usize,
    ) -> (XmssPublicKey<Self::Field>, XmssSecretKey<Self::Field>) {
        let leaf_count = 1usize << height;
        let mut signing_keys = Vec::with_capacity(leaf_count);
        // simulate the generation of random field elements for signing keys, in practice this should be done securely.
        for _ in 0..leaf_count {
            signing_keys.push(std::array::from_fn(|_| {
                Self::Field::from_u64(rng.random::<u64>() % Self::Field::ORDER_U64)
            }));
        }

        let mut leaf_public_keys = Vec::with_capacity(leaf_count);
        for secret in &signing_keys {
            leaf_public_keys.push(self.derive_leaf_from_secret(secret));
        }

        let sk = XmssSecretKey {
            height,
            signing_keys,
            leaf_public_keys,
        };
        let pk = XmssPublicKey {
            root: self.compute_merkle_root(sk.leaf_public_keys()),
        };
        (pk, sk)
    }

    fn chunk_count(&self) -> usize {
        self.message_digits() + self.checksum_digits()
    }

    fn max_chain_steps(&self) -> usize {
        let w = self.winternitz_w();
        assert!(w >= 2, "Winternitz parameter w must be >= 2");
        w - 1
    }

    fn log_w(&self) -> usize {
        let w = self.winternitz_w();
        assert!(
            w.is_power_of_two(),
            "This simplified impl requires Winternitz w to be a power of two"
        );
        w.trailing_zeros() as usize
    }

    fn message_digits(&self) -> usize {
        let log_w = self.log_w();
        (MESSAGE_LEN * 8).div_ceil(log_w)
    }

    fn checksum_digits(&self) -> usize {
        let w = self.winternitz_w();
        let mut max_checksum = self.message_digits() * (w - 1);
        let mut digits = 0;
        while max_checksum > 0 {
            digits += 1;
            max_checksum /= w;
        }
        digits.max(1)
    }

    fn chain_hash(&self, start: &HashFe<Self::Field>, steps: usize) -> HashFe<Self::Field> {
        let mut cur = *start;
        for _ in 0..steps {
            self.hash_one_in_place(&mut cur);
        }
        cur
    }

    fn encode_chain_lengths(&self, message: &[u8; MESSAGE_LEN]) -> Vec<usize> {
        let w = self.winternitz_w();
        let mut acc = BigUint::from_bytes_le(message);
        let base_u64 = w as u64;
        let base = BigUint::from(base_u64);
        let len1 = self.message_digits();
        let len2 = self.checksum_digits();

        // 1) Encode the message into base-w digits.
        // These digits directly determine per-chain signing steps in WOTS.
        let mut digits = Vec::with_capacity(len1 + len2);
        for _ in 0..len1 {
            let digit = &acc % &base;
            acc /= &base;
            let d: u64 = digit.try_into().unwrap();
            digits.push(d as usize);
        }
        assert!(acc == BigUint::ZERO, "message was not fully decomposed");

        // 2) Append checksum digits (also in base-w).
        // The checksum compensates for small message digits, which is the key WOTS idea:
        // it makes total chain budget constrained by parameters instead of being purely
        // driven by the message digit sum.
        let checksum = digits.iter().map(|d| w - 1 - d).sum::<usize>();
        let mut checksum_acc = BigUint::from(checksum as u64);
        for _ in 0..len2 {
            let digit = &checksum_acc % &base;
            checksum_acc /= &base;
            let d: u64 = digit.try_into().unwrap();
            digits.push(d as usize);
        }

        digits
    }

    fn derive_leaf_from_secret(&self, secret: &HashFe<Self::Field>) -> HashFe<Self::Field> {
        let base = self.chain_hash(secret, self.max_chain_steps());
        let mut acc = base;
        for _ in 1..self.chunk_count() {
            self.hash_pair_in_place(&mut acc, &base);
        }
        acc
    }

    fn compute_root_from_leaf(
        &self,
        leaf: &HashFe<Self::Field>,
        leaf_index: usize,
        auth_path: &[HashFe<Self::Field>],
    ) -> HashFe<Self::Field> {
        let mut node = *leaf;
        for (level, sibling) in auth_path.iter().enumerate() {
            let bit = (leaf_index >> level) & 1;
            if bit == 0 {
                self.hash_pair_in_place(&mut node, sibling);
            } else {
                let mut parent = *sibling;
                self.hash_pair_in_place(&mut parent, &node);
                node = parent;
            }
        }
        node
    }

    fn compute_merkle_root(&self, leaves: &[HashFe<Self::Field>]) -> HashFe<Self::Field> {
        assert!(
            !leaves.is_empty(),
            "Merkle tree must have at least one leaf"
        );
        let mut level_nodes: Cow<'_, [HashFe<Self::Field>]> = Cow::Borrowed(leaves);
        while level_nodes.len() > 1 {
            let mut next = Vec::with_capacity(level_nodes.len() / 2);
            for pair in level_nodes.chunks_exact(2) {
                let mut parent = pair[0];
                self.hash_pair_in_place(&mut parent, &pair[1]);
                next.push(parent);
            }
            level_nodes = Cow::Owned(next);
        }
        level_nodes[0]
    }

    fn compute_auth_path(
        &self,
        leaves: &[HashFe<Self::Field>],
        leaf_index: usize,
    ) -> Vec<HashFe<Self::Field>> {
        assert!(
            !leaves.is_empty(),
            "Merkle tree must have at least one leaf"
        );
        assert!(leaf_index < leaves.len(), "leaf_index out of range");

        let mut path = Vec::with_capacity(leaves.len().ilog2() as usize);
        let mut idx = leaf_index;
        let mut level_nodes: Cow<'_, [HashFe<Self::Field>]> = Cow::Borrowed(leaves);
        while level_nodes.len() > 1 {
            let sibling_idx = if idx % 2 == 0 { idx + 1 } else { idx - 1 };
            path.push(level_nodes[sibling_idx]);

            let mut next = Vec::with_capacity(level_nodes.len() / 2);
            for pair in level_nodes.chunks_exact(2) {
                let mut parent = pair[0];
                self.hash_pair_in_place(&mut parent, &pair[1]);
                next.push(parent);
            }
            idx /= 2;
            level_nodes = Cow::Owned(next);
        }
        path
    }
}

pub struct XmssPublicKey<FF: Field> {
    root: HashFe<FF>, // Root of the Merkle tree
}

pub struct XmssSecretKey<FF: Field> {
    height: usize, // 2-arity merkle tree height, determines the number of signatures (2^height)
    signing_keys: Vec<HashFe<FF>>, // One-time secret keys per leaf
    leaf_public_keys: Vec<HashFe<FF>>, // Cached leaf public keys (Merkle leaves)
}

impl<FF: Field> XmssSecretKey<FF> {
    pub fn height(&self) -> usize {
        self.height
    }

    pub fn signing_key_at(&self, index: usize) -> &HashFe<FF> {
        &self.signing_keys[index]
    }

    pub fn leaf_count(&self) -> usize {
        self.signing_keys.len()
    }

    pub fn leaf_public_keys(&self) -> &[HashFe<FF>] {
        &self.leaf_public_keys
    }
}

#[derive(Clone)]
pub struct PoseidonXmssEngine {
    poseidon16: Poseidon2KoalaBear<16>,
    winternitz_w: usize,
}

impl Default for PoseidonXmssEngine {
    fn default() -> Self {
        Self::new(DEFAULT_WINTERNITZ_W)
    }
}

impl PoseidonXmssEngine {
    pub fn new(winternitz_w: usize) -> Self {
        Self {
            poseidon16: default_koalabear_poseidon2_16(),
            winternitz_w,
        }
    }
}

impl XmssHashOps for PoseidonXmssEngine {
    type Field = KoalaBear;

    fn winternitz_w(&self) -> usize {
        self.winternitz_w
    }

    fn hash_one_in_place(&self, input: &mut HashFe<Self::Field>) {
        // simple wrapper around the Poseidon permutation, we use a 16-element state and only fill the first MESSAGE_LEN_FE elements with the input, the rest are zero. This is not a secure way to use Poseidon and is just for demonstration purposes.
        let mut state = [Self::Field::ZERO; 16];
        state[..MESSAGE_LEN_FE].copy_from_slice(input);
        self.poseidon16.permute_mut(&mut state);
        input.copy_from_slice(&state[..MESSAGE_LEN_FE]);
    }

    fn hash_pair_in_place(&self, left: &mut HashFe<Self::Field>, right: &HashFe<Self::Field>) {
        let mut state = [Self::Field::ZERO; 16];
        state[..MESSAGE_LEN_FE].copy_from_slice(left);
        state[MESSAGE_LEN_FE..(2 * MESSAGE_LEN_FE)].copy_from_slice(right);
        self.poseidon16.permute_mut(&mut state);
        left.copy_from_slice(&state[..MESSAGE_LEN_FE]);
    }
}

pub struct XmssSignature<FF: Field> {
    pub auth_path: Vec<HashFe<FF>>, // Authentication path for the private key used for signing (siblings only)
    pub leaf_index: usize,          // Index of the leaf used for signings
    pub chunks_wots: Vec<HashFe<FF>>, // WOTS signatures for each chunk
    pub msg: [u8; MESSAGE_LEN],
}

pub trait XmssSignatureScheme {
    type Engine: XmssHashOps + Sync;
    type Field: Field + PrimeCharacteristicRing + PrimeField64 + Copy + Eq + Send + Sync;
    type PublicKey;
    type SecretKey;
    type Signature;
    fn keygen<R: Rng>(
        rng: &mut R,
        engine: &Self::Engine,
        height: usize,
    ) -> (Self::PublicKey, Self::SecretKey);
    fn sign(
        message: &[u8; MESSAGE_LEN], // message is the hash of the original arbitrary message
        secret_key: &Self::SecretKey,
        leaf_index: usize,
        engine: &Self::Engine,
    ) -> Self::Signature;
    fn verify(
        signature: &Self::Signature,
        public_key: &Self::PublicKey,
        engine: &Self::Engine,
    ) -> bool;
}

pub struct SimpleXmssScheme;

impl XmssSignatureScheme for SimpleXmssScheme {
    type Engine = PoseidonXmssEngine;
    type Field = <Self::Engine as XmssHashOps>::Field;
    type PublicKey = XmssPublicKey<Self::Field>;
    type SecretKey = XmssSecretKey<Self::Field>;
    type Signature = XmssSignature<Self::Field>;

    fn keygen<R: Rng>(
        rng: &mut R,
        engine: &Self::Engine,
        height: usize,
    ) -> (Self::PublicKey, Self::SecretKey) {
        engine.keygen(rng, height)
    }

    fn sign(
        message: &[u8; MESSAGE_LEN],
        secret_key: &Self::SecretKey,
        leaf_index: usize,
        engine: &Self::Engine,
    ) -> Self::Signature {
        debug_assert_eq!(secret_key.leaf_count(), 1usize << secret_key.height());
        XmssSignature::sign(message, secret_key, leaf_index, engine)
    }

    fn verify(
        signature: &Self::Signature,
        public_key: &Self::PublicKey,
        engine: &Self::Engine,
    ) -> bool {
        signature.verify(public_key, engine)
    }
}

pub fn keygen<R: Rng, E: XmssHashOps>(
    rng: &mut R,
    engine: &E,
    height: usize,
) -> (XmssPublicKey<E::Field>, XmssSecretKey<E::Field>) {
    engine.keygen(rng, height)
}

impl<FF: Field + PrimeCharacteristicRing + PrimeField64 + Copy + Eq + Send + Sync>
    XmssSignature<FF>
{
    pub fn sign(
        message: &[u8; MESSAGE_LEN],
        secret_key_tree: &XmssSecretKey<FF>,
        leaf_index: usize,
        engine: &(impl XmssHashOps<Field = FF> + Sync),
    ) -> Self {
        let started = Instant::now();
        // Implement the signing logic, this will involve:
        // 1. Encoding the message into field elements
        // 2. Split the message into chunks according to CHUNK_WIDTH bits and generate WOTS signatures for each chunk (hash the secret key for x times, where x is the value of the chunk)
        // 3. Construct the authentication path for the leaf corresponding to the secret key
        // 4. Return the XMSSSignature struct with the generated data
        assert!(
            leaf_index < secret_key_tree.leaf_count(),
            "leaf_index out of range"
        );
        let secret_key = secret_key_tree.signing_key_at(leaf_index);
        let chain_lengths = engine.encode_chain_lengths(message);

        let chunks_wots = chain_lengths
            .par_iter()
            .map(|&x| engine.chain_hash(secret_key, x))
            .collect::<Vec<_>>();

        let auth_path = engine.compute_auth_path(secret_key_tree.leaf_public_keys(), leaf_index);

        let sig = Self {
            auth_path,
            leaf_index,
            chunks_wots,
            msg: *message,
        };
        println!("XMSS sign took: {:?}", started.elapsed());
        sig
    }

    pub fn verify(
        &self,
        public_key: &XmssPublicKey<FF>,
        engine: &(impl XmssHashOps<Field = FF> + Sync),
    ) -> bool {
        let started = Instant::now();
        // Implement the verification logic， this will involve:
        // 1. for each chunk, verify the chunks_wots by hashing the secret key for (CHUNK_WIDTH - x) times, where x is the value of the chunk, and compare with the corresponding pubkey in the authentication path
        // 2. Reconstruct the root of the Merkle tree using the authentication path and compare with the public key's root
        let ok = {
            let chain_lengths = engine.encode_chain_lengths(&self.msg);
            if chain_lengths.len() != self.chunks_wots.len() {
                false
            } else {
                let max_chain = engine.max_chain_steps();
                let recovered_chunks = chain_lengths
                    .par_iter()
                    .zip(self.chunks_wots.par_iter())
                    .map(|(&x, sig_chunk)| engine.chain_hash(sig_chunk, max_chain - x))
                    .collect::<Vec<_>>();

                let Some(base_pk_chunk) = recovered_chunks.first().copied() else {
                    println!("XMSS verify took: {:?}", started.elapsed());
                    return false;
                };

                if !recovered_chunks
                    .par_iter()
                    .all(|chunk| *chunk == base_pk_chunk)
                {
                    false
                } else {
                    let mut leaf_pub = base_pk_chunk;
                    for _ in 1..engine.chunk_count() {
                        engine.hash_pair_in_place(&mut leaf_pub, &base_pk_chunk);
                    }
                    let root =
                        engine.compute_root_from_leaf(&leaf_pub, self.leaf_index, &self.auth_path);
                    root == public_key.root
                }
            }
        };
        println!("XMSS verify took: {:?}", started.elapsed());
        ok
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::env;

    #[test]
    fn test_signature_verification_works() {
        type Scheme = SimpleXmssScheme;
        let mut rng = rand::rng();
        let engine = PoseidonXmssEngine::default();
        let (pk, sk) = keygen(&mut rng, &engine, TREE_HEIGHT);
        let message = rng.random();
        let signature = Scheme::sign(&message, &sk, 0, &engine);
        assert!(Scheme::verify(&signature, &pk, &engine));
    }

    #[test]
    fn test_signature_verification_fails() {
        type Scheme = SimpleXmssScheme;
        let mut rng = rand::rng();
        let engine = PoseidonXmssEngine::default();
        let (pk, sk) = Scheme::keygen(&mut rng, &engine, TREE_HEIGHT);
        let message = rng.random();
        let mut signature = Scheme::sign(&message, &sk, 0, &engine);
        signature.chunks_wots[0][0] += <PoseidonXmssEngine as XmssHashOps>::Field::ONE; // Corrupt the signature deterministically
        assert!(!Scheme::verify(&signature, &pk, &engine));
    }

    fn benchmark_params_from_env() -> (usize, usize) {
        let height = env::var("XMSS_H")
            .ok()
            .and_then(|v| v.parse::<usize>().ok())
            .unwrap_or(TREE_HEIGHT);
        let width = env::var("XMSS_W")
            .ok()
            .and_then(|v| v.parse::<usize>().ok())
            .unwrap_or(DEFAULT_WINTERNITZ_W);
        (height, width)
    }

    // XMSS_H=12 XMSS_W=2 cargo test --release test_xmss_benchmark_params -- --nocapture
    #[test]
    fn test_xmss_benchmark_params() {
        let (height, width) = benchmark_params_from_env();
        type Scheme = SimpleXmssScheme;
        let mut rng = rand::rng();
        let engine = PoseidonXmssEngine::new(width);
        let (pk, sk) = Scheme::keygen(&mut rng, &engine, height);
        let message = rng.random();
        let signature = Scheme::sign(&message, &sk, 0, &engine);
        assert!(Scheme::verify(&signature, &pk, &engine));
    }

    // cargo test test_wots_chain_length_breakdown -- --nocapture
    #[test]
    fn test_wots_chain_length_breakdown() {
        fn analyze(engine: &PoseidonXmssEngine, label: &str, msg: [u8; MESSAGE_LEN]) {
            let len1 = engine.message_digits();
            let chains = engine.encode_chain_lengths(&msg);
            let msg_sum: usize = chains[..len1].iter().copied().sum();
            let cks_sum: usize = chains[len1..].iter().copied().sum();
            let sign_total = msg_sum + cks_sum;
            let max_chain = engine.max_chain_steps();
            let chunk_count = chains.len();
            let verify_total = max_chain * chunk_count - sign_total;
            let budget = max_chain * chunk_count;
            println!(
                "{label}: msg_sum={msg_sum}, cks_sum={cks_sum}, sign_total={sign_total}, verify_total={verify_total}, max_chain_steps*chunk_count={budget}"
            );
        }

        let engine = PoseidonXmssEngine::default();
        analyze(&engine, "all-zero", [0u8; MESSAGE_LEN]);
        analyze(&engine, "all-ff", [0xffu8; MESSAGE_LEN]);
        analyze(
            &engine,
            "pattern",
            std::array::from_fn(|i| (i as u8).wrapping_mul(17)),
        );
    }
}
