import socket
import struct
import threading
import time
import random

HOST = '127.0.0.1'
PORT = 12345

messages = ['1a', '12a', '123a', '1234a', '12345a']
mu = threading.Lock()

'''
The wrong implentation for client: process the request not in the order which they are sent.
Because the TCP stream processed by the server is sequentialy, we must process the response from the server in the order we sent the request, or the {req-> response} mapping would be wrong.
'''
def send_request(s, msg):
    data = msg.encode()
    print(f"Client sendReq: {msg}")
    s.sendall(struct.pack('!I', len(data)) + data)

    # sleep for some random time to simulate random scheduler
    time.sleep(random.randint(0,3)/1000)

    # # read length
    with mu:
        header = s.recv(4)
        length = struct.unpack('!I', header)[0]

        # read payload
        resp = b''
        while len(resp) < length:
            chunk = s.recv(length - len(resp))
            resp += chunk
        print(f"Client received: {resp.decode()} for req: {msg}")

with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
    s.connect((HOST, PORT))

    threads = []
    for msg in messages:
        t = threading.Thread(target=send_request, args=(s, msg))
        threads.append(t)
        t.start()

    for t in threads:
        t.join()
