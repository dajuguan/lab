import socket
import struct
import threading
import queue
import time
import random

HOST = "127.0.0.1"
PORT = 12345


class VirtualStream:
    def __init__(self, conn, stream_id):
        self.conn = conn
        self.stream_id = stream_id
        self.queue = queue.Queue()
        self.thread = threading.Thread(target=self.process)
        self.thread.daemon = True
        self.thread.start()

    def process(self):
        while True:
            payload = self.queue.get()
            if payload is None:
                break
            # simulate the processing time cost
            time.sleep(random.uniform(0.1, 2.0))
            print(
                f"Server process req+, stream_id:{self.stream_id}, req: {payload.decode()}"
            )
            resp = payload.upper()
            frame = struct.pack("!II", self.stream_id, len(resp)) + resp
            self.conn.sendall(frame)
            print(f"Server sent response for stream {self.stream_id}")


def server():
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind((HOST, PORT))
        s.listen()
        print(f"Server listening on {HOST}:{PORT}")
        conn, addr = s.accept()
        with conn:
            print("Connected by", addr)
            streams = {}  # stream_id -> VirtualStream

            while True:
                header = conn.recv(8)
                if not header:
                    break
                stream_id, length = struct.unpack("!II", header)
                payload = b""
                while len(payload) < length:
                    chunk = conn.recv(length - len(payload))
                    if not chunk:
                        break
                    payload += chunk

                if stream_id not in streams:
                    #  create stream for each stream_id
                    streams[stream_id] = VirtualStream(conn, stream_id)
                # add the payload to corresponding stream processor queue
                print(
                    f"Server receive req=, stream_id:{stream_id}, req:{payload.decode()}"
                )
                streams[stream_id].queue.put(payload)


if __name__ == "__main__":
    server()
