#!/usr/bin/env python3
"""Настоящий disposable API/publisher; guard ACK принадлежит тесту, не broker."""
import base64
import copy
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "tools/install/bootstrap-secret-drafts.sh"
RECOVERY = ROOT / "tools/install/draft-key-recovery.py"
RETAINED = "kodex-secret-drafts"
KEYRING = "secret-broker-draft-keyring"
GUARD = "secret-broker-draft-key-guard"


def require(ok, message):
    if not ok:
        raise AssertionError(message)


def run(args, value=None, env=None, cwd=None, timeout=30):
    return subprocess.run(args, input=value, capture_output=True, env=env,
                          cwd=cwd, timeout=timeout)


def kube(container, args, value=None):
    return run(["docker", "exec", "-i", container, "kubectl", *args], value)


def get(container, kind, name, namespace=RETAINED):
    result = kube(container, ["-n", namespace, "get", kind, name, "-o", "json"])
    require(result.returncode == 0, "fixture API read failed")
    return json.loads(result.stdout)


def acknowledge(container):
    # Явная fixture mutation canonical manifest. Проверяет API/installer, а не
    # readiness broker: настоящие Observe/Reserve остаются отдельными Go tests.
    secret = get(container, "secret", KEYRING, "kodex-system")
    document = json.loads(base64.b64decode(secret["data"]["keyring.json"]))
    keys = [{"ID": k["id"], "Generation": k["generation"]}
            for k in sorted(document["keys"], key=lambda k: k["generation"])]
    manifest = {"revision": document["revision"],
                "current": next(k for k in keys if k["ID"] == document["current"]),
                "keys": keys, "digest": ""}
    manifest["digest"] = hashlib.sha256(json.dumps(manifest, separators=(",", ":")).encode()).hexdigest()
    guard = get(container, "configmap", GUARD)
    state = json.loads(guard["data"]["state.json"])
    previous = {k["id"]: k for k in state["uses"]}
    state["manifest"] = manifest
    state["uses"] = [previous.get(k["ID"], {"id": k["ID"], "generation": k["Generation"], "encryptions": 7}) for k in keys]
    guard["data"]["state.json"] = json.dumps(state, separators=(",", ":"))
    require(kube(container, ["replace", "-f", "-"], json.dumps(guard).encode()).returncode == 0,
            "fixture consumer ACK failed")


def kubectl_wrapper():
    container = os.environ["DRAFT_API_CONTAINER"]
    args = sys.argv[2:]
    if "--context" in args:
        index = args.index("--context")
        require(args[index + 1] == "fixture", "fixture context mismatch")
        del args[index:index + 2]
    args = [a for a in args if not a.startswith("--request-timeout=")]
    payload = None
    if "-f" in args:
        index = args.index("-f")
        payload = sys.stdin.buffer.read() if args[index + 1] == "-" else Path(args[index + 1]).read_bytes()
        args[index + 1] = "-"
    incoming = json.loads(payload) if payload else None
    serving = incoming and incoming.get("kind") == "Secret" and incoming.get("metadata", {}).get("name") == KEYRING
    if serving and args[0] == "replace":
        race = os.environ.get("DRAFT_API_RACE")
        if race == "resourceVersion":
            patch = {"metadata": {"annotations": {"fixture-race": "changed"}}}
            require(kube(container, ["-n", "kodex-system", "patch", "secret", KEYRING, "--type=merge", "-p", json.dumps(patch)]).returncode == 0,
                    "fixture concurrent writer failed")
        elif race == "UID":
            incoming["metadata"]["uid"] = "00000000-0000-4000-8000-000000000001"
            payload = json.dumps(incoming).encode()
    result = kube(container, args, payload)
    race = os.environ.get("DRAFT_API_PRESERVE_RACE")
    marker = Path(os.environ.get("DRAFT_API_RACE_MARKER", "/nonexistent/fixture-marker"))
    if result.returncode == 0 and race and "get" in args and "secret" in args and KEYRING in args and not marker.exists():
        marker.write_text("triggered")
        before = json.loads(result.stdout)
        if race == "resourceVersion":
            patch = {"metadata": {"annotations": {"fixture-preserve-race": "changed"}}}
            require(kube(container, ["-n", "kodex-system", "patch", "secret", KEYRING, "--type=merge", "-p", json.dumps(patch)]).returncode == 0,
                    "fixture preserve concurrent writer failed")
        elif race == "UID":
            require(kube(container, ["-n", "kodex-system", "delete", "secret", KEYRING]).returncode == 0,
                    "fixture preserve replacement delete failed")
            for key in ("uid", "resourceVersion", "creationTimestamp", "managedFields"):
                before["metadata"].pop(key, None)
            require(kube(container, ["create", "-f", "-"], json.dumps(before).encode()).returncode == 0,
                    "fixture preserve replacement create failed")
        # Возвращается реальный GET, совершённый до настоящей concurrent mutation.
    if result.returncode == 0 and serving and args[0] in ("create", "replace"):
        acknowledge(container)
    # Вывод нужен только внутреннему helper, родитель suite его никогда не печатает.
    sys.stdout.buffer.write(result.stdout)
    sys.stderr.buffer.write(result.stderr)
    sys.exit(result.returncode)


def main():
    image = os.environ["KODEX_DRAFT_RECOVERY_TEST_IMAGE"]
    require(re.fullmatch(r"sha256:[0-9a-f]{64}", image), "immutable fixture image required")
    name = "kodex-draft-recovery-api-" + str(os.getpid())
    volumes, created = [], False
    with tempfile.TemporaryDirectory(prefix="kodex-draft-api-") as raw:
        temporary = Path(raw)
        temporary.chmod(0o700)
        binary = temporary / "secret-draft-keys"
        result = run(["go", "build", "-o", str(binary), "./cmd/secret-draft-keys"],
                     cwd=ROOT / "services/internal/secret-broker", timeout=120)
        require(result.returncode == 0, "key CLI compilation failed")
        wrapper = temporary / "bin"
        wrapper.mkdir(mode=0o700)
        (wrapper / "kubectl").write_text("#!/bin/sh\nexec python3 " + str(Path(__file__).resolve()) + " --kubectl \"$@\"\n")
        # Go CLI уже собран из проверяемого checkout; не меняем production helper API.
        (wrapper / "go").write_text("#!/bin/sh\n[ \"$1\" = run ] && [ \"$2\" = ./cmd/secret-draft-keys ] || exit 1\nshift 2\nexec " + str(binary) + " \"$@\"\n")
        for item in wrapper.iterdir():
            item.chmod(0o700)
        env = {"HOME": os.environ["HOME"], "PATH": str(wrapper) + os.pathsep + os.environ["PATH"],
               "TMPDIR": str(temporary), "DRAFT_API_CONTAINER": name}
        first, second, wrong = (temporary / p for p in ("first.json", "second.json", "wrong.json"))
        for args in (["generate", "--output-file", str(first)], ["generate", "--output-file", str(wrong)],
                     ["rotate", "--input-file", str(first), "--output-file", str(second), "--expected-revision", "1"]):
            require(run([str(binary), *args]).returncode == 0, "synthetic key generation failed")
        forbidden = [k["material"].encode() for p in (first, second, wrong) for k in json.loads(p.read_bytes())["keys"]]

        def invoke(args, success=True, extra=None):
            result = run(args, cwd=ROOT, env=dict(env, **(extra or {})), timeout=45)
            require(not any(k in result.stdout + result.stderr for k in forbidden), "key escaped diagnostics")
            require((result.returncode == 0) == success, "recovery operation outcome mismatch: " + args[2])
            return result.stdout

        def bootstrap(mode, key, success=True, extra=None):
            args = ["bash", str(SCRIPT), mode, "--context", "fixture", "--keyring-file", str(key), "--readback-timeout-seconds", "2"]
            if mode == "rotate":
                args += ["--expected-revision", "1"]
            return invoke(args, success, extra)

        def recovery(mode, key=None, success=True, directory=None, extra=None):
            args = ["python3", str(RECOVERY), mode, "--context", "fixture"]
            if key:
                args += ["--keyring-file", str(key)]
            if directory:
                args += ["--output-directory", str(directory)]
            return invoke(args, success, extra)

        try:
            require(run(["docker", "image", "inspect", image]).returncode == 0, "fixture image absent")
            result = run(["docker", "run", "-d", "--pull=never", "--network=none", "--name", name,
                          "--tmpfs", "/run", "--entrypoint", "/bin/k3s", image, "server", "--disable-agent",
                          "--disable=traefik,servicelb,metrics-server,coredns,local-storage",
                          "--disable-helm-controller", "--disable-network-policy", "--flannel-backend=none",
                          "--node-ip=127.0.0.1", "--bind-address=127.0.0.1", "--advertise-address=127.0.0.1"])
            require(result.returncode == 0, "fixture container start failed")
            created = True
            volumes = [m["Name"] for m in json.loads(run(["docker", "inspect", name]).stdout)[0]["Mounts"] if m["Type"] == "volume"]
            deadline = time.monotonic() + 60
            while kube(name, ["get", "--raw=/readyz"]).returncode:
                require(time.monotonic() < deadline, "fixture API readiness timeout")
                time.sleep(1)
            for namespace in (RETAINED, "kodex-system"):
                require(kube(name, ["create", "namespace", namespace]).returncode == 0, "fixture namespace create failed")
            bootstrap("ensure", first)
            original = get(name, "secret", KEYRING, "kodex-system")
            guard = get(name, "configmap", GUARD)
            recovery("preserve")
            summary = json.loads(run([str(binary), "check", "--input-file", str(first)]).stdout)
            backup_name = "draft-key-backup-r1-" + summary["digest"][:32]
            backup = get(name, "secret", backup_name)
            require(backup["immutable"] is True and backup["metadata"]["labels"]["kodex.dev/purpose"] == "secret-draft-key-backup",
                    "retained backup purpose or immutability mismatch")
            bootstrap("ensure", first)
            recovery("preserve")
            require(get(name, "secret", KEYRING, "kodex-system") == original and get(name, "configmap", GUARD) == guard and get(name, "secret", backup_name) == backup,
                    "replay changed serving, guard or backup")
            changed = copy.deepcopy(backup)
            changed["data"]["keyring.json"] = base64.b64encode(wrong.read_bytes()).decode()
            require(kube(name, ["replace", "-f", "-"], json.dumps(changed).encode()).returncode != 0,
                    "API allowed immutable backup data mutation")
            # Immutable защищает data, но purpose/namespace UID проверяет сам helper.
            for patch in ({"metadata": {"labels": {"kodex.dev/purpose": "secret-draft"}}},
                          {"metadata": {"annotations": {"kodex.dev/retained-namespace-uid": "wrong-uid"}}}):
                require(kube(name, ["-n", RETAINED, "patch", "secret", backup_name, "--type=merge", "-p", json.dumps(patch)]).returncode == 0,
                        "fixture backup metadata mutation failed")
                recovery("preserve", success=False)
                restore = {"metadata": {"labels": backup["metadata"]["labels"], "annotations": backup["metadata"]["annotations"]}}
                require(kube(name, ["-n", RETAINED, "patch", "secret", backup_name, "--type=merge", "-p", json.dumps(restore)]).returncode == 0,
                        "fixture backup metadata restoration failed")
            backup = get(name, "secret", backup_name)
            recovery("validate-restore", wrong, success=False)
            for field in ("UID", "resourceVersion"):
                bootstrap("rotate", second, success=False, extra={"DRAFT_API_RACE": field})
                require(get(name, "secret", KEYRING, "kodex-system")["data"] == original["data"], "stale write changed serving keys")
                require(get(name, "configmap", GUARD)["data"] == guard["data"], "stale write changed guard")
            bootstrap("rotate", second)
            rotated_guard = get(name, "configmap", GUARD)
            require(json.loads(rotated_guard["data"]["state.json"])["uses"][:1] == json.loads(guard["data"]["state.json"])["uses"], "rotation reset uses")
            require(get(name, "secret", backup_name) == backup, "rotation changed retained old backup")
            recovery("preserve")
            # Настоящее пересоздание source namespace; retained namespace не удаляется.
            retained_uid = get(name, "namespace", RETAINED)["metadata"]["uid"]
            require(kube(name, ["delete", "namespace", "kodex-system", "--wait=true", "--timeout=25s"]).returncode == 0,
                    "fixture serving namespace deletion failed")
            require(kube(name, ["create", "namespace", "kodex-system"]).returncode == 0,
                    "fixture serving namespace replacement failed")
            require(get(name, "namespace", RETAINED)["metadata"]["uid"] == retained_uid,
                    "retained namespace was replaced")
            recovery("preserve")
            export_dir = temporary / "export"
            export_dir.mkdir(mode=0o700)
            exported = Path(recovery("export", directory=export_dir).decode().strip())
            require(exported.parent == export_dir and exported.read_bytes() == second.read_bytes(), "export bytes mismatch")
            bootstrap("restore", first, success=False)
            bootstrap("restore", exported)
            restored = get(name, "secret", KEYRING, "kodex-system")
            require(restored["metadata"]["uid"] != original["metadata"]["uid"] and base64.b64decode(restored["data"]["keyring.json"]) == second.read_bytes(), "restore did not create exact projection")
            bootstrap("restore", exported)
            require(get(name, "configmap", GUARD)["data"] == rotated_guard["data"], "restore changed durable guard uses")
            for field in ("UID", "resourceVersion"):
                marker = temporary / ("preserve-race-" + field)
                before = get(name, "secret", KEYRING, "kodex-system")
                recovery("preserve", success=False, extra={"DRAFT_API_PRESERVE_RACE": field,
                                                          "DRAFT_API_RACE_MARKER": str(marker)})
                require(marker.exists(), "preserve race was not reached")
                after = get(name, "secret", KEYRING, "kodex-system")
                require(after["data"] == before["data"], "preserve race changed key bytes")
                recovery("preserve")
            invalid_guard = {"metadata": {"labels": {"kodex.dev/purpose": "other-purpose"}}}
            require(kube(name, ["-n", RETAINED, "patch", "configmap", GUARD, "--type=merge", "-p", json.dumps(invalid_guard)]).returncode == 0,
                    "fixture guard mutation failed")
            recovery("preserve", success=False)
            bootstrap("restore", exported, success=False)
            require(get(name, "secret", KEYRING, "kodex-system")["data"] == restored["data"], "invalid guard changed serving material")
            print("Secret draft recovery actual API fixtures passed; broker consumer and live NOT RUN")
        finally:
            if created:
                require(run(["docker", "rm", "-fv", name]).returncode == 0, "fixture cleanup failed")
                require(all(run(["docker", "volume", "inspect", volume]).returncode != 0 for volume in volumes), "fixture volume retained")


if __name__ == "__main__":
    try:
        if len(sys.argv) > 1 and sys.argv[1] == "--kubectl":
            kubectl_wrapper()
        else:
            main()
    except AssertionError as error:
        print("Secret draft recovery actual API fixture failed: " + str(error), file=sys.stderr)
        sys.exit(1)
    except Exception:
        print("Secret draft recovery actual API fixture failed", file=sys.stderr)
        sys.exit(1)
