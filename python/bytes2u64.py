# u64 to little endian bytes
import re
import struct
import os
a = "--private 14625441452057167097:i64 --private 441:i64 --private 0:i64 --private 0:i64 --private 144115188084244480:i64 --private 17592186044416:i64 --private 0:i64 --private 0:i64 --private 2:i64 --private 0:i64 --private 281474976710656:i64 --private 72057594037928224:i64 --private 0:i64 --private 144115188075855872:i64 --private 4398046511104:i64 --private 2048:i64 --private 0:i64 --private 288230376151711744:i64 --private 562949953421312:i64 --private 36033195065475072:i64 --private 0:i64 --private 1152921504606846992:i64 --private 0:i64 --private 72057594037927936:i64 --private 0:i64 --private 0:i64 --private 72057594037927936:i64 --private 274877906944:i64 --private 0:i64 --private 8192:i64 --private 0:i64 --private 0:i64 --private 0:i64 --private 142172368092004352:i64 --private 10663670667014018268:i64 --private 15598333267600830878:i64 --private 4825637194728734969:i64 --private 11537926770794296992:i64 --private 8941585237026987872:i64 --private 1060144843738714138:i64 --private 15286290987074524363:i64 --private 41041:i64 --private 0:i64 --private 0:i64 --private 0:i64 --private 549784760702:i64 --private 0:i64 --private 0:i64 --private 13839280179932823552:i64 --private 9466528:i64 --private 0:i64 --private 1245741926200423424:i64 --private 9993052845762533317:i64 --private 643603743268:i64 --private 0:i64 --private 0:i64 --private 0:i64 --private 687194767360:i64 --private 0:i64 --private 0:i64 --private 0:i64 --private 274894684160:i64 --private 0:i64 --private 17752714368831347629:i64 --private 14734568103978781184:i64 --private 16340025600:i64 --private 0:i64 --private 0:i64 --private 0:i64 --private 17179869184:i64 --private 0:i64 --private 0:i64 --private 13839280179932823552:i64 --private 9466528:i64 --private 0:i64 --private 0:i64 --private 13839280179932823552:i64 --private 9466528:i64 --private 0:i64 --private 0:i64 --private 13839280179932823552:i64 --private 9466528:i64 --private 0:i64 --private 0:i64 --private 13983395368008679424:i64 --private 180934170288:i64 --private 0:i64 --private 0:i64 --private 0:i64 --private 216736848758702080:i64 --private 0:i64 --private 0:i64 --private 0:i64 --private 10708425217887174656:i64 --private 8187143887307480351:i64 --private 70325280878010241:i64 --private 117203507575396024:i64 --private 11486502108844260361:i64 --private 13539931197926996738:i64 --private 18161434576524511916:i64 --private 11561024771253616253:i64 --private 0:i64 --private 0:i64 --private 0:i64 --private 12789659991778787328:i64 --private 160:i64 --private 0:i64 --private 0:i64 --private 0:i64 --private 40960:i64 --private 0:i64 --private 0:i64 --private 15880255236061790208:i64 --private 17950538412901046486:i64 --private 8547692942764276983:i64 --private 8509190860294355049:i64 --private 5730928406529570843:i64 --private 18210150271972058323:i64 --private 3994395479395232905:i64 --private 6563862530498629762:i64 --private 688805136118:i64 --private 0:i64 --private 0:i64 --private 13839280179932823552:i64 --private 175921869910688:i64 --private 0:i64 --private 0:i64 --private 0:i64 --private 45231150997700608:i64 --private 0:i64 --private 0:i64 --private 0:i64 --private 43020438485336064:i64 "
res = re.findall("--private ([0-9]+):i64 ", a)
bytes = struct.pack('Q', int(res[0]))
print(bytes)
with open("private.bin", "wb") as f:
    for data in res:
        bytes = struct.pack('<Q', int(data)) # < means little endian
        f.write(bytes)
with open("private.bin", "rb") as f:
    allbytes = f.read()
    size = int(len(allbytes)/8)
    for i in range(size):
        bytes = allbytes[i*8: (i+1)*8]
        int_val = int.from_bytes(bytes, "little")
        print(int_val)

os.remove("private.bin")