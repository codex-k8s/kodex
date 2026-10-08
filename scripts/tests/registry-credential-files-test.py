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
NODE_SCRIPTS = [ROOT / "tools/dev/configure-k3d-node-registry.sh",
                ROOT / "tools/dev/configure-local-node-registry.sh"]
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

# Полные node scripts исполняются только против этих fake commands. Host/cluster
# команды не доступны: argv/env проверяется перед каждой синтетической операцией.
NODE_WRAPPER = r'''#!/usr/bin/env python3
import json, os, pathlib, shutil, stat, subprocess, sys
root = pathlib.Path(os.environ["FIXTURE_ROOT"])
values = json.loads((root / "values.json").read_text())
secrets = [*values.values(), *[value.rstrip("\n") for value in values.values()]]
for secret in secrets:
    assert secret not in "\0".join(sys.argv), "credential in argv"
    assert secret not in "\0".join(os.environ.values()), "credential in environment"
name, args = pathlib.Path(sys.argv[0]).name, sys.argv[1:]
with (root / "calls.jsonl").open("a") as out:
    out.write(json.dumps([name, *args])+"\n")
configuration = root / "configuration.json"
if name == "jq":
    for option in ("--rawfile", "--slurpfile"):
        for index, argument in enumerate(args):
            if argument == option:
                path = pathlib.Path(args[index+2])
                assert path.is_file() and not path.is_symlink()
                assert stat.S_IMODE(path.stat().st_mode) == 0o600
                assert stat.S_IMODE(path.parent.stat().st_mode) == 0o700
    os.execv(os.environ["REAL_JQ"], ["jq", *args])
if name == "yq":
    path = next((pathlib.Path(arg) for arg in args if arg.startswith("/")), None)
    text = path.read_text() if path else sys.stdin.read()
    if path is None:
        temporary = pathlib.Path(os.environ["TMPDIR"])
        for directory in temporary.iterdir():
            assert stat.S_IMODE(directory.stat().st_mode) == 0o700
            for child in directory.iterdir():
                assert stat.S_IMODE(child.stat().st_mode) == 0o600
    print(text, end="")
    sys.exit(0)
if name == "kubectl":
    if args == ["config", "current-context"]:
        print(os.environ["EXPECTED_CONTEXT"])
    else:
        assert args[0] in ("get", "wait")
    sys.exit(0)
if name == "k3d":
    assert args == ["node", "list", "-o", "json"]
    print(json.dumps([{"name":"k3d-fixture-server-0","role":"server"},
                      {"name":"k3d-fixture-agent-0","role":"agent"}]))
    sys.exit(0)
if name in ("sleep", "systemctl"):
    if name == "systemctl":
        assert os.environ["MODE"] == "apply" and args == ["restart", "k3s"]
    sys.exit(0)
def digest(path):
    import hashlib
    print(hashlib.sha256(path.read_bytes()).hexdigest()+"  "+str(path))
if name == "sudo":
    assert args[0] == "-n"
    command = args[1:]
    if command[:2] == ["test", "-f"]:
        sys.exit(0 if (configuration.exists() if command[2].endswith("registries.yaml") else True) else 1)
    if command[0] == "yq":
        print(configuration.read_text(), end="")
    elif command[0] == "sha256sum":
        digest(root / "material/node-pull" / pathlib.Path(command[1]).name)
    elif command[0] == "install":
        assert os.environ["MODE"] == "apply"
        if command[-1].endswith("registries.yaml"):
            source = pathlib.Path(command[-2])
            assert stat.S_IMODE(source.stat().st_mode) == 0o600
            configuration.write_text(source.read_text())
    elif command[0] == "systemctl":
        assert os.environ["MODE"] == "apply" and command == ["systemctl", "restart", "k3s"]
    else:
        raise AssertionError("unsupported synthetic sudo operation")
    sys.exit(0)
assert name == "docker"
if args[0] == "inspect":
    print("running" if "{{.State.Status}}" in args else "10.1.2.3")
elif args[0] in ("cp", "restart"):
    assert os.environ["MODE"] == "apply", "readback attempted node mutation"
    if args[0] == "cp" and args[-1].endswith("registries.yaml"):
        source = pathlib.Path(args[1])
        assert stat.S_IMODE(source.stat().st_mode) == 0o600
        configuration.write_text(source.read_text())
else:
    assert args[0] == "exec"
    command = args[2:]
    if command == ["true"]:
        pass
    elif command[:2] == ["test", "-f"]:
        sys.exit(0 if configuration.exists() else 1)
    elif command[0] == "cat":
        assert command[1].endswith("registries.yaml")
        print(configuration.read_text(), end="")
    elif command[0] == "sha256sum":
        digest(root / "material/node-pull" / pathlib.Path(command[1]).name)
    elif command[0] in ("sh", "install", "chmod"):
        assert os.environ["MODE"] == "apply", "readback attempted node mutation"
        if command[0] == "sh":
            (root / "alias").write_text("present")
    elif command[0] == "grep":
        sys.exit(0 if (root / "alias").exists() else 1)
    else:
        raise AssertionError("unsupported synthetic docker operation")
'''


class CredentialFiles(unittest.TestCase):
    def run_node_case(self, script_path, mode="apply", existing="object",
                      trailing_newlines=False, alias=True):
        with tempfile.TemporaryDirectory(prefix="kodex-node-credentials-") as directory:
            root = Path(directory)
            work = root / "material/node-pull"
            work.mkdir(parents=True, mode=0o700)
            values = {"ca.crt": "SENTINEL_NODE_CA\n", "client.crt": "SENTINEL_NODE_CERT\n",
                      "client.key": "SENTINEL_NODE_PRIVATE_KEY\n", "username": "SENTINEL_NODE_USER",
                      "password": 'SENTINEL_NODE_PASSWORD_LONG_"_\\_雪_0123456789',
                      "legacy_password": "SENTINEL_OLD_NODE_PASSWORD_0123456789"}
            if trailing_newlines:
                for name in ("username", "password"):
                    values[name] += "\n\n"
            (root / "values.json").write_text(json.dumps(values))
            for name, value in values.items():
                path = work / name
                path.write_text(value)
                path.chmod(0o600)
            host = "pull.fixture.invalid"
            prior = {"mirrors": {"other.invalid": {"endpoint": ["https://other.invalid"]}},
                     "configs": {"other.invalid": {"auth": {"password": values["legacy_password"]}}},
                     "unrelated": {"keep": [1, True, "value"]}}
            expected = dict(prior) if existing == "object" else {}
            expected["mirrors"] = {**expected.get("mirrors", {}), host: {"endpoint": ["https://"+host]}}
            expected["configs"] = {**expected.get("configs", {}), host: {
                "auth": {"username": values["username"].rstrip("\n"), "password": values["password"].rstrip("\n")},
                "tls": {"ca_file": "/etc/rancher/k3s/kodex-registry/ca.crt",
                        "cert_file": "/etc/rancher/k3s/kodex-registry/client.crt",
                        "key_file": "/etc/rancher/k3s/kodex-registry/client.key"}}}
            configuration = root / "configuration.json"
            if existing != "absent":
                document = expected if mode == "readback" else prior if existing == "object" else None
                configuration.write_text(json.dumps(document))
                if existing == "multiple":
                    configuration.write_text("{}\n{}\n")
            if alias:
                (root / "alias").write_text("present")
            temporary = root / "temporary"
            temporary.mkdir(mode=0o700)
            for name in ("jq", "yq", "docker", "k3d", "kubectl", "sudo", "systemctl", "sleep"):
                path = root / name
                path.write_text(NODE_WRAPPER)
                path.chmod(0o700)
            context = "k3d-fixture" if "k3d" in script_path.name else "local-fixture"
            environment = {"PATH": str(root)+":/usr/bin:/bin", "FIXTURE_ROOT": str(root),
                           "REAL_JQ": shutil.which("jq"), "TMPDIR": str(temporary),
                           "EXPECTED_CONTEXT": context, "MODE": mode,
                           "username": "public inherited placeholder", "password": "public inherited placeholder"}
            for name in ("actual", "expected", "actual_host", "expected_host"):
                environment[name] = "public inherited placeholder"
            result = subprocess.run(["/bin/bash", str(script_path), "--mode", mode,
                                     "--context", context, "--material-directory", str(work.parent),
                                     "--promoted-pull-host", host], env=environment,
                                    capture_output=True, text=True, timeout=15)
            failed = existing == "multiple" or (mode == "readback" and not alias and "k3d" in script_path.name)
            self.assertEqual(result.returncode != 0, failed, "synthetic node script outcome mismatch")
            calls = (root / "calls.jsonl").read_text()
            for secret in [*values.values(), *[value.rstrip("\n") for value in values.values()]]:
                self.assertNotIn(secret, calls+result.stdout+result.stderr)
            if not failed:
                self.assertEqual(json.loads(configuration.read_text()), expected)
            if mode == "readback":
                for call in map(json.loads, calls.splitlines()):
                    if call[0] == "docker":
                        self.assertNotIn(call[1], ("cp", "restart"))
                        if call[1] == "exec":
                            self.assertNotIn(call[3], ("sh", "install", "chmod"))
                    if call[0] == "sudo":
                        self.assertNotIn(call[2], ("install", "systemctl"))
            self.assertEqual(list(temporary.iterdir()), [])

    def test_node_credentials_file_merge_and_argv_guard(self):
        for script in NODE_SCRIPTS:
            for existing in ("object", "absent", "null", "multiple"):
                with self.subTest(script=script.name, existing=existing):
                    self.run_node_case(script, existing=existing, trailing_newlines=True)

    def test_node_readback_never_mutates(self):
        for script in NODE_SCRIPTS:
            with self.subTest(script=script.name):
                self.run_node_case(script, mode="readback")
        self.run_node_case(NODE_SCRIPTS[0], mode="readback", alias=False)

    def test_node_source_never_passes_secret_arguments(self):
        for script in NODE_SCRIPTS:
            source = script.read_text()
            self.assertNotRegex(source, r"--arg(?:json)?\s+(?:username|password|existing)\b")
            self.assertIn("umask 077", source)
            self.assertIn("--rawfile password", source)
            self.assertIn("--slurpfile existing", source)

    def run_case(self, case, fail=False, missing=False, component="all", readback=False):
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
            for name in ["RUNNER", "FRONTEND", "ROLE_INPUT", "SESSION_ARCHIVE", "ROLE_IMAGE_BUILDER"]:
                environment["KODEX_"+name+"_DIGEST"] = digest
            environment.update(KODEX_SEED_COMPONENT=component, KODEX_SEED_READBACK_ONLY="true" if readback else "false",
                               KODEX_FRONTEND_REFERENCE="public.invalid/frontend@"+digest,
                               KODEX_SOURCE_REVISION="b"*40)
            if case == "seed":
                source = SEED.read_text()
                body = source.split('--entrypoint /bin/sh "$tools_image" -ec ', 1)[1].split("'", 2)[1]
                cleanup = source.split("cleanup() {", 1)[1].split("\n}\n", 1)[0]
                script = 'set -eu\ntemporary_directory="'+str(work)+'"\nport_forward_pid=""\ntools_image=fixture\n'
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
            if case == "seed" and component in ("session-archive", "role-image-builder"):
                if not fail:
                    self.assertIn("kodex/" + component + "@sha256:", calls)
                self.assertNotIn("kodex/agent-runner", calls)
                self.assertNotIn("kodex/roles", calls)
                if readback:
                    self.assertNotIn('"import"', calls)
                else:
                    self.assertIn('"import"', calls)
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

    def test_platform_archive_exact_publication(self):
        self.run_case("seed", component="session-archive")

    def test_platform_archive_readback_has_no_write(self):
        self.run_case("seed", component="session-archive", readback=True)

    def test_platform_archive_failure_cleanup(self):
        self.run_case("seed", True, component="session-archive")

    def test_platform_builder_exact_publication(self):
        self.run_case("seed", component="role-image-builder")

    def test_platform_builder_readback_has_no_write(self):
        self.run_case("seed", component="role-image-builder", readback=True)

    def test_platform_builder_failure_cleanup(self):
        self.run_case("seed", True, component="role-image-builder")

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
