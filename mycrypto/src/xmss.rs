/* eXtended Merkle Signature Scheme (XMSS) implementation in Rust.
This module provides a basic implementation of the XMSS signature scheme, which is a hash-based signature.
It's not secure for production use and is intended for educational purposes only.
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
pub const DEFAULT_CHUNK_WIDTH: usize = 4; // number of bits per chunk for WOTS signatures

type HashFe<FF> = [FF; MESSAGE_LEN_FE];

pub trait XmssHashOps {
    type Field: Field + PrimeCharacteristicRing + PrimeField64 + Copy + Eq + Send + Sync;

    fn chunk_width(&self) -> usize;
    fn hash_one_in_place(&self, input: &mut HashFe<Self::Field>);
    fn hash_pair_in_place(&self, left: &mut HashFe<Self::Field>, right: &HashFe<Self::Field>);

    fn keygen<const HEIGHT: usize, R: Rng>(
        &self,
        rng: &mut R,
    ) -> (
        XmssPublicKey<Self::Field>,
        XmssSecretKey<HEIGHT, Self::Field>,
    ) {
        let leaf_count = 1usize << HEIGHT;
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
            signing_keys,
            leaf_public_keys,
        };
        let pk = XmssPublicKey {
            root: self.compute_merkle_root(sk.leaf_public_keys()),
        };
        (pk, sk)
    }

    fn chunk_count(&self) -> usize {
        (MESSAGE_LEN * 8).div_ceil(self.chunk_width())
    }

    fn max_chain_steps(&self) -> usize {
        let width = self.chunk_width();
        assert!(width > 0 && width < usize::BITS as usize);
        (1usize << width) - 1
    }

    fn chain_hash(&self, start: &HashFe<Self::Field>, steps: usize) -> HashFe<Self::Field> {
        let mut cur = *start;
        for _ in 0..steps {
            self.hash_one_in_place(&mut cur);
        }
        cur
    }

    fn encode_chunks(&self, message: &[u8; MESSAGE_LEN]) -> Vec<usize> {
        assert!(
            self.chunk_width() <= 63,
            "chunk width too large for u64 conversion"
        );

        let mut acc = BigUint::from_bytes_le(message);
        let base_u64 = 1u64 << self.chunk_width();
        let base = BigUint::from(base_u64);
        let chunk_count = (MESSAGE_LEN * 8).div_ceil(self.chunk_width());

        let mut chunks = Vec::with_capacity(chunk_count);
        for _ in 0..chunk_count {
            let digit = &acc % &base;
            acc /= &base;
            let d: u64 = digit.try_into().unwrap();
            chunks.push(d as usize);
        }
        assert!(acc == BigUint::ZERO, "message was not fully decomposed");
        chunks
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

pub struct XmssSecretKey<const HEIGHT: usize, FF: Field> {
    signing_keys: Vec<HashFe<FF>>,     // One-time secret keys per leaf
    leaf_public_keys: Vec<HashFe<FF>>, // Cached leaf public keys (Merkle leaves)
}

impl<const HEIGHT: usize, FF: Field> XmssSecretKey<HEIGHT, FF> {
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
    chunk_width: usize,
}

impl Default for PoseidonXmssEngine {
    fn default() -> Self {
        Self {
            poseidon16: default_koalabear_poseidon2_16(),
            chunk_width: DEFAULT_CHUNK_WIDTH,
        }
    }
}

impl XmssHashOps for PoseidonXmssEngine {
    type Field = KoalaBear;

    fn chunk_width(&self) -> usize {
        self.chunk_width
    }

    fn hash_one_in_place(&self, input: &mut HashFe<Self::Field>) {
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
    const HEIGHT: usize;

    fn keygen<R: Rng>(rng: &mut R, engine: &Self::Engine) -> (Self::PublicKey, Self::SecretKey);
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

pub struct SimpleXmssScheme<const HEIGHT: usize>;

impl<const HEIGHT: usize> XmssSignatureScheme for SimpleXmssScheme<HEIGHT> {
    type Engine = PoseidonXmssEngine;
    type Field = <Self::Engine as XmssHashOps>::Field;
    type PublicKey = XmssPublicKey<Self::Field>;
    type SecretKey = XmssSecretKey<HEIGHT, Self::Field>;
    type Signature = XmssSignature<Self::Field>;
    const HEIGHT: usize = HEIGHT;

    fn keygen<R: Rng>(rng: &mut R, engine: &Self::Engine) -> (Self::PublicKey, Self::SecretKey) {
        engine.keygen::<HEIGHT, _>(rng)
    }

    fn sign(
        message: &[u8; MESSAGE_LEN],
        secret_key: &Self::SecretKey,
        leaf_index: usize,
        engine: &Self::Engine,
    ) -> Self::Signature {
        debug_assert_eq!(secret_key.leaf_count(), 1usize << Self::HEIGHT);
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

impl<FF: Field + PrimeCharacteristicRing + PrimeField64 + Copy + Eq + Send + Sync>
    XmssSignature<FF>
{
    pub fn sign<const HEIGHT: usize>(
        message: &[u8; MESSAGE_LEN],
        secret_key_tree: &XmssSecretKey<HEIGHT, FF>,
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
        let chunk_values = engine.encode_chunks(message);

        let chunks_wots = chunk_values
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
            let chunk_values = engine.encode_chunks(&self.msg);
            if chunk_values.len() != self.chunks_wots.len() {
                false
            } else {
                let max_chain = engine.max_chain_steps();
                let recovered_chunks = chunk_values
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

    #[test]
    fn test_signature_verification_works() {
        type Scheme = SimpleXmssScheme<TREE_HEIGHT>;
        let mut rng = rand::rng();
        let engine = PoseidonXmssEngine::default();
        let (pk, sk) = Scheme::keygen(&mut rng, &engine);
        let message = rng.random();
        let signature = Scheme::sign(&message, &sk, 0, &engine);
        assert!(Scheme::verify(&signature, &pk, &engine));
    }

    #[test]
    fn test_signature_verification_fails() {
        type Scheme = SimpleXmssScheme<TREE_HEIGHT>;
        let mut rng = rand::rng();
        let engine = PoseidonXmssEngine::default();
        let (pk, sk) = Scheme::keygen(&mut rng, &engine);
        let message = rng.random();
        let mut signature = Scheme::sign(&message, &sk, 0, &engine);
        signature.chunks_wots[0][0] += <PoseidonXmssEngine as XmssHashOps>::Field::ONE; // Corrupt the signature deterministically
        assert!(!Scheme::verify(&signature, &pk, &engine));
    }
}
