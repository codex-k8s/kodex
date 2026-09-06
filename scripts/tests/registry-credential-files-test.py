#!/usr/bin/env python3
"""Проверка реальных shell-фрагментов с искусственными credentials, без сети."""
import base64
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SEED = ROOT / "tools/dev/seed-local-image-supply-chain.sh"
ADMISSION = ROOT / "deploy/k8s/base/image-supply-chain/image-admission.sh"
CLEANUP = ROOT / "deploy/k8s/base/image-supply-chain/cleanup.sh"
VALUES = {"ca.pem": "SENTINEL_CA\n", "client.crt": "SENTINEL_CERT\n",
          "client.key": "SENTINEL_PRIVATE_KEY\n", "username": "SENTINEL_USER\r\n",
          "password": 'SENTINEL_PASSWORD_"_\\\r\n'}

# Wrapper видит фактический argv каждого jq/regctl. Секреты в fixture синтетические.
WRAPPER = r'''#!/usr/bin/env python3
import base64, json, os, pathlib, stat, subprocess, sys
root = pathlib.Path(os.environ["FIXTURE_ROOT"])
values = json.loads((root / "values.json").read_text())
user = values["username"].replace("\r", "").replace("\n", "")
password = values["password"].replace("\r", "").replace("\n", "")
for secret in [*values.values(), user, password, base64.b64encode((user+":"+password).encode()).decode()]:
    assert secret not in "\0".join(sys.argv), "credential in argv"
name = pathlib.Path(sys.argv[0]).name
with (root / "calls.jsonl").open("a") as out:
    out.write(json.dumps([name, *sys.argv[1:]])+"\n")
if name == "jq":
    os.execv(os.environ["REAL_JQ"], ["jq", *sys.argv[1:]])
if name == "awk":
    os.execv(os.environ["REAL_AWK"], ["busybox", "awk", *sys.argv[1:]])
if name == "docker":
    shutil = __import__("shutil")
    for child in ["home", "docker"]:
        shutil.rmtree(root / "material" / child, ignore_errors=True)
    sys.exit(0)
config = pathlib.Path(os.environ["REGCTL_CONFIG"])
if sys.argv[1:3] == ["registry", "set"]:
    assert sys.argv[-3:] == ["--skip-check", "--tls", "enabled"]
    config.write_text(json.dumps({"version":1,"hosts":{sys.argv[3]:{"tls":"enabled"}}}))
    config.chmod(0o600)
    sys.exit(0)
assert stat.S_ISREG(config.lstat().st_mode) and stat.S_IMODE(config.stat().st_mode) == 0o600
assert stat.S_IMODE(config.parent.stat().st_mode) == 0o700
hosts = json.loads(config.read_text())["hosts"]
assert len(hosts) == int(os.environ.get("EXPECTED_HOSTS", "1"))
for host in hosts.values():
    assert host["tls"] == "enabled"
    assert host["regcert"] == values["ca.pem"]
    assert host["clientCert"] == values["client.crt"]
    assert host["clientKey"] == values["client.key"]
    assert host["user"] == user and host["pass"] == password
if os.environ["CASE"] == "admission":
    docker = pathlib.Path(os.environ["DOCKER_CONFIG"]) / "config.json"
    assert stat.S_IMODE(docker.stat().st_mode) == 0o600
    auths = json.loads(docker.read_text())["auths"]
    assert len(auths) == len(hosts)
    for auth in auths.values():
        assert base64.b64decode(auth["auth"]).decode() == user+":"+password
(root / "validated").write_text("PASS")
if os.environ.get("FAIL_REGISTRY") == "1":
    sys.exit(19)
if sys.argv[1:3] == ["image", "digest"]:
    print("sha256:" + "a"*64)
'''


class CredentialFiles(unittest.TestCase):
    def run_case(self, case, fail=False, missing=False):
        with tempfile.TemporaryDirectory(prefix="kodex-credentials-") as directory:
            root = Path(directory)
            work = root / "material"
            work.mkdir(mode=0o700)
            (root / "values.json").write_text(json.dumps(VALUES))
            for name, value in VALUES.items():
                path = work / name
                path.write_text(value)
                path.chmod(0o600)
            if missing:
                (work / "client.key").unlink()
            for name in ["jq", "awk", "regctl", "docker"]:
                path = root / name
                path.write_text(WRAPPER)
                path.chmod(0o700)
            environment = {"PATH": str(root)+":/usr/bin:/bin", "FIXTURE_ROOT": str(root),
                           "REAL_JQ": shutil.which("jq"), "REAL_AWK": shutil.which("busybox"), "CASE": case,
                           "FAIL_REGISTRY": "1" if fail else "0", "TMPDIR": str(work)}
            digest = "sha256:" + "a"*64
            for name in ["RUNNER", "FRONTEND", "ROLE_INPUT"]:
                environment["KODEX_"+name+"_DIGEST"] = digest
            environment.update(KODEX_FRONTEND_REFERENCE="public.invalid/frontend@"+digest,
                               KODEX_SOURCE_REVISION="b"*40)
            if case == "seed":
                source = SEED.read_text()
                body = source.split('--entrypoint /bin/sh "$tools_tag" -ec ', 1)[1].split("'", 2)[1]
                cleanup = source.split("cleanup() {", 1)[1].split("\n}\n", 1)[0]
                script = 'set -eu\ntemporary_directory="'+str(work)+'"\nport_forward_pid=""\ntools_tag=fixture\n'
                script += "cleanup() {"+cleanup+"\n}\ntrap cleanup EXIT\n"+body
            elif case == "admission":
                function = ADMISSION.read_text().split("login_registry() {", 1)[1].split("\n}\n", 1)[0]
                script = "set -eu\nlogin_registry() {"+function+"\n}\n"
                script += 'login_registry registry-one /work/username /work/password\n'
                script += 'login_registry registry-two /work/username /work/password\nregctl repo ls registry-two\n'
                environment["EXPECTED_HOSTS"] = "2"
            else:
                script = CLEANUP.read_text().replace("/var/run/secrets/kodex/image-registry/admin", "/work")
            script = script.replace("/tmp/docker", str(work / "docker"))
            script = script.replace("/identity/registry-client", "/work/client")
            script = script.replace("/identity/ca.pem", "/work/ca.pem")
            script = script.replace("/work", str(work))
            script_file = root / "run.sh"
            script_file.write_text(script)
            result = subprocess.run(["/bin/sh", str(script_file)], env=environment,
                                    capture_output=True, text=True, timeout=10)
            if missing:
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse((root / "validated").exists())
            else:
                self.assertEqual(result.returncode, 19 if fail else 0, result.stderr)
                self.assertTrue((root / "validated").exists(), result.stderr)
            calls_path = root / "calls.jsonl"
            calls = calls_path.read_text() if calls_path.exists() else ""
            for secret in [*VALUES.values(), *[v.strip() for v in VALUES.values()]]:
                self.assertNotIn(secret, calls+result.stdout+result.stderr)
            if case == "seed":
                self.assertFalse(work.exists())
            elif case == "admission":
                self.assertFalse((work / "docker").exists())
            else:
                self.assertEqual(sorted(p.name for p in work.iterdir()),
                                 sorted(k for k in VALUES if not missing or k != "client.key"))

    def test_seed_success(self):
        self.run_case("seed")

    def test_seed_failure_cleanup(self):
        self.run_case("seed", True)

    def test_admission_multi_host_success(self):
        self.run_case("admission")

    def test_admission_failure_cleanup(self):
        self.run_case("admission", True)

    def test_cleanup_success(self):
        self.run_case("cleanup")

    def test_cleanup_failure(self):
        self.run_case("cleanup", True)

    def test_missing_key_never_reaches_registry(self):
        for case in ["seed", "admission", "cleanup"]:
            with self.subTest(case=case):
                self.run_case(case, missing=True)


if __name__ == "__main__":
    unittest.main()
