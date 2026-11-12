"""Только loopback/Unix-сокеты синтетической оснастки, без внешних credentials."""
import os, select, socket, socketserver, sys, time, stat
from pathlib import Path
pod_mode = os.environ.get("KODEX_TEST_NATIVE_POD") == "1"
harness = "/harness/private" if pod_mode else "/harness"
if pod_mode:
    if os.getuid() != 10001:
        raise SystemExit("private synthetic proxy UID is invalid")
    deadline = time.monotonic() + 180
    while not Path(harness + "/proxy.yaml").is_file():
        if time.monotonic() >= deadline:
            raise SystemExit("private synthetic proxy configuration timed out")
        time.sleep(0.1)
    info = os.lstat(harness + "/proxy.yaml")
    if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or info.st_mode & 0o777 != 0o600 or info.st_nlink != 1:
        raise SystemExit("private synthetic proxy configuration is invalid")
class Relay(socketserver.BaseRequestHandler):
    def handle(self):
        is_model = self.server.address_family == socket.AF_INET
        family = socket.AF_UNIX if is_model else socket.AF_INET
        target_address = harness + "/upstream.sock" if is_model else ("127.0.0.1", 4000)
        with socket.socket(family, socket.SOCK_STREAM) as upstream:
            try:
                upstream.connect(target_address)
            except OSError:
                return
            peers = [self.request, upstream]
            while peers:
                readable, _, _ = select.select(peers, [], [], 30)
                if not readable:
                    return
                for source in readable:
                    target = upstream if source is self.request else self.request
                    data = source.recv(65536)
                    if not data:
                        return
                    target.sendall(data)
if os.fork() == 0:
    socketserver.ThreadingTCPServer.allow_reuse_address = True
    with socketserver.ThreadingTCPServer(("127.0.0.1", 4001), Relay) as server:
        server.serve_forever()
if os.fork() == 0:
    os.umask(0o077)
    with socketserver.ThreadingUnixStreamServer(harness + "/proxy.sock", Relay) as server:
        server.serve_forever()
os.execvp("litellm", ["litellm"] + sys.argv[1:])
