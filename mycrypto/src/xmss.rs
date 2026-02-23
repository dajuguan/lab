/* eXtended Merkle Signature Scheme (XMSS) implementation in Rust.
This module provides a basic implementation of the XMSS signature scheme, which is a hash-based signature.
It's not secure for production use and is intended for educational purposes only.
 */

use num_bigint::BigUint;
use p3_field::PrimeCharacteristicRing;
use p3_field::PrimeField64;
use p3_koala_bear::{KoalaBear, Poseidon2KoalaBear, default_koalabear_poseidon2_16};
use p3_symmetric::Permutation;
use rand::Rng;
use std::borrow::Cow;

pub const MESSAGE_LEN: usize = 32; // message length in bytes
pub const MESSAGE_LEN_FE: usize = 5; // message field elements
pub const TREE_HEIGHT: usize = 10; // height of the Merkle tree
pub const DEFAULT_CHUNK_WIDTH: usize = 4; // number of bits per chunk for WOTS signatures

type F = KoalaBear;
type HashFe = [F; MESSAGE_LEN_FE];

pub trait XmssEngineOps {
    fn chunk_width(&self) -> usize;
    fn hash_one_in_place(&self, input: &mut HashFe);
    fn hash_pair_in_place(&self, left: &mut HashFe, right: &HashFe);

    fn chunk_count(&self) -> usize {
        (MESSAGE_LEN * 8).div_ceil(self.chunk_width())
    }

    fn max_chain_steps(&self) -> usize {
        let width = self.chunk_width();
        assert!(width > 0 && width < usize::BITS as usize);
        (1usize << width) - 1
    }

    fn chain_hash(&self, start: &HashFe, steps: usize) -> HashFe {
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

    fn derive_leaf_from_secret(&self, secret: &HashFe) -> HashFe {
        let base = self.chain_hash(secret, self.max_chain_steps());
        let mut acc = base;
        for _ in 1..self.chunk_count() {
            self.hash_pair_in_place(&mut acc, &base);
        }
        acc
    }

    fn compute_root_from_leaf(
        &self,
        leaf: &HashFe,
        leaf_index: usize,
        auth_path: &[HashFe],
    ) -> HashFe {
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

    fn compute_merkle_root(&self, leaves: &[HashFe]) -> HashFe {
        assert!(
            !leaves.is_empty(),
            "Merkle tree must have at least one leaf"
        );
        let mut level_nodes: Cow<'_, [HashFe]> = Cow::Borrowed(leaves);
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

    fn compute_auth_path(&self, leaves: &[HashFe], leaf_index: usize) -> Vec<HashFe> {
        assert!(
            !leaves.is_empty(),
            "Merkle tree must have at least one leaf"
        );
        assert!(leaf_index < leaves.len(), "leaf_index out of range");

        let mut path = Vec::with_capacity(leaves.len().ilog2() as usize);
        let mut idx = leaf_index;
        let mut level_nodes: Cow<'_, [HashFe]> = Cow::Borrowed(leaves);
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

pub struct PubKey {
    root: HashFe, // Root of the Merkle tree
}

pub struct SecretKeyTree<const HEIGHT: usize> {
    leafs: Vec<HashFe>,      // Secret key for signing
    cached_pks: Vec<HashFe>, // Cached public keys for each leaf (the leaf nodes of the Merkle tree)
}

impl<const HEIGHT: usize> SecretKeyTree<HEIGHT> {
    pub fn sk_at_index(&self, index: usize) -> &HashFe {
        &self.leafs[index]
    }

    pub fn leaf_count(&self) -> usize {
        self.leafs.len()
    }

    pub fn cached_pks(&self) -> &[HashFe] {
        &self.cached_pks
    }
}

#[derive(Clone)]
pub struct XmssEngine {
    poseidon16: Poseidon2KoalaBear<16>,
    chunk_width: usize,
}

impl Default for XmssEngine {
    fn default() -> Self {
        Self {
            poseidon16: default_koalabear_poseidon2_16(),
            chunk_width: DEFAULT_CHUNK_WIDTH,
        }
    }
}

impl XmssEngine {
    fn key_gen<const HEIGHT: usize, R: Rng>(&self, rng: &mut R) -> (PubKey, SecretKeyTree<HEIGHT>) {
        let leaf_count = 1usize << HEIGHT;
        let mut leafs = Vec::with_capacity(leaf_count);
        for _ in 0..leaf_count {
            // simulate generating secret keys by random field elements, in practice this should be done with a secure PRNG and proper seeding
            leafs.push(std::array::from_fn(|_| {
                F::from_u64(rng.random::<u64>() % F::ORDER_U64)
            }));
        }
        let mut cached_pks = Vec::with_capacity(leaf_count);
        for secret in &leafs {
            cached_pks.push(self.derive_leaf_from_secret(secret));
        }
        let sk = SecretKeyTree { leafs, cached_pks };

        let pk = PubKey {
            root: self.compute_merkle_root(sk.cached_pks()),
        };

        (pk, sk)
    }
}

impl XmssEngineOps for XmssEngine {
    fn chunk_width(&self) -> usize {
        self.chunk_width
    }

    fn hash_one_in_place(&self, input: &mut HashFe) {
        let mut state = [F::ZERO; 16];
        state[..MESSAGE_LEN_FE].copy_from_slice(input);
        self.poseidon16.permute_mut(&mut state);
        input.copy_from_slice(&state[..MESSAGE_LEN_FE]);
    }

    fn hash_pair_in_place(&self, left: &mut HashFe, right: &HashFe) {
        let mut state = [F::ZERO; 16];
        state[..MESSAGE_LEN_FE].copy_from_slice(left);
        state[MESSAGE_LEN_FE..(2 * MESSAGE_LEN_FE)].copy_from_slice(right);
        self.poseidon16.permute_mut(&mut state);
        left.copy_from_slice(&state[..MESSAGE_LEN_FE]);
    }
}

pub fn key_gen<const HEIGHT: usize, R: Rng>(rng: &mut R) -> (PubKey, SecretKeyTree<HEIGHT>) {
    XmssEngine::default().key_gen::<HEIGHT, _>(rng)
}

pub struct XMSSSignature {
    pub auth_path: Vec<HashFe>, // Authentication path for the private key used for signing (siblings only)
    pub leaf_index: usize,      // Index of the leaf used for signings
    pub chunks_wots: Vec<HashFe>, // WOTS signatures for each chunk
    pub msg: [u8; MESSAGE_LEN],
}

impl XMSSSignature {
    pub fn sign<const HEIGHT: usize>(
        message: &[u8; MESSAGE_LEN],
        secret_key_tree: &SecretKeyTree<HEIGHT>,
        leaf_index: usize,
        engine: &impl XmssEngineOps,
    ) -> Self {
        // Implement the signing logic, this will involve:
        // 1. Encoding the message into field elements
        // 2. Split the message into chunks according to CHUNK_WIDTH bits and generate WOTS signatures for each chunk (hash the secret key for x times, where x is the value of the chunk)
        // 3. Construct the authentication path for the leaf corresponding to the secret key
        // 4. Return the XMSSSignature struct with the generated data
        assert!(
            leaf_index < secret_key_tree.leaf_count(),
            "leaf_index out of range"
        );
        let secret_key = secret_key_tree.sk_at_index(leaf_index);
        let chunk_values = engine.encode_chunks(message);

        let mut chunks_wots = Vec::with_capacity(chunk_values.len());
        for &x in &chunk_values {
            chunks_wots.push(engine.chain_hash(secret_key, x));
        }

        let auth_path = engine.compute_auth_path(secret_key_tree.cached_pks(), leaf_index);

        Self {
            auth_path,
            leaf_index,
            chunks_wots,
            msg: *message,
        }
    }

    pub fn verify(&self, public_key: &PubKey, engine: &impl XmssEngineOps) -> bool {
        // Implement the verification logic， this will involve:
        // 1. for each chunk, verify the chunks_wots by hashing the secret key for (CHUNK_WIDTH - x) times, where x is the value of the chunk, and compare with the corresponding pubkey in the authentication path
        // 2. Reconstruct the root of the Merkle tree using the authentication path and compare with the public key's root
        let chunk_values = engine.encode_chunks(&self.msg);
        if chunk_values.len() != self.chunks_wots.len() {
            return false;
        }
        let max_chain = engine.max_chain_steps();

        let mut recovered_iter = chunk_values
            .iter()
            .zip(self.chunks_wots.iter())
            .map(|(&x, sig_chunk)| engine.chain_hash(sig_chunk, max_chain - x));
        let Some(base_pk_chunk) = recovered_iter.next() else {
            return false;
        };

        for recovered in recovered_iter {
            if recovered != base_pk_chunk {
                return false;
            }
        }

        let mut leaf_pub = base_pk_chunk;
        for _ in 1..engine.chunk_count() {
            engine.hash_pair_in_place(&mut leaf_pub, &base_pk_chunk);
        }
        let root = engine.compute_root_from_leaf(&leaf_pub, self.leaf_index, &self.auth_path);
        root == public_key.root
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_signature_verification_works() {
        let mut rng = rand::rng();
        let (pk, sk) = key_gen::<TREE_HEIGHT, _>(&mut rng);
        let message = rng.random();
        let engine = XmssEngine::default();
        let signature = XMSSSignature::sign(&message, &sk, 0, &engine);
        assert!(signature.verify(&pk, &engine));
    }

    #[test]
    fn test_signature_verification_fails() {
        let mut rng = rand::rng();
        let (pk, sk) = key_gen::<TREE_HEIGHT, _>(&mut rng);
        let message = rng.random();
        let engine = XmssEngine::default();
        let mut signature = XMSSSignature::sign(&message, &sk, 0, &engine);
        signature.chunks_wots[0] = signature.chunks_wots[1]; // Corrupt the signature
        assert!(!signature.verify(&pk, &engine));
    }
}
