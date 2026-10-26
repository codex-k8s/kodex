#!/usr/bin/env python3
"""Проверяет полный environment render с synthetic lock; не обращается к cluster/target."""

import base64
import hashlib
import json
import os
from pathlib import Path
import struct
import subprocess
import tempfile


def run(arguments, root):
    """Выполняет bounded локальные генераторы с одним Go compiler slot."""
    result = subprocess.run(arguments, cwd=root, env={**os.environ, "GOMAXPROCS": "2", "GOFLAGS": "-p=1", "GOWORK": "off"},
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=120, check=False)
    if result.returncode:
        raise RuntimeError("synthetic environment render failed: " + (result.stdout + result.stderr).decode(errors="replace")[-12000:])
    return result.stdout


def main():
    """Материализует оба профиля и проверяет exact SSH egress и CP-only signing projection."""
    root = Path(__file__).resolve().parents[2]
    components = json.loads((root / "tools/release/images.json").read_text())["images"]
    with tempfile.TemporaryDirectory(prefix="kodex-ssh-render-") as temporary:
        directory = Path(temporary)
        connections = []
        for index, address in enumerate(("8.8.8.8", "2606:4700:4700::1111")):
            public = struct.pack(">I", 11) + b"ssh-ed25519" + struct.pack(">I", 32) + bytes([1] * 32)
            configuration = {"project_ref": "prj_fixture_1838", "owner": "example", "repository": "project", "address": address,
                             "user": "kdx_fixture", "host_key": "ssh-ed25519 " + base64.b64encode(public).decode(), "published_port": "8080",
                             "runtime_image": "docker.io/library/python@sha256:" + "a" * 64, "ssh_secret_ref": "sec_fixture_1838"}
            path = directory / ("connection-" + str(index) + ".json")
            path.write_text(json.dumps(configuration))
            connections += ["--ssh-compose-connection", str(path)]
        failures = []
        for profile in ("web-only", "web-with-mattermost"):
            lock = {"schema_version": 2, "profile": profile, "source_sha": "b" * 40, "build_run_id": "local",
                    "registry": {"push": "push.fixture.example", "node_pull": "pull.fixture.example", "repository_prefix": "kodex"},
                    "images": [{"component": item["component"], "repository": "kodex/" + item["component"], "digest": "sha256:" + "c" * 64,
                                "pull_ref": "pull.fixture.example/kodex/" + item["component"] + "@sha256:" + "c" * 64} for item in components],
                    "external_images": [{"component": "admission-tools", "digest": "sha256:" + "d" * 64, "pull_ref": "tools.fixture.example/admission@sha256:" + "d" * 64}],
                    "role_image_input": {"repository": "kodex/role-image-inputs", "manifest_digest": "sha256:" + "e" * 64,
                                         "payload_sha256": "f" * 64, "source_sha256": "a" * 64,
                                         "pull_ref": "pull.fixture.example/kodex/role-image-inputs@sha256:" + "e" * 64}}
            lock_path, output = directory / "lock.json", directory / "render.yaml"
            lock_path.write_text(json.dumps(lock))
            arguments = [str(root / "tools/release/render-web-only.sh"), "--lock", str(lock_path), "--lock-sha256", hashlib.sha256(lock_path.read_bytes()).hexdigest(),
                         "--output", str(output), "--profile", profile, "--public-host", "kodex.fixture.example", "--public-origin", "https://kodex.fixture.example",
                         "--oidc-issuer", "https://sso.fixture.example/realms/fixture", "--oidc-jwks-url", "https://sso.fixture.example/realms/fixture/protocol/openid-connect/certs",
                         "--oidc-connect-address", "keycloak.kodex-identity.svc:8443", "--oidc-tls-server-name", "sso.fixture.example",
                         "--promoted-pull-host", "pull.fixture.example", "--kubernetes-api-service-cidr", "10.43.0.1/32",
                         "--kubernetes-api-endpoint-cidrs", "192.0.2.11/32", "--kubernetes-api-endpoint-ports", "6443",
                         "--ingress-class", "nginx", "--cluster-issuer", "fixture-issuer", "--ingress-namespace", "ingress-nginx", "--ingress-pod-name", "ingress-nginx",
                         "--oidc-namespace", "kodex-identity", "--oidc-pod-name", "keycloak", "--oidc-pod-component", "identity", "--oidc-target-port", "8443"] + connections
            if profile == "web-with-mattermost":
                arguments += ["--mattermost-host", "mattermost.fixture.example"]
            try:
                run(arguments, root)
            except RuntimeError as error:
                # Render содержит только synthetic metadata; выводим bounded причину fail-closed.
                zero_pins = [line.strip()[:320] for line in output.read_text().splitlines() if "sha256:" + "0" * 64 in line] if output.exists() else []
                failures.append(str(error) + " remaining synthetic render zero pins: " + json.dumps(zero_pins[:8]))
            documents = [json.loads(line) for line in run(["yq", "-o=json", "-I=0", ".", str(output)], root).decode().splitlines() if line.strip() != "null"]
            identities = [(item["apiVersion"], item["kind"], item["metadata"].get("namespace", ""), item["metadata"]["name"]) for item in documents]
            assert len(identities) == len(set(identities)), "duplicate environment resources"
            ssh_rules = []
            for item in documents:
                if item["kind"] == "NetworkPolicy":
                    for rule in item["spec"].get("egress", []):
                        if any(port.get("port") == 22 for port in rule.get("ports", [])):
                            assert item["spec"]["podSelector"]["matchLabels"] == {"app.kubernetes.io/name": "integration-gateway", "app.kubernetes.io/component": "integration-worker"}
                            assert rule["ports"] == [{"protocol": "TCP", "port": 22}]
                            ssh_rules.append(rule["to"])
                if item["kind"] == "Deployment":
                    pod = item["spec"]["template"]["spec"]
                    signing = [volume for volume in pod.get("volumes", []) if volume.get("secret", {}).get("secretName") == "kodex-ssh-compose-signing"]
                    if item["metadata"]["name"] == "control-plane":
                        assert len(signing) == 1 and signing[0]["secret"]["defaultMode"] == 0o440
                        main = next(container for container in pod["containers"] if container["name"] == "control-plane")
                        assert {"name": "CONTROL_PLANE_SSH_COMPOSE_SIGNING_KEY_FILE", "value": "/var/run/secrets/kodex/ssh-compose-signing/private-key"} in main["env"]
                        assert any(mount["name"] == signing[0]["name"] and mount["readOnly"] for mount in main["volumeMounts"])
                    else:
                        assert not signing, "owner signing key projected outside CP"
            assert sorted(json.dumps(rule, sort_keys=True) for rule in ssh_rules) == sorted(json.dumps([{"ipBlock": {"cidr": cidr}}], sort_keys=True) for cidr in ("8.8.8.8/32", "2606:4700:4700::1111/128"))
            print("SSH Compose environment boundary passed:", profile, flush=True)
        if failures:
            raise RuntimeError("full environment render failed despite SSH boundary checks: " + " | ".join(failures))
        print("SSH Compose full environment render passed for both profiles")


if __name__ == "__main__":
    main()
