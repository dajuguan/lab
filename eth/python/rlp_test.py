def rlp_encode(input):
    if isinstance(input,str):
        if len(input) == 1 and ord(input) < 0x80:
            return input
        return encode_length(len(input), 0x80) + input
    elif isinstance(input, list):
        output = ''
        for item in input:
            output += rlp_encode(item)
        return encode_length(len(output), 0xc0) + output

def encode_length(L, offset):
    if L < 56:
         return chr(L + offset)
    elif L < 256**8:
         BL = to_binary(L)
         return chr(len(BL) + offset + 55) + BL
    raise Exception("input too long")

def to_binary(x):
    if x == 0:
        return ''
    return to_binary(int(x / 256)) + chr(x % 256)


########### Transaction
import rlp
from dataclasses import dataclass
from typing import Optional
from eth_utils import keccak, to_bytes, to_checksum_address
from rlp.sedes import Binary, big_endian_int, binary
class Transaction(rlp.Serializable):
    fields = [
        ("nonce", big_endian_int),
        ("gas_price", big_endian_int),
        ("gas", big_endian_int),
        ("to", Binary.fixed_length(20, allow_empty=True)),
        ("value", big_endian_int),
        ("data", binary),
        ("v", big_endian_int),
        ("r", big_endian_int),
        ("s", big_endian_int),
    ]

    @classmethod
    def deserialize(cls, serial, **kwargs):
        tx = super().deserialize(serial, **kwargs)
        # from_ = w3.eth.account.recover_transaction(raw_tx)
        setattr(tx, "_to", to_checksum_address(tx.to))
        return tx

import unittest
class TestRPL(unittest.TestCase):
    def testRLP(self):
        data_str = "dog"
        result = rlp_encode(data_str)
        result = bytes(result, encoding="raw_unicode_escape")
        assert(result == b'\x83dog')

        data_list = [ "cat", "dog" ] 
        result = rlp_encode(data_list)
        result = bytes(result, encoding="raw_unicode_escape")
        print("result:", result[0], 0xc8, len(result))
        assert(result == b'\xc8\x83cat\x83dog')
    def testRLPDecode(self):
        encoded = rlp.encode("dog")
        print("encoded dog:", encoded)
        decoded = rlp.decode(encoded)
        print("decoded dog:", decoded)
    
    def testRLPDecodeTxTypeLegacyTransaction (self):
        # example from https://docs.klaytn.foundation/docs/learn/transactions/basic/
        tx = "f8668204d219830f4240947b65b75d204abed71587c9e519a89277766ee1d00a843132333425a0b2a5a15550ec298dc7dddde3774429ed75f864c82caeb5ee24399649ad731be9a029da1014d16f2011b3307f7bbe1035b6e699a4204fc416c763def6cefd976567"
        txBytes = bytes.fromhex(tx)
        decoded = rlp.decode(txBytes, Transaction)
        print("decoded tx:", decoded)


if __name__ == "__main__":
    unittest.main()