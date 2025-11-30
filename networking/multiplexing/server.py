import socket
import struct

HOST = '127.0.0.1'
PORT = 12345

def handle_client(conn):
    while True:
        # read 4 bytes length
        header = conn.recv(4)
        if not header:
            break
        length = struct.unpack('!I', header)[0]  # network byte order

        # 再读 payload
        data = b''
        while len(data) < length:
            chunk = conn.recv(length - len(data))
            if not chunk:
                break
            data += chunk

        print("Server received:", data.decode())

        # response to client, send length first
        resp = data.upper()  # simply resp the upper case of request
        conn.sendall(struct.pack('!I', len(resp)) + resp)
        print("Server sendResp, len, b:", struct.pack('!I', len(resp)), "unpacked:", len(resp),"resp:", resp)

with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
    s.bind((HOST, PORT))
    s.listen()
    print(f"Server listening on {HOST}:{PORT}")
    conn, addr = s.accept()
    with conn:
        print('Connected by', addr)
        handle_client(conn)
