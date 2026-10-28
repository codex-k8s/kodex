#!/usr/bin/env python3
"""Проверяет реальные пакеты firewall в отдельном Linux network namespace.

Запуск: sudo -n unshare --net python3 tools/qa/test-host-firewall-network-test.py.
До любого изменения проверяет отличие namespace от PID1. Никаких портов или
правил в сетевом пространстве хоста не создаёт.
"""

import importlib.util
import json
import os
from pathlib import Path
import socket
import subprocess
import threading
import time


def command(args, **kwargs):
    """Запускает ограниченную команду теста с проверкой exit и deadline."""
    return subprocess.run(args, check=True, capture_output=True, text=True, timeout=10, **kwargs).stdout


def listen(port, ipv6=False):
    """Принимает TCP только внутри изолированного пространства теста."""
    server = socket.socket(socket.AF_INET6 if ipv6 else socket.AF_INET)
    if ipv6:
        server.setsockopt(socket.IPPROTO_IPV6, socket.IPV6_V6ONLY, 1)
    server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    server.bind(("::" if ipv6 else "0.0.0.0", port))
    server.listen()
    def serve():
        while True:
            try:
                conn, _ = server.accept()
                conn.close()
            except OSError:
                return
    threading.Thread(target=serve, daemon=True).start()
    return server


def main():
    """Проверяет input и Docker-подобный DNAT без использования живого сервера."""
    if os.getuid() != 0 or os.readlink("/proc/self/ns/net") == os.readlink("/proc/1/ns/net"):
        raise RuntimeError("ISOLATED_ROOT_NETWORK_NAMESPACE_REQUIRED")
    spec = importlib.util.spec_from_file_location("qa_firewall", Path(__file__).with_name("test-host-firewall.py"))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    config = {"managementIP": "192.0.2.10", "externalInterface": "wan", "githubHooks": ["198.51.100.0/24", "2001:db8:100::/48"]}
    client = subprocess.Popen(["unshare", "--net", "sleep", "90"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    backend = subprocess.Popen(["unshare", "--net", "sleep", "90"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    backend_server = None
    listeners = []
    try:
        for process in (client, backend):
            for _ in range(100):
                if os.readlink(f"/proc/{process.pid}/ns/net") != os.readlink("/proc/self/ns/net"):
                    break
                time.sleep(0.01)
            else:
                raise RuntimeError("CHILD_NAMESPACE_UNAVAILABLE")
        command(["ip", "link", "set", "lo", "up"])
        for external, peer, process in [("wan", "client", client), ("lan", "backend", backend)]:
            command(["ip", "link", "add", external, "type", "veth", "peer", "name", peer])
            command(["ip", "link", "set", peer, "netns", str(process.pid)])
            command(["ip", "link", "set", external, "up"])
            command(["nsenter", "-t", str(process.pid), "-n", "ip", "link", "set", peer, "up"])
        for address in ["192.0.2.1/24", "198.51.100.1/24"]:
            command(["ip", "address", "add", address, "dev", "wan"])
        for address in ["192.0.2.10/24", "192.0.2.20/24", "198.51.100.10/24"]:
            command(["nsenter", "-t", str(client.pid), "-n", "ip", "address", "add", address, "dev", "client"])
        for address in ["2001:db8:1::1/64", "2001:db8:100::1/64"]:
            command(["ip", "-6", "address", "add", address, "dev", "wan", "nodad"])
        for address in ["2001:db8:1::20/64", "2001:db8:100::10/64"]:
            command(["nsenter", "-t", str(client.pid), "-n", "ip", "-6", "address", "add", address, "dev", "client", "nodad"])
        command(["ip", "address", "add", "10.200.0.1/24", "dev", "lan"])
        command(["nsenter", "-t", str(backend.pid), "-n", "ip", "address", "add", "10.200.0.2/24", "dev", "backend"])
        command(["nsenter", "-t", str(backend.pid), "-n", "ip", "route", "add", "default", "via", "10.200.0.1"])
        command(["ip", "-6", "address", "add", "2001:db8:200::1/64", "dev", "lan", "nodad"])
        command(["nsenter", "-t", str(backend.pid), "-n", "ip", "-6", "address", "add", "2001:db8:200::2/64", "dev", "backend", "nodad"])
        command(["nsenter", "-t", str(backend.pid), "-n", "ip", "-6", "route", "add", "default", "via", "2001:db8:200::1"])
        command(["nft", "-f", "-"], input=module.render(config))
        listeners = [listen(port, ipv6) for ipv6 in [False, True] for port in [22, 80, 443, 8080]]
        probe_code = "import socket,sys;v6=':' in sys.argv[1];s=socket.socket(socket.AF_INET6 if v6 else socket.AF_INET);s.settimeout(.3);s.bind((sys.argv[1],0));r=s.connect_ex(('2001:db8:1::1' if v6 else '192.0.2.1',int(sys.argv[2])));print(int(r==0))"
        cases = [("192.0.2.10", 22, True), ("192.0.2.10", 8080, True),
                 ("192.0.2.20", 80, True), ("192.0.2.20", 22, False),
                 ("192.0.2.20", 443, False), ("192.0.2.20", 8080, False),
                 ("198.51.100.10", 443, True), ("198.51.100.10", 22, False),
                 ("2001:db8:1::20", 80, True), ("2001:db8:1::20", 22, False),
                 ("2001:db8:1::20", 443, False), ("2001:db8:100::10", 443, True)]
        for source, port, expected in cases:
            observed = command(["nsenter", "-t", str(client.pid), "-n", "python3", "-c", probe_code, source, str(port)]).strip() == "1"
            if observed != expected:
                raise RuntimeError(f"INPUT_CASE_FAILED_{port}")
        command(["sysctl", "-q", "-w", "net.ipv4.ip_forward=1"])
        command(["sysctl", "-q", "-w", "net.ipv6.conf.all.forwarding=1"])
        backend_code = """import socket,threading,time
def serve(p,v6):
 s=socket.socket(socket.AF_INET6 if v6 else socket.AF_INET)
 if v6:s.setsockopt(socket.IPPROTO_IPV6,socket.IPV6_V6ONLY,1)
 s.bind(('::' if v6 else '0.0.0.0',p));s.listen()
 while True:
  c,_=s.accept();c.close()
for v6 in [False,True]:
 for p in [80,8080,8443]:threading.Thread(target=serve,args=(p,v6),daemon=True).start()
time.sleep(60)
"""
        backend_server = subprocess.Popen(["nsenter", "-t", str(backend.pid), "-n", "python3", "-c", backend_code], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        time.sleep(0.2)
        command(["nft", "-f", "-"], input="""table ip docker_fixture {
chain prerouting { type nat hook prerouting priority dstnat; policy accept;
iifname "wan" tcp dport 80 dnat to 10.200.0.2:8080
iifname "wan" tcp dport 443 dnat to 10.200.0.2:8443
iifname "wan" tcp dport 8080 dnat to 10.200.0.2:80
}
}
table ip6 docker_fixture_v6 {
chain prerouting { type nat hook prerouting priority dstnat; policy accept;
iifname "wan" tcp dport 80 dnat to [2001:db8:200::2]:8080
iifname "wan" tcp dport 443 dnat to [2001:db8:200::2]:8443
iifname "wan" tcp dport 8080 dnat to [2001:db8:200::2]:80
}
}
""")
        forwards = [("192.0.2.10", 8080, True), ("192.0.2.20", 80, True),
                    ("192.0.2.20", 8080, False), ("192.0.2.20", 443, False),
                    ("198.51.100.10", 443, True), ("2001:db8:1::20", 80, True),
                    ("2001:db8:1::20", 8080, False), ("2001:db8:1::20", 443, False),
                    ("2001:db8:100::10", 443, True)]
        for source, port, expected in forwards:
            observed = command(["nsenter", "-t", str(client.pid), "-n", "python3", "-c", probe_code, source, str(port)]).strip() == "1"
            if observed != expected:
                raise RuntimeError(f"DNAT_CASE_FAILED_{port}")
        print(json.dumps({"status": "PASS", "inputCases": len(cases), "dnatCases": len(forwards), "scope": "ISOLATED_NETWORK_NAMESPACES"}))
    finally:
        for listener in listeners:
            listener.close()
        for process in [backend_server, client, backend]:
            if process is not None:
                process.terminate()
                process.wait(timeout=5)


if __name__ == "__main__":
    main()
