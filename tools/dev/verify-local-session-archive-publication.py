#!/usr/bin/env python3
"""Проверяет HTTPS publication; отдельный opt-in runner pull добавляет только CRI cache."""

import argparse
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import selectors
import subprocess
import tempfile
import time
import uuid

DIGEST = re.compile(r"sha256:[a-f0-9]{64}\Z")
PIN = re.compile(r"([a-z0-9][a-z0-9.-]*\.[a-z0-9.-]+)/kodex/session-archive@(sha256:[a-f0-9]{64})\Z")
HOST = re.compile(r"(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\Z")
RUNNER_ENTRYPOINT = ["/usr/local/bin/kodex-init", "entrypoint", "/usr/local/bin/kodex-agent-runner"]
CRI_CLIENT = "/bin/crictl"
MAX_JSON = 4 << 20
MAX_BLOB = 8 << 30
MAX_TOTAL = 32 << 30
MANIFEST = "application/vnd.oci.image.manifest.v1+json"
INDEX = "application/vnd.oci.image.index.v1+json"
CONFIG = "application/vnd.oci.image.config.v1+json"
LAYERS = {"application/vnd.oci.image.layer.v1.tar", "application/vnd.oci.image.layer.v1.tar+gzip"}
TLS_PATHS = {"ca_file": "/etc/rancher/k3s/kodex-registry/ca.crt",
             "cert_file": "/etc/rancher/k3s/kodex-registry/client.crt",
             "key_file": "/etc/rancher/k3s/kodex-registry/client.key"}
STAGES = frozenset({"UNKNOWN", "STATE", "RENDER", "LIVE_DEPLOYMENT", "TOOLS_IMAGE",
                    "NODE_INVENTORY", "NODE_CONTAINER", "FILE_METADATA", "NODE_CONFIG",
                    "DNS", "NODE_ROUTE", "TLS_READ", "HTTPS_GRAPH", "POLICY", "CRI_CLIENT", "CRI_PULL", "CRI_READBACK"})
active_stage = "UNKNOWN"
cri_attempted = False


def enter_stage(stage):
    global active_stage
    active_stage = stage if stage in STAGES else "UNKNOWN"


class Failure(Exception):
    def __init__(self, code):
        super().__init__(code)
        self.stage = active_stage


def require(ok, code):
    if not ok:
        raise Failure(code)


def pairs(items):
    result = {}
    for key, value in items:
        require(key not in result, "JSON_INVALID")
        result[key] = value
    return result


def document(raw):
    require(len(raw) <= MAX_JSON, "JSON_BOUND_EXCEEDED")
    try:
        return json.loads(raw, object_pairs_hook=pairs)
    except (ValueError, UnicodeError):
        raise Failure("JSON_INVALID") from None


def capture(argv, deadline, data=None, maximum_seconds=20):
    # Вывод дочерних процессов никогда не становится диагностическим текстом.
    try:
        require(type(maximum_seconds) is int and 1 <= maximum_seconds <= 360, "BUDGET_INVALID")
        end = min(deadline, time.monotonic()+maximum_seconds)
        require(data is None or len(data) <= MAX_JSON, "COMMAND_BOUND_EXCEEDED")
        output, offset = bytearray(), 0
        with subprocess.Popen(argv, stdin=subprocess.PIPE if data is not None else subprocess.DEVNULL,
                              stdout=subprocess.PIPE, stderr=subprocess.DEVNULL) as process:
            with selectors.DefaultSelector() as ready:
                ready.register(process.stdout, selectors.EVENT_READ)
                if data is not None:
                    ready.register(process.stdin, selectors.EVENT_WRITE)
                try:
                    while ready.get_map():
                        require(time.monotonic() < end, "COMMAND_TIMEOUT")
                        for key, _event in ready.select(min(1, max(.01, end-time.monotonic()))):
                            if key.fileobj is process.stdin:
                                if offset < len(data):
                                    offset += os.write(process.stdin.fileno(), data[offset:offset+4096])
                                if offset == len(data):
                                    ready.unregister(process.stdin)
                                    process.stdin.close()
                            else:
                                chunk = os.read(process.stdout.fileno(), 65536)
                                if not chunk:
                                    ready.unregister(process.stdout)
                                output.extend(chunk)
                                require(len(output) <= MAX_JSON, "COMMAND_BOUND_EXCEEDED")
                    require(process.wait(timeout=max(.01, end-time.monotonic())) == 0, "COMMAND_FAILED")
                finally:
                    if process.poll() is None:
                        process.kill()
    except (OSError, subprocess.TimeoutExpired):
        raise Failure("COMMAND_UNAVAILABLE") from None
    return bytes(output)


def deployment_pin(deployment):
    require(deployment.get("kind") == "Deployment" and
            deployment.get("metadata", {}).get("name") == "session-archive" and
            deployment.get("metadata", {}).get("namespace") == "kodex-system", "DEPLOYMENT_INVALID")
    template = deployment["spec"]["template"]
    require(template.get("metadata", {}).get("labels", {}).get("kodex.dev/local-profile") == "hot-reload", "PROFILE_INVALID")
    containers = [c for c in template["spec"]["containers"] if c.get("name") == "session-archive"]
    require(len(containers) == 1, "DEPLOYMENT_INVALID")
    pins = [e.get("value") for e in containers[0].get("env", []) if e.get("name") == "SESSION_ARCHIVE_WORKER_IMAGE"]
    require(len(pins) == 1 and isinstance(pins[0], str) and PIN.fullmatch(pins[0]), "PIN_INVALID")
    return pins[0]


def credentials(config, host):
    require(config.get("mirrors", {}).get(host, {}).get("endpoint") == ["https://"+host], "TLS_ROUTE_INVALID")
    entry = config.get("configs", {}).get(host, {})
    require(entry.get("tls") == TLS_PATHS, "TLS_ROUTE_INVALID")
    auth = entry.get("auth", {})
    require(set(auth) == {"username", "password"}, "APPLICATION_IDENTITY_INVALID")
    for value in auth.values():
        require(isinstance(value, str) and 0 < len(value) <= 8192 and
                all(32 <= ord(c) <= 126 for c in value), "APPLICATION_IDENTITY_INVALID")
    require(":" not in auth["username"], "APPLICATION_IDENTITY_INVALID")
    return auth


def valid_node_file_metadata(metadata, operator_uid):
    # docker cp штатного installer может сохранить UID владельца material.
    # Допускаются только root/текущий оператор; private key/auth остаются 0600.
    return metadata in {"regular file:0:600", "regular file:"+str(operator_uid)+":600"}


def node_host_route(raw, host, expected_address):
    require(0 < len(raw) <= 65536 and all(value in (9, 10) or 32 <= value <= 126 for value in raw), "HOST_ROUTE_INVALID")
    lines = raw.decode("ascii").splitlines()
    require(len(lines) <= 1024, "HOST_ROUTE_INVALID")
    matches = []
    for line in lines:
        fields = line.split("#", 1)[0].split()
        if host in fields:
            require(len(fields) == 2 and fields[1] == host, "HOST_ROUTE_INVALID")
            try:
                address = ipaddress.IPv4Address(fields[0])
            except ValueError:
                raise Failure("HOST_ROUTE_INVALID") from None
            require(not address.is_loopback and not address.is_unspecified and not address.is_multicast and
                    str(address) == expected_address, "HOST_ROUTE_INVALID")
            matches.append(str(address))
    require(len(matches) == 1, "HOST_ROUTE_INVALID")
    return matches[0]


def quote(value):
    return '"'+value.replace("\\", "\\\\").replace('"', '\\"')+'"'


class NodeReader:
    def __init__(self, node, image, directory, host, address, auth, deadline, component="session-archive"):
        require(component in {"session-archive", "runner"}, "COMPONENT_INVALID")
        self.repository = "agent-runner" if component == "runner" else "session-archive"
        self.node, self.image, self.directory = node, image, directory
        self.host, self.deadline = host, deadline
        lines = ['silent', 'fail', 'proto = "=https"', 'proxy = ""', 'tlsv1.3', 'connect-timeout = 5',
                 'max-time = 60', 'cacert = "/work/ca_file"', 'cert = "/work/cert_file"',
                 'key = "/work/key_file"', 'resolve = '+quote(host+":443:"+address),
                 'user = '+quote(auth["username"]+":"+auth["password"]),
                 'header = '+quote("Accept: "+MANIFEST+", "+INDEX)]
        path = directory / "curl.conf"
        path.write_text("\n".join(lines)+"\n")
        path.chmod(0o600)

    def read(self, kind, digest, size=None):
        require(kind in {"manifests", "blobs"} and DIGEST.fullmatch(digest), "DESCRIPTOR_INVALID")
        maximum = MAX_JSON if kind == "manifests" else size
        require(isinstance(maximum, int) and 0 < maximum <= MAX_BLOB, "DESCRIPTOR_INVALID")
        cidfile = self.directory / ("reader-"+uuid.uuid4().hex+".cid")
        argv = ["docker", "run", "--rm", "--cidfile", str(cidfile), "--pull=never", "--read-only", "--cap-drop=ALL",
                "--security-opt=no-new-privileges", "--network=container:"+self.node,
                "--user="+str(os.getuid())+":"+str(os.getgid()),
                "--mount", "type=bind,src="+str(self.directory)+",dst=/work,readonly",
                "--entrypoint=/usr/bin/curl", self.image, "--config", "/work/curl.conf",
                "https://"+self.host+"/v2/kodex/"+self.repository+"/"+kind+"/"+digest]
        sha, count, raw = hashlib.sha256(), 0, bytearray()
        try:
            with subprocess.Popen(argv, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL) as process:
                with selectors.DefaultSelector() as ready:
                    ready.register(process.stdout, selectors.EVENT_READ)
                    try:
                        while True:
                            require(time.monotonic() < self.deadline, "BUDGET_EXCEEDED")
                            if not ready.select(min(1, max(.01, self.deadline-time.monotonic()))):
                                continue
                            chunk = os.read(process.stdout.fileno(), 65536)
                            if not chunk:
                                break
                            count += len(chunk)
                            require(count <= maximum, "RESPONSE_BOUND_EXCEEDED")
                            sha.update(chunk)
                            if kind == "manifests" or size <= MAX_JSON:
                                raw.extend(chunk)
                        require(process.wait(timeout=max(.01, self.deadline-time.monotonic())) == 0, "HTTPS_READ_FAILED")
                    finally:
                        if process.poll() is None:
                            process.kill()
                require(sha.hexdigest() == digest[7:] and (size is None or count == size), "DIGEST_MISMATCH")
                return bytes(raw)
        except (OSError, subprocess.TimeoutExpired):
            raise Failure("HTTPS_READ_FAILED") from None
        finally:
            # Удаляется только собственный одноразовый reader, никогда node/image.
            if cidfile.is_file() and not cidfile.is_symlink():
                identity = cidfile.read_text().strip()
                if re.fullmatch(r"[a-f0-9]{64}", identity):
                    try:
                        subprocess.run(["docker", "container", "rm", "--force", identity],
                                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=5)
                    except (OSError, subprocess.TimeoutExpired):
                        pass
                cidfile.unlink()


def verify_graph(reader, digest, component="session-archive", details=None):
    require(component in {"session-archive", "runner"}, "COMPONENT_INVALID")
    count, total, seen = 0, 0, set()

    def descriptor(value, media):
        require(isinstance(value, dict) and value.get("mediaType") in media and
                isinstance(value.get("digest"), str) and DIGEST.fullmatch(value["digest"]) and
                type(value.get("size")) is int and 0 < value["size"] <= MAX_BLOB and
                "urls" not in value and "data" not in value, "DESCRIPTOR_INVALID")
        return value["digest"], value["size"]

    def manifest(value, expected_size=None):
        nonlocal count, total
        require(value not in seen and count < 3, "GRAPH_INVALID")
        seen.add(value)
        raw = reader.read("manifests", value)
        require(expected_size is None or len(raw) == expected_size, "DIGEST_MISMATCH")
        data = document(raw)
        require(isinstance(data, dict) and data.get("schemaVersion") == 2, "GRAPH_INVALID")
        count += 1
        if data.get("mediaType") == INDEX:
            require(count == 1 and isinstance(data.get("manifests"), list) and len(data["manifests"]) == 1, "GRAPH_INVALID")
            item = data["manifests"][0]
            child, size = descriptor(item, {MANIFEST})
            require(item.get("platform") == {"os": "linux", "architecture": "amd64"}, "PLATFORM_INVALID")
            manifest(child, size)
            return
        require(data.get("mediaType") == MANIFEST and isinstance(data.get("layers"), list) and
                1 <= len(data["layers"]) <= 128, "GRAPH_INVALID")
        config_digest, config_size = descriptor(data.get("config"), {CONFIG})
        require(config_size <= MAX_JSON, "JSON_BOUND_EXCEEDED")
        cfg = document(reader.read("blobs", config_digest, config_size))
        require(cfg.get("os") == "linux" and cfg.get("architecture") == "amd64" and
                cfg.get("config", {}).get("Entrypoint") == (RUNNER_ENTRYPOINT if component == "runner" else
                    ["/usr/local/bin/session-archive"]), "PLATFORM_INVALID")
        if details is not None:
            details["configDigest"] = config_digest
        for layer in data["layers"]:
            layer_digest, layer_size = descriptor(layer, LAYERS)
            total += layer_size
            require(total <= MAX_TOTAL, "GRAPH_BOUND_EXCEEDED")
            reader.read("blobs", layer_digest, layer_size)

    manifest(digest)
    return count


def runner_policy_pin(policy):
    metadata, data = policy.get("metadata", {}), policy.get("data", {})
    require(policy.get("apiVersion") == "v1" and policy.get("kind") == "ConfigMap" and
            metadata.get("namespace") == "kodex-system" and policy.get("immutable") is True and
            re.fullmatch(r"[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}", metadata.get("uid", "")) and
            re.fullmatch(r"[1-9][0-9]*", metadata.get("resourceVersion", "")) and
            metadata.get("labels", {}).get("app.kubernetes.io/part-of") == "kodex" and
            metadata.get("labels", {}).get("kodex.dev/owner-intent") == "true", "POLICY_INVALID")
    require(isinstance(data, dict) and all(isinstance(k, str) and re.fullmatch(r"[A-Za-z][A-Za-z0-9]*", k) and
            isinstance(v, str) and all(32 <= ord(c) <= 126 for c in v) for k, v in data.items()), "POLICY_INVALID")
    canonical = {k: v for k, v in sorted(data.items()) if k not in {"orchestrationRevision", "policySHA256"}}
    checksum = hashlib.sha256((json.dumps(canonical, separators=(",", ":"))+"\n").encode()).hexdigest()
    require(data.get("policySHA256") == checksum and
            metadata.get("name") in {"kodex-image-admission-policy", "kodex-image-admission-policy-"+checksum[:32]}, "POLICY_DIGEST_INVALID")
    host, digest, generation = data.get("pullRegistryHost", ""), data.get("trustedRoleBaseDigest", ""), data.get("pullCredentialGeneration", "")
    require(len(host) <= 253 and HOST.fullmatch(host) and DIGEST.fullmatch(digest) and digest != "sha256:"+"0"*64 and
            data.get("promotedPullRepository") == host+"/kodex/roles" and
            re.fullmatch(r"[1-9][0-9]*", generation) and int(generation) <= 9007199254740991, "POLICY_PULL_TARGET_INVALID")
    pin = host+"/kodex/agent-runner@"+digest
    require(data.get("nodeReadbackImage") == pin, "POLICY_PULL_TARGET_INVALID")
    return pin, host, digest


def active_runner_policy(context, deadline):
    def get(kind, name):
        return document(capture(["kubectl", "--context", context, "--request-timeout=10s", "-n", "kodex-system",
            "get", kind+"/"+name, "-o=json"], deadline))
    deployment = get("deployment", "image-admission-controller")
    labels = deployment.get("spec", {}).get("template", {}).get("metadata", {}).get("labels", {})
    staging_profile = labels.get("kodex.dev/local-profile") == "hot-reload" or (
        labels.get("kodex.dev/local-profile") == "stable" and
        labels.get("kodex.dev/environment") == "staging" and
        labels.get("app.kubernetes.io/part-of") == "kodex")
    require(deployment.get("kind") == "Deployment" and deployment.get("metadata", {}).get("namespace") == "kodex-system" and
            deployment.get("metadata", {}).get("name") == "image-admission-controller" and
            staging_profile, "POLICY_SELECTOR_INVALID")
    containers = [c for c in deployment["spec"]["template"]["spec"]["containers"] if c.get("name") == "image-admission-controller"]
    require(len(containers) == 1, "POLICY_SELECTOR_INVALID")
    entries = [e for e in containers[0].get("env", []) if e.get("name") == "IMAGE_ADMISSION_CONTROLLER_POLICY_CONFIG_MAP"]
    require(len(entries) == 1 and set(entries[0]) == {"name", "value"} and
            re.fullmatch(r"kodex-image-admission-policy(?:-[a-f0-9]{32})?", entries[0].get("value", "")), "POLICY_SELECTOR_INVALID")
    policy = get("configmap", entries[0]["value"])
    require(policy.get("metadata", {}).get("name") == entries[0]["value"], "POLICY_SELECTOR_INVALID")
    runner_policy_pin(policy)
    return policy


def node_snapshot(node, host, load_balancer_address, deadline, strict=False):
    enter_stage("NODE_CONTAINER")
    info = document(capture(["docker", "inspect", node], deadline))
    require(len(info) == 1 and info[0].get("State", {}).get("Running") is True and
            (not strict or re.fullmatch(r"[a-f0-9]{64}", info[0].get("Id", ""))) and
            info[0].get("Config", {}).get("Labels", {}).get("k3d.cluster") == "kodex" and
            info[0].get("Config", {}).get("Labels", {}).get("k3d.role") == ("server" if "-server-" in node else "agent"), "NODE_IDENTITY_INVALID")
    enter_stage("FILE_METADATA")
    for path in ["/etc/rancher/k3s/registries.yaml", *TLS_PATHS.values()]:
        metadata = capture(["docker", "exec", node, "stat", "-c", "%F:%u:%a", path], deadline).decode().strip()
        require(valid_node_file_metadata(metadata, os.getuid()), "NODE_IDENTITY_INVALID")
    enter_stage("NODE_CONFIG")
    raw = capture(["docker", "exec", node, "cat", "/etc/rancher/k3s/registries.yaml"], deadline)
    config = document(capture(["yq", "-o=json", "-I=0", "."], deadline, raw))
    auth = credentials(config, host)
    enter_stage("NODE_ROUTE")
    require(capture(["docker", "exec", node, "stat", "-c", "%F", "/etc/hosts"], deadline).decode().strip() == "regular file", "HOST_ROUTE_INVALID")
    hosts = capture(["docker", "exec", node, "cat", "/etc/hosts"], deadline)
    address = node_host_route(hosts, host, load_balancer_address)
    enter_stage("TLS_READ")
    tls = {}
    for key, path in TLS_PATHS.items():
        data = capture(["docker", "exec", node, "cat", path], deadline)
        require(0 < len(data) <= 64 << 10, "TLS_MATERIAL_INVALID")
        tls[key] = data
    fingerprint = hashlib.sha256(json.dumps({"id": info[0].get("Id"), "config": hashlib.sha256(raw).hexdigest(),
        "hosts": hashlib.sha256(hosts).hexdigest(), "tls": {k: hashlib.sha256(v).hexdigest() for k, v in tls.items()}}, sort_keys=True).encode()).hexdigest()
    return auth, address, tls, fingerprint, info[0].get("Id")


def cri_client(container_id, deadline):
    # В k3d установлен отдельный cri-tools applet, не подкоманда k3s. Ни PATH, ни caller argv не используются.
    enter_stage("CRI_CLIENT")
    require(re.fullmatch(r"[a-f0-9]{64}", container_id), "CRI_TARGET_INVALID")
    argv = ["docker", "exec", container_id, CRI_CLIENT]
    version = capture([*argv, "--version"], deadline).decode("ascii").strip()
    require(re.fullmatch(r"crictl (?:version|github\.com/k3s-io/containerd/v2) v1\.[0-9]+\.[0-9]+(?:-k3s[1-9][0-9]*)?", version), "CRI_CLIENT_INVALID")
    for command, flags in (([], ("--runtime-endpoint", "--image-endpoint")),
                           (["pull"], ("--pull-timeout",)), (["inspecti"], ("--output",))):
        help_text = capture([*argv, *command, "--help"], deadline).decode("ascii")
        require(0 < len(help_text) <= 64 << 10 and all(c in "\n\r\t" or 32 <= ord(c) <= 126 for c in help_text) and
                ("crictl "+command[0] if command else "crictl - client for CRI") in help_text and
                all(re.search(r"^\s+"+re.escape(flag)+r"(?:[ ,=]|$)", help_text, re.MULTILINE) for flag in flags), "CRI_CLIENT_INVALID")
    return version


def cri_pull(node, container_id, pin, config_digest, deadline):
    global cri_attempted
    require(re.fullmatch(r"k3d-kodex-(server|agent)-[0-9]+", node) and
            re.fullmatch(r"[a-f0-9]{64}", container_id) and
            re.fullmatch(r"[a-z0-9.-]+/kodex/agent-runner@sha256:[a-f0-9]{64}", pin) and
            DIGEST.fullmatch(config_digest), "CRI_TARGET_INVALID")
    remaining = int(deadline-time.monotonic())
    require(1 <= remaining <= 360, "BUDGET_EXCEEDED")
    enter_stage("CRI_PULL")
    cri_attempted = True
    print(json.dumps({"status": "INTENT", "evidence": "NODE_CRI_PULL", "node": node,
        "digest": pin.split("@")[1], "cacheWrites": True}), flush=True)
    # Контейнер закреплён ID, CRI endpoint — штатным k3s socket; caller/окружение их не выбирают.
    argv = ["docker", "exec", container_id, CRI_CLIENT,
        "--runtime-endpoint=unix:///run/k3s/containerd/containerd.sock",
        "--image-endpoint=unix:///run/k3s/containerd/containerd.sock"]
    capture([*argv, "pull", "--pull-timeout="+str(remaining)+"s", pin], deadline, maximum_seconds=remaining)
    enter_stage("CRI_READBACK")
    result = document(capture([*argv, "inspecti", "--output=json", pin], deadline))
    status = result.get("status", {})
    size = status.get("size")
    valid_size = (type(size) is int and 0 < size <= MAX_TOTAL) or (isinstance(size, str) and
        re.fullmatch(r"[1-9][0-9]{0,10}", size) and int(size) <= MAX_TOTAL)
    require(status.get("id") == config_digest and isinstance(status.get("repoDigests"), list) and
            pin in status["repoDigests"] and valid_size, "CRI_READBACK_MISMATCH")
    print(json.dumps({"status": "PASS", "evidence": "NODE_CRI_PULL", "node": node,
        "digest": pin.split("@")[1], "imageID": config_digest, "cacheWrites": True, "runtimeELF": "NOT_CHECKED"}), flush=True)


def main():
    parser = argparse.ArgumentParser(description="Closed archive/runner node HTTPS graph verification. Runner-only explicit CRI pull adds image cache; no Pods/config/PVC writes or runtime ELF claims.")
    parser.add_argument("--component", choices=["session-archive", "runner"], default="session-archive")
    parser.add_argument("--context", required=True, choices=["k3d-kodex"])
    parser.add_argument("--state-directory", type=Path, required=True)
    parser.add_argument("--render", type=Path)
    parser.add_argument("--timeout", type=int, default=360)
    parser.add_argument("--cri-pull", action="store_true")
    parser.add_argument("--confirm")
    args = parser.parse_args()
    enter_stage("STATE")
    require((args.cri_pull and args.component == "runner" and args.confirm == "PULL-EXACT-LOCAL-RUNNER") or
            (not args.cri_pull and args.confirm is None), "CRI_CONFIRMATION_INVALID")
    require(1 <= args.timeout <= 360, "BUDGET_INVALID")
    deadline = time.monotonic()+args.timeout
    require(os.environ.get("KUBECONFIG") == "/home/s/.kube/config", "KUBECONFIG_INVALID")
    require(args.state_directory.is_absolute() and args.state_directory.is_dir() and not args.state_directory.is_symlink(), "INPUT_INVALID")
    if args.component == "runner":
        require(args.render is None, "RUNNER_ARGUMENTS_INVALID")
        source = args.state_directory/"agent-runner-image"
        require(source.is_file() and not source.is_symlink() and source.stat().st_size <= 1024, "INPUT_INVALID")
        source_pin = source.read_text().strip()
        require(re.fullmatch(r"registry\.local\.kodex/kodex/agent-runner@sha256:[a-f0-9]{64}", source_pin), "PIN_INVALID")
        enter_stage("POLICY")
        policy = active_runner_policy(args.context, deadline)
        pin, host, digest = runner_policy_pin(policy)
        require(source_pin.split("@")[1] == digest, "PIN_MISMATCH")
    else:
        require(args.render is not None and args.render.is_absolute() and args.render.is_file() and not args.render.is_symlink(), "INPUT_INVALID")
        source_pin = (args.state_directory/"session-archive-image").read_text().strip()
        require(re.fullmatch(r"registry\.local\.kodex/kodex/session-archive@sha256:[a-f0-9]{64}", source_pin), "PIN_INVALID")
        enter_stage("RENDER")
        rendered = document(capture(["yq", "-o=json", "-I=0", 'select(.kind == "Deployment" and .metadata.name == "session-archive")', str(args.render)], deadline))
        pin = deployment_pin(rendered)
        host, digest = PIN.fullmatch(pin).groups()
        require(digest != "sha256:"+"0"*64 and source_pin.split("@")[1] == digest, "PIN_MISMATCH")
        enter_stage("LIVE_DEPLOYMENT")
        with tempfile.TemporaryDirectory(prefix="kodex-archive-kube-") as cache:
            live = document(capture(["kubectl", "--context", args.context, "--cache-dir", cache,
                                     "--request-timeout=10s", "-n", "kodex-system", "get",
                                     "deployment/session-archive", "-o=json"], deadline))
        require(deployment_pin(live) == pin, "LIVE_PIN_MISMATCH")
    enter_stage("TOOLS_IMAGE")
    tag = (args.state_directory/"image-supply-chain-tools-docker-tag").read_text().strip()
    require(re.fullmatch(r"kodex-local/image-admission-tools:[a-f0-9]{64}", tag), "TOOLS_IMAGE_INVALID")
    image = capture(["docker", "image", "inspect", "--format={{.Id}}", tag], deadline).decode().strip()
    require(DIGEST.fullmatch(image), "TOOLS_IMAGE_INVALID")
    enter_stage("NODE_INVENTORY")
    inventory = document(capture(["k3d", "node", "list", "-o", "json"], deadline))
    nodes = sorted(n["name"] for n in inventory if n.get("role") in {"server", "agent"} and re.fullmatch(r"k3d-kodex-(server|agent)-[0-9]+", n.get("name", "")))
    require(0 < len(nodes) <= 16 and len(nodes) == len(set(nodes)), "NODE_INVENTORY_INVALID")
    enter_stage("NODE_CONTAINER")
    load_balancer_address = capture(["docker", "inspect", "--format",
        '{{(index .NetworkSettings.Networks "k3d-kodex").IPAddress}}', "k3d-kodex-serverlb"], deadline).decode().strip()
    ipaddress.IPv4Address(load_balancer_address)
    verified = {}
    for node in nodes:
        auth, address, tls, node_fingerprint, container_id = node_snapshot(node, host, load_balancer_address, deadline, args.component == "runner")
        with tempfile.TemporaryDirectory(prefix="kodex-archive-https-") as name:
            directory = Path(name)
            directory.chmod(0o700)
            for key, data in tls.items():
                (directory/key).write_bytes(data)
                (directory/key).chmod(0o600)
            enter_stage("HTTPS_GRAPH")
            details = {}
            count = verify_graph(NodeReader(node, image, directory, host, address, auth, deadline, args.component), digest, args.component, details)
            verified[node] = (node_fingerprint, details["configDigest"], container_id)
        print(json.dumps({"status": "PASS", "evidence": "NODE_HTTPS_GRAPH", "node": node,
                          "digest": digest, "manifestCount": count, "routeSource": "K3D_HOSTS", "criPull": "NOT_CHECKED"}), flush=True)
    if args.cri_pull:
        # Все HTTPS graphs завершены; несовпадение до первого pull не создаёт cache effect.
        enter_stage("POLICY")
        require(active_runner_policy(args.context, deadline) == policy and source.read_text().strip() == source_pin, "POLICY_CHANGED")
        enter_stage("NODE_INVENTORY")
        require(document(capture(["k3d", "node", "list", "-o", "json"], deadline)) == inventory, "NODE_INVENTORY_CHANGED")
        enter_stage("NODE_CONTAINER")
        require(capture(["docker", "inspect", "--format",
            '{{(index .NetworkSettings.Networks "k3d-kodex").IPAddress}}', "k3d-kodex-serverlb"], deadline).decode().strip() == load_balancer_address, "NODE_ROUTE_CHANGED")
        for node in nodes:
            require(node_snapshot(node, host, load_balancer_address, deadline, True)[3] == verified[node][0], "NODE_CONFIG_CHANGED")
        # Наличие и совместимость всех node клиентов проверяются до первого cache INTENT.
        clients = {node: cri_client(verified[node][2], deadline) for node in nodes}
        for node in nodes:
            enter_stage("POLICY")
            require(active_runner_policy(args.context, deadline) == policy, "POLICY_CHANGED")
            require(node_snapshot(node, host, load_balancer_address, deadline, True)[3] == verified[node][0], "NODE_CONFIG_CHANGED")
            require(cri_client(verified[node][2], deadline) == clients[node], "CRI_CLIENT_CHANGED")
            cri_pull(node, verified[node][2], pin, verified[node][1], deadline)


if __name__ == "__main__":
    try:
        main()
    except Failure as error:
        print(json.dumps({"status": "UNKNOWN" if cri_attempted else "FAIL", "code": str(error), "stage": error.stage,
            **({"cacheWrites": True} if cri_attempted else {})}))
        raise SystemExit(1) from None
    except (OSError, ValueError, KeyError, TypeError, AttributeError):
        print(json.dumps({"status": "UNKNOWN" if cri_attempted else "FAIL", "code": "INPUT_OR_SHAPE_INVALID", "stage": active_stage,
            **({"cacheWrites": True} if cri_attempted else {})}))
        raise SystemExit(1) from None
