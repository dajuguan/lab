import socket
import struct
import threading
import time
import random
import queue

HOST = "127.0.0.1"
PORT = 12345
MAX_STREAMS = 3

messages = ["1a", "12a", "123a", "1234a", "12345a", "123456a"]

"""
The wrong implentation for client: process the request not in the order which they are sent.
Because the TCP stream processed by the server is sequentialy, we must process the response from the server in the order we sent the request, or the {req-> response} mapping would be wrong.
"""


class VirtualStream:
    def __init__(self, conn, stream_id):
        self.conn = conn
        self.stream_id = stream_id
        self.resps = queue.Queue()
        self._req = ""
        self.mu = threading.Lock()

    def send(self, msg):
        with self.mu:
            data = msg.encode()
            self._req = msg
            s.sendall(struct.pack("!II", self.stream_id, len(data)) + data)

    def req(self):
        with self.mu:
            return self._req

    def recv(self):
        return self.resps.get()


def recv_loop(socket, streams):
    while True:
        header = socket.recv(8)
        stream_id, length = struct.unpack("!II", header)

        # read payload
        payload = b""
        while len(payload) < length:
            chunk = socket.recv(length - len(payload))
            payload += chunk

        if stream_id in streams:
            streams[stream_id].resps.put(payload.decode())
        else:
            print(f"Unknown stream_id {stream_id}")


with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
    s.connect((HOST, PORT))

    #  stream_id -> VirtualStream mapping
    streams = {}
    for i in range(0, len(messages)):
        i = i % MAX_STREAMS
        streams[i] = VirtualStream(s, i)

    # start resp receiving loop
    threading.Thread(target=recv_loop, args=(s, streams), daemon=True).start()

    # send request concurrently
    threads = []
    for i, msg in enumerate(messages, 0):
        i = i % MAX_STREAMS
        t = threading.Thread(
            target=lambda s, m: (
                s.send(m),
                print(f"Stream {s.stream_id}, req:{s.req()}, received: {s.recv()}"),
            ),
            args=(streams[i], msg),
        )
        threads.append(t)
        t.start()

    for t in threads:
        t.join()
