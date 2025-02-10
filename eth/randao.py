from utils import bls 
from typing import List
from dataclasses import dataclass
import hashlib



""" BLS simple usage
private_key = 5566
public_key = bls.SkToPk(private_key)
message = b'\xab' * 32  # The message to be signed
# Signing
signature = bls.Sign(private_key, message)
# Verifying
assert bls.Verify(public_key, message, signature)
 """

EPOCHS_PER_HISTORICAL_VECTOR = 8192
DOMAIN_RANDAO = "0x02000000"

BLSSignature =  str

@dataclass
class BeaconBlock:
    slot: int = 0

@dataclass
class BeaconBlockBody:
    randao_reveal: str

@dataclass
class Proposer:
    pubkey: str

@dataclass
class BeaconState:
    validators: List[Proposer]
    randao_mixes: List[int]
    epoch: int = 0



def get_epoch_signature(state: BeaconState, block: BeaconBlock, privkey: int) -> BLSSignature:
    domain = get_domain(state, DOMAIN_RANDAO)
    signing_root = compute_signing_root(compute_epoch_at_slot(block.slot), domain)
    return bls.Sign(privkey, signing_root)

# python's builtin hash is not determinstic, so use hashlib instead
def sha256Hash(body: str):
    m = hashlib.sha256()
    m.update(body)
    return int(m.hexdigest(), base=16)

def process_randao(state: BeaconState, body: BeaconBlockBody) -> None:
    epoch = get_current_epoch(state)
    # Verify RANDAO reveal
    proposer = state.validators[get_beacon_proposer_index(state)]
    signing_root = compute_signing_root(epoch, get_domain(state, DOMAIN_RANDAO))
    assert bls.Verify(proposer.pubkey, signing_root, body.randao_reveal)
    # Mix in RANDAO reveal
    mix = get_randao_mix(state, epoch) ^ sha256Hash(body.randao_reveal)
    state.randao_mixes[epoch % EPOCHS_PER_HISTORICAL_VECTOR] = mix

# mock
def get_current_epoch(s: BeaconState):
    return 0

# mock
def get_domain(state, domain):
    return DOMAIN_RANDAO

# mock
def compute_epoch_at_slot(slot):
    return 1

# mock 
def compute_signing_root(epoch, domain):
    return b'\xab' * 32

#mock
def get_beacon_proposer_index(state):
    return 0

def get_randao_mix(s: BeaconState, epoch: int) -> int:
    return s.randao_mixes[epoch]


import unittest
class TestRandao(unittest.TestCase):
    def test_upper(self):
        privkey = 5566
        proposer = Proposer(pubkey=bls.SkToPk(privkey))
        state = BeaconState(validators=[proposer], randao_mixes=[11111])
        print("randao prev:", state.randao_mixes[0])
        block = BeaconBlock()
        sig = get_epoch_signature(state, block, privkey)
        print("randao sig:", sig)
        # randao
        body = BeaconBlockBody(randao_reveal=sig)
        process_randao(state, body)
        print("randao after:", state.randao_mixes[0])

if __name__ == '__main__':
    unittest.main()