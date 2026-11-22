"""Реальный CLI/TLS контур без cluster, paid calls и private fixtures."""

import base64
import hashlib
import http.server
import json
import os
from pathlib import Path
import ssl
import subprocess
import sys
import tempfile
import threading
import unittest
from datetime import datetime, timedelta, timezone

CLI = Path(__file__).with_name("managed-kubernetes-identity.py")


class SetupCLI(unittest.TestCase):
    """Проверяет private-file policy, одно TLS POST и отсутствие redirect/retry."""

    @classmethod
    def setUpClass(cls):
        cls.directory = tempfile.TemporaryDirectory(prefix="k2145-cli-")
        cls.root = Path(cls.directory.name)
        cls.cert, cls.key = cls.root / "ca.pem", cls.root / "tls.key"
        subprocess.run(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1", "-subj", "/CN=localhost", "-addext", "subjectAltName=DNS:localhost", "-keyout", str(cls.key), "-out", str(cls.cert)], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=10)

    @classmethod
    def tearDownClass(cls):
        cls.directory.cleanup()

    def setUp(self):
        self.work = tempfile.TemporaryDirectory(prefix="case-", dir=self.root)
        self.addCleanup(self.work.cleanup)
        self.material = Path(self.work.name) / "kubeconfig"
        self.material.write_bytes(b"synthetic-private-material-not-for-output")
        self.material.chmod(0o600)

    def invoke(self, *args):
        result = subprocess.run([sys.executable, "-I", str(CLI), *map(str, args)], capture_output=True, timeout=10, env={"PATH": os.environ["PATH"], "LC_ALL": "C", "HTTPS_PROXY": "http://unreachable.invalid:1"})
        self.assertNotIn(self.material.read_bytes(), result.stdout + result.stderr)
        self.assertNotIn(b"Traceback", result.stderr)
        return result

    def test_inspect_outputs_only_digest_and_size(self):
        result = self.invoke("inspect", "--kubeconfig", self.material)
        self.assertEqual(result.returncode, 0)
        self.assertEqual(json.loads(result.stdout), {"contentSHA256": hashlib.sha256(self.material.read_bytes()).hexdigest(), "contentBytes": self.material.stat().st_size})

    def test_unsafe_local_files_fail_before_network(self):
        self.material.chmod(0o644)
        self.assertEqual(self.invoke("inspect", "--kubeconfig", self.material).returncode, 2)
        self.material.chmod(0o600)
        link = Path(self.work.name) / "link"
        link.symlink_to(self.material)
        self.assertEqual(self.invoke("inspect", "--kubeconfig", link).returncode, 2)
        link.unlink()
        os.link(self.material, link)
        self.assertEqual(self.invoke("inspect", "--kubeconfig", self.material).returncode, 2)

    def run_tls(self, status):
        calls = []
        view = {"setupRef": "ksu_fixture01", "connectionRef": "int_fixture01", "connectionVersion": 2, "bindingGeneration": 1, "bindingVersion": 1, "identityRef": "kbi_fixture01", "identityRevision": 1,
                "targetRef": "target_fixture01", "canonicalContext": "operator-context", "state": "PREPARED", "expiresAt": (datetime.now(timezone.utc) + timedelta(seconds=60)).isoformat(), "reasonCode": "", "version": 1, "credentialPolicy": "INITIATOR_REQUIRED"}
        material = self.material.read_bytes()

        class Backend(http.server.BaseHTTPRequestHandler):
            def do_POST(self):
                body = self.rfile.read(int(self.headers["Content-Length"]))
                calls.append((self.path, self.headers["Authorization"], body))
                self.send_response(status)
                self.send_header("Content-Type", "application/json")
                self.send_header("Location", "/unexpected-redirect")
                self.end_headers()
                result = dict(view, state="COMPLETED", version=2)
                self.wfile.write(json.dumps(result).encode("ascii"))

            def log_message(self, *_):
                pass

        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Backend)
        server.daemon_threads = True
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        context.minimum_version = ssl.TLSVersion.TLSv1_3
        context.load_cert_chain(self.cert, self.key)
        server.socket = context.wrap_socket(server.socket, server_side=True)
        thread = threading.Thread(target=server.serve_forever)
        thread.start()
        try:
            ca = self.cert.read_bytes()
            admission = {"setup": view, "reconcileOnly": False, "uploadPermit": base64.b64encode(bytes(32)).decode("ascii"), "endpoint": {"serverOrigin": f"https://localhost:{server.server_port}", "tlsServerName": "localhost",
                          "certificateAuthorityPem": base64.b64encode(ca).decode("ascii"), "certificateAuthoritySHA256": hashlib.sha256(ca).hexdigest(), "addressIPs": ["127.0.0.1"]}}
            path = Path(self.work.name) / "admission.json"
            path.write_text(json.dumps(admission))
            path.chmod(0o600)
            result = self.invoke("upload", "--admission-file", path, "--kubeconfig", self.material)
            self.assertEqual(len(calls), 1)
            self.assertEqual(calls[0][0], "/v1/kubernetes-identity-setups/ksu_fixture01/upload")
            self.assertEqual(calls[0][2], material)
            self.assertEqual(calls[0][1], "Bearer " + "A" * 43)
            return result
        finally:
            server.shutdown()
            server.server_close()
            thread.join(timeout=2)
            self.assertFalse(thread.is_alive())

    def test_exact_tls_upload_ignores_proxy_environment(self):
        result = self.run_tls(200)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout)["state"], "COMPLETED")

    def test_redirect_has_one_wire_attempt_and_unknown_result(self):
        result = self.run_tls(307)
        self.assertEqual(result.returncode, 3)
        self.assertEqual(result.stdout, b"")
        self.assertIn(b"Do not recreate material", result.stderr)


if __name__ == "__main__":
    unittest.main()
