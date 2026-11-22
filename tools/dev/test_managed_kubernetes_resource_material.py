"""Реальный process/TLS контур защищённого Secret material без live Kubernetes."""

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
import time
import unittest
from datetime import datetime, timedelta, timezone

CLI = Path(__file__).with_name("managed-kubernetes-resource-material.py")
SENSITIVE = "synthetic-private-secret-never-print"


class MaterialCLI(unittest.TestCase):
    """Проверяет one-shot wire, private-file policy, точные координаты и redaction."""

    @classmethod
    def setUpClass(cls):
        cls.directory = tempfile.TemporaryDirectory(prefix="k2145-material-cli-")
        cls.root = Path(cls.directory.name)
        cls.cert, cls.key = cls.root / "ca.pem", cls.root / "tls.key"
        cls.other_cert, cls.other_key = cls.root / "other-ca.pem", cls.root / "other.key"
        subprocess.run(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1", "-subj", "/CN=localhost", "-addext", "subjectAltName=DNS:localhost", "-keyout", str(cls.key), "-out", str(cls.cert)], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=10)
        subprocess.run(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1", "-subj", "/CN=foreign", "-keyout", str(cls.other_key), "-out", str(cls.other_cert)], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=10)

    @classmethod
    def tearDownClass(cls):
        cls.directory.cleanup()

    def setUp(self):
        self.work = tempfile.TemporaryDirectory(prefix="case-", dir=self.root)
        self.addCleanup(self.work.cleanup)
        self.material = Path(self.work.name) / "secret.json"
        self.source = {"apiVersion": "v1", "kind": "Secret", "metadata": {"name": "application", "namespace": "team"}, "type": "Opaque", "stringData": {"password": SENSITIVE}}
        self.material.write_text(json.dumps(self.source))
        self.material.chmod(0o600)

    def invoke(self, *args):
        result = subprocess.run([sys.executable, "-I", "-B", str(CLI), *map(str, args)], capture_output=True, timeout=10, env={"PATH": os.environ["PATH"], "LC_ALL": "C", "HTTPS_PROXY": "http://unreachable.invalid:1"})
        self.assertNotIn(SENSITIVE.encode(), result.stdout + result.stderr)
        self.assertNotIn(b"Traceback", result.stderr)
        return result

    def test_inspect_canonical_narrow_content_has_only_safe_metadata(self):
        result = self.invoke("inspect", "--operation", "kubernetes.create", "--secret-file", self.material)
        self.assertEqual(result.returncode, 0, result.stderr)
        expected = json.dumps({"stringData": {"password": SENSITIVE}}, separators=(",", ":"), sort_keys=True).encode()
        self.assertEqual(json.loads(result.stdout), {"operation": "kubernetes.create", "namespace": "team", "name": "application", "generateName": "", "secretType": "Opaque", "patchType": "", "contentSHA256": hashlib.sha256(expected).hexdigest(), "contentBytes": len(expected)})

    def test_patch_inspect_preserves_entire_native_document_and_empty_noop(self):
        for dialect, body in (("JSON", [{"op": "add", "path": "/stringData", "value": {"password": SENSITIVE}}]), ("MERGE", {"stringData": {"password": SENSITIVE}}), ("STRATEGIC", {}), ("JSON", [])):
            self.material.write_text(json.dumps(body))
            result = self.invoke("inspect", "--operation", "kubernetes.patch", "--patch-file", self.material, "--patch-type", dialect, "--namespace", "team", "--name", "application", "--secret-type", "Opaque")
            self.assertEqual(result.returncode, 0, result.stderr)
            expected = json.dumps(body, sort_keys=True, separators=(",", ":")).encode()
            self.assertEqual(json.loads(result.stdout)["contentSHA256"], hashlib.sha256(expected).hexdigest())

    def test_unsafe_files_ambiguous_json_and_wrong_secret_destination_are_closed(self):
        args = ("inspect", "--operation", "kubernetes.create", "--secret-file", self.material)
        for raw in (b'{"apiVersion":"v1","apiVersion":"v1"}', b'{"kind":"Secret","stringData":{"value":"\xff"}}', json.dumps(dict(self.source, type="kubernetes.io/service-account-token")).encode(), json.dumps(dict(self.source, metadata={"name": "application", "generateName": "application-", "namespace": "team"})).encode()):
            self.material.write_bytes(raw)
            self.assertEqual(self.invoke(*args).returncode, 2)
        self.material.write_text(json.dumps(self.source))
        self.material.chmod(0o644)
        self.assertEqual(self.invoke(*args).returncode, 2)
        self.material.chmod(0o600)
        link = Path(self.work.name) / "link"
        link.symlink_to(self.material)
        self.assertEqual(self.invoke("inspect", "--operation", "kubernetes.create", "--secret-file", link).returncode, 2)
        link.unlink()
        os.link(self.material, link)
        self.assertEqual(self.invoke(*args).returncode, 2)

    def tls_case(self, status=200, *, wrong_sni=False, wrong_ca=False, changed_destination=False, response_private=False, action="upload", drop_ack=False, slow_headers=False):
        calls = []
        view = {"materialRef": "krs_fixture01", "connectionRef": "int_fixture01", "connectionVersion": 7, "identityRef": "kbi_fixture01", "identityRevision": 2, "targetRef": "target_fixture01", "namespace": "team", "name": "application", "generateName": "", "operation": "kubernetes.create", "secretType": "Opaque", "patchType": "", "state": "PREPARED", "expiresAt": (datetime.now(timezone.utc) + timedelta(seconds=0.4 if slow_headers else 60)).isoformat(), "version": 1, "reasonCode": "", "credentialPolicy": "INITIATOR_REQUIRED", "bindingVersion": 3}

        class Backend(http.server.BaseHTTPRequestHandler):
            def do_POST(inner):
                body = inner.rfile.read(int(inner.headers["Content-Length"]))
                calls.append((inner.path, dict(inner.headers), body))
                if drop_ack:
                    inner.close_connection = True
                    return
                if slow_headers:
                    try:
                        inner.wfile.write(b"HTTP/1.1 200 OK\r\nX-Slow: ")
                        inner.wfile.flush()
                        for _ in range(30):
                            inner.wfile.write(b"x")
                            inner.wfile.flush()
                            time.sleep(0.05)
                        inner.wfile.write(b"\r\nContent-Type: application/json\r\nContent-Length: 0\r\n\r\n")
                        inner.wfile.flush()
                    except OSError:
                        pass
                    inner.close_connection = True
                    return
                inner.send_response(status)
                inner.send_header("Content-Type", "application/json")
                inner.send_header("Location", "/unexpected-redirect")
                inner.end_headers()
                result = dict(view, state="AVAILABLE", version=2, expiresAt=(datetime.now(timezone.utc) + timedelta(minutes=15)).isoformat())
                if response_private:
                    result["data"] = {"password": SENSITIVE}
                inner.wfile.write(json.dumps(result).encode())

            def log_message(inner, *_):
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
            ca = self.other_cert.read_bytes() if wrong_ca else self.cert.read_bytes()
            endpoint = {"serverOrigin": f"https://localhost:{server.server_port}", "tlsServerName": "wrong.invalid" if wrong_sni else "localhost", "certificateAuthorityPem": base64.b64encode(ca).decode(), "certificateAuthoritySHA256": hashlib.sha256(ca).hexdigest(), "addressIPs": ["127.0.0.1"]}
            admission = {"material": dict(view, name="other") if changed_destination else view, "reconcileOnly": False, "uploadPermit": base64.b64encode(bytes(32)).decode(), "endpoint": endpoint}
            path = Path(self.work.name) / "admission.json"
            path.write_text(json.dumps(admission))
            path.chmod(0o600)
            args = [action, "--admission-file", path]
            if action == "upload":
                args += ["--operation", "kubernetes.create", "--secret-file", self.material]
            started = time.monotonic()
            result = self.invoke(*args)
            elapsed = time.monotonic() - started
            if slow_headers:
                self.assertLess(elapsed, 1.0, "drip response exceeded absolute admission deadline")
            if wrong_ca or wrong_sni or changed_destination:
                self.assertEqual(calls, [])
            else:
                self.assertEqual(len(calls), 1)
                route, headers, body = calls[0]
                self.assertEqual(route, "/v1/kubernetes-resource-materials/krs_fixture01/" + action)
                self.assertEqual(headers["Authorization"], "Bearer " + "A" * 43)
                self.assertNotIn("Origin", headers)
                self.assertNotIn("Cookie", headers)
                self.assertEqual(body, b"" if action == "observe" else json.dumps({"stringData": {"password": SENSITIVE}}, sort_keys=True, separators=(",", ":")).encode())
            return result
        finally:
            server.shutdown()
            server.server_close()
            thread.join(timeout=2)
            self.assertFalse(thread.is_alive())

    def test_tls_upload_and_observe_return_only_safe_artifact_view(self):
        for action in ("upload", "observe"):
            result = self.tls_case(action=action)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(json.loads(result.stdout)["state"], "AVAILABLE")
            self.assertEqual(len(json.loads(result.stdout)), 18)

    def test_redirect_lost_ack_and_private_response_are_unknown_without_retry(self):
        for flags in ({"status": 307}, {"drop_ack": True}, {"response_private": True}):
            result = self.tls_case(**flags)
            self.assertEqual(result.returncode, 3)
            self.assertEqual(result.stdout, b"")
            self.assertIn(b"Do not repeat upload", result.stderr)

    def test_absolute_deadline_bounds_drip_response_without_repeating_write(self):
        result = self.tls_case(slow_headers=True)
        self.assertEqual(result.returncode, 3)

    def test_wrong_sni_ca_and_changed_destination_make_no_post(self):
        for flags in ({"wrong_sni": True}, {"wrong_ca": True}, {"changed_destination": True}):
            self.assertEqual(self.tls_case(**flags).returncode, 2)


if __name__ == "__main__":
    unittest.main()
