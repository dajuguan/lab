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
        assert(result == b'\xc8\x83cat\x83dog')



if __name__ == "__main__":
    unittest.main()