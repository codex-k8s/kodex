"""Bounded Kubernetes/loopback transport static backup operator; secrets только stdin."""

import base64
import hashlib
import json
import os
import selectors
import subprocess
import time
from urllib.parse import quote, urlencode

import scoped_backup_drill as custody
import static_backup_identity_plan as contract

HELLO = b"KODEX_STATIC_S3_READY\n"
MAX_RESPONSE = 256 << 10


def strict_json(raw):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            custody.require(key not in result, "DUPLICATE_JSON_KEY")
            result[key] = value
        return result
    return json.loads(raw, object_pairs_hook=unique)


def identity(value):
    result = {key: value["metadata"][key] for key in ("uid", "resourceVersion")}
    contract.resource_identity(result)
    return result


def safe_sts(value):
    return {"apiVersion": value["apiVersion"], "kind": value["kind"],
            "metadata": {key: value["metadata"][key] for key in ("name", "namespace", "uid", "resourceVersion", "generation")},
            "spec": value["spec"],
            "status": {key: value.get("status", {}).get(key) for key in
                       ("readyReplicas", "currentRevision", "updateRevision", "observedGeneration")}}


def pod_pin(value, image):
    owners = [item for item in value["metadata"].get("ownerReferences", []) if item.get("controller")]
    custody.require(len(owners) == 1 and owners[0]["kind"] == "StatefulSet" and
                    owners[0]["uid"] == contract.STS_UID and value["metadata"]["name"] == "seaweedfs-0" and
                    value["metadata"]["namespace"] == custody.SOURCE_NAMESPACE, "POD_OWNER_CHANGED")
    container, = value["spec"]["containers"]
    custody.require(container["name"] == "seaweedfs" and container["image"] == image and
                    not value["metadata"].get("deletionTimestamp"), "POD_IMAGE_OR_LIFECYCLE_CHANGED")
    result = identity(value)
    result.update({"name": "seaweedfs-0", "ownerUID": contract.STS_UID,
                   "revision": value["metadata"]["labels"]["controller-revision-hash"],
                   "ready": any(item["type"] == "Ready" and item["status"] == "True"
                                for item in value.get("status", {}).get("conditions", [])),
                   "pvcName": next(item["persistentVolumeClaim"]["claimName"] for item in value["spec"]["volumes"] if item["name"] == "data"),
                   "authSecretName": next(item["secret"]["secretName"] for item in value["spec"]["volumes"] if item["name"] == "s3-config")})
    return result


def parse_response(raw):
    custody.require(len(raw) <= MAX_RESPONSE and raw.startswith(b"HTTP/"), "PROBE_RESPONSE_INVALID")
    # curl --include может выдать interim 100 Continue; максимум два header envelopes.
    for _ in range(2):
        header, separator, body = raw.partition(b"\r\n\r\n")
        custody.require(separator, "PROBE_HEADERS_INVALID")
        lines = header.decode("iso-8859-1").split("\r\n")
        status = int(lines[0].split()[1])
        if status == 100:
            raw = body
            continue
        headers = {}
        for line in lines[1:]:
            key, separator, value = line.partition(":")
            custody.require(separator, "PROBE_HEADERS_INVALID")
            key = key.lower()
            custody.require(key not in headers, "PROBE_DUPLICATE_HEADER")
            headers[key] = value.strip()
        return status, headers, body
    raise custody.Rejected("PROBE_INTERIM_RESPONSE_INVALID")


class Kubernetes(custody.Kubernetes):
    """Читает только exact context; изменения отправляет без shell и без retries."""

    def get(self, kind, name):
        return strict_json(self.run(["-n", custody.SOURCE_NAMESPACE, "get", kind, name, "-o", "json"]))

    def optional(self, kind, name):
        raw = self.run(["-n", custody.SOURCE_NAMESPACE, "get", kind, name, "--ignore-not-found", "-o", "json"])
        return strict_json(raw) if raw.strip() else None

    def secret_metadata(self, name):
        query = ('jsonpath={.metadata.uid}{"|"}{.metadata.resourceVersion}{"|"}{.immutable}{"|"}{.type}{"|"}'
                 '{.metadata.labels}{"|"}{.metadata.annotations.kodex\\.dev/payload-sha256}')
        fields = self.run(["-n", custody.SOURCE_NAMESPACE, "get", "secret", name, "-o", query]).decode().split("|")
        custody.require(len(fields) == 6, "SECRET_METADATA_INVALID")
        keys = self.run(["-n", custody.SOURCE_NAMESPACE, "get", "secret", name, "-o",
                         "go-template={{range $key,$value := .data}}{{$key}},{{end}}"])
        return {"uid": fields[0], "resourceVersion": fields[1], "immutable": fields[2] == "true",
                "type": fields[3], "labels": strict_json(fields[4]), "payloadSHA256": fields[5],
                "keys": sorted(keys.decode().rstrip(",").split(","))}

    def tls_preflight(self):
        raw = self.run(["config", "view", "--minify", "-o",
                        'jsonpath={.clusters[0].cluster.server}{"|"}{.clusters[0].cluster.insecure-skip-tls-verify}{"|"}'
                        '{.clusters[0].cluster.certificate-authority}{"|"}{.clusters[0].cluster.certificate-authority-data}'])
        server, skip, ca_file, ca_data = raw.decode().split("|")
        custody.require(server.startswith("https://") and skip in ("", "false") and (ca_file or ca_data),
                        "AUTHENTICATED_KUBERNETES_TLS_REQUIRED")

    def endpoint_ready(self, uid, pod_ip):
        slices = strict_json(self.run(["-n", custody.SOURCE_NAMESPACE, "get", "endpointslices",
                                      "-l", "kubernetes.io/service-name=seaweedfs-s3", "-o", "json"]))
        ready = []
        for item in slices["items"]:
            custody.require(item["metadata"]["namespace"] == custody.SOURCE_NAMESPACE and
                            item["metadata"]["labels"]["kubernetes.io/service-name"] == "seaweedfs-s3", "SERVICE_ENDPOINT_SCOPE_CHANGED")
            for endpoint in item.get("endpoints", []):
                if endpoint.get("conditions", {}).get("ready") is True:
                    ref = endpoint.get("targetRef", {})
                    custody.require(ref.get("kind") == "Pod" and ref.get("namespace") == custody.SOURCE_NAMESPACE and
                                    ref.get("name") == "seaweedfs-0" and ref.get("uid") == uid and
                                    endpoint["addresses"] == [pod_ip] and
                                    any(port.get("port") == 8333 and port.get("protocol", "TCP") == "TCP" for port in item["ports"]),
                                    "SERVICE_ENDPOINT_POD_CHANGED")
                    ready.append(endpoint)
        custody.require(len(ready) == 1, "EXACT_SINGLE_SERVICE_ENDPOINT_REQUIRED")

    def create(self, body):
        return strict_json(self.run(["create", "-f", "-", "-o", "json"], custody.canonical(body)))

    def patch_sts(self, body):
        return strict_json(self.run(["-n", custody.SOURCE_NAMESPACE, "patch", "statefulset", "seaweedfs",
                                     "--type=json", "--patch-file=/dev/stdin", "-o", "json"], custody.canonical(body)))

    def delete(self, kind, name, pins, grace=None):
        custody.require(kind in ("secrets", "pods"), "DELETE_KIND_INVALID")
        options = {"apiVersion": "v1", "kind": "DeleteOptions", "preconditions": pins}
        if grace is not None:
            options["gracePeriodSeconds"] = grace
        self.run(["delete", "--raw", f"/api/v1/namespaces/{custody.SOURCE_NAMESPACE}/{kind}/{name}", "--filename=-"],
                 custody.canonical(options))

    def consumers_stopped(self, value):
        # Own run во всех namespaces плюс любые consumers input Secrets в source namespace.
        own = strict_json(self.run(["get", "pods", "--all-namespaces", "-l", "kodex.dev/e2e-run=" + value["run"], "-o", "json"]))
        local = strict_json(self.run(["-n", custody.SOURCE_NAMESPACE, "get", "pods", "-o", "json"]))
        names = {value["authSecret"]["name"], value["scopedSecret"]["name"],
                 "backup-controller-credentials", "backup-controller-repository"}
        for pod in own["items"] + local["items"]:
            if (pod["metadata"]["name"] == "seaweedfs-0" and pod["metadata"].get("namespace") == custody.SOURCE_NAMESPACE and
                    any(item.get("controller") is True and item.get("kind") == "StatefulSet" and item.get("uid") == contract.STS_UID
                        for item in pod["metadata"].get("ownerReferences", []))):
                continue
            refs = set()
            for volume in pod["spec"].get("volumes", []):
                refs.add(volume.get("secret", {}).get("secretName"))
                refs.update(item.get("secret", {}).get("name") for item in volume.get("projected", {}).get("sources", []))
            for container in pod["spec"].get("containers", []) + pod["spec"].get("initContainers", []) + pod["spec"].get("ephemeralContainers", []):
                refs.update(item.get("secretRef", {}).get("name") for item in container.get("envFrom", []))
                refs.update(item.get("valueFrom", {}).get("secretKeyRef", {}).get("name") for item in container.get("env", []))
            relevant = pod["metadata"].get("labels", {}).get("kodex.dev/e2e-run") == value["run"] or bool(refs & names)
            custody.require(not relevant or pod.get("status", {}).get("phase") in ("Succeeded", "Failed"), "ACTIVE_STATIC_CREDENTIAL_CONSUMER")


class Loopback:
    """Exec HELLO → второй Pod UID pin → секретный curl config в существующий stdin."""

    def __init__(self, kube, pin, value):
        contract.validate_plan(value)
        self.kube, self.pin, self.value = kube, pin, value

    def request(self, method, path, query=(), access=None, secret=None, body=b"", extra=None, filer=False):
        custody.require(method in ("GET", "HEAD", "PUT", "DELETE") and path.startswith("/") and
                        "\n" not in path and "\r" not in path and len(body) < 1024, "PROBE_REQUEST_INVALID")
        if filer:
            allowed = path in ("/etc/iam/identity.json", "/etc/iam/policies.json",
                               "/etc/iam/identities/" + self.value["identityName"] + ".json",
                               "/etc/iam/policies/" + self.value["policyName"] + ".json")
            custody.require(method == "HEAD" and not query and access is None and not body and allowed,
                            "FILER_HEAD_ONLY")
            headers, encoded, port = {}, "", 8888
        else:
            custody.require(access is not None and secret is not None, "PROBE_CREDENTIAL_REQUIRED")
            roots = {"/" + bucket for bucket in contract.BUCKETS}
            qa_paths = {"/kodex-backups/" + self.value["qaPrefixes"]["repository"] + "/_static-auth-probe/" + self.value["owner"],
                        "/kodex-restore-fixture/" + self.value["qaPrefixes"]["target"] + "/_static-auth-probe/" + self.value["owner"]}
            source_paths = {"/kodex-artifacts/organizations/_static-auth-absent-" + self.value["owner"],
                            "/kodex-session-archives/session-archive/v1/_static-auth-absent-" + self.value["owner"]}
            custody.require(path in roots | qa_paths | source_paths, "PROBE_EXACT_PATH_REQUIRED")
            if method in ("PUT", "DELETE"):
                custody.require(path in qa_paths, "PROBE_SOURCE_MUTATION_FORBIDDEN")
                if method == "PUT":
                    custody.require(not query and body == ("kodex-static-probe-v1:" + self.value["owner"]).encode() and
                                    extra == {"if-none-match": "*", "x-amz-meta-kodex-static-owner": self.value["owner"]}, "PROBE_CONDITIONAL_CREATE_ONLY")
                else:
                    custody.require(len(query) == 1 and query[0][0] == "versionId" and 0 < len(query[0][1]) <= 256 and not body and not extra,
                                    "PROBE_EXACT_VERSION_DELETE_ONLY")
            else:
                custody.require(not body and not extra and all(key in ("prefix", "max-keys", "list-type", "versions", "versioning", "policy", "versionId") for key, _ in query) and
                                (path not in source_paths or method == "HEAD" and not query), "PROBE_READ_PROFILE_INVALID")
            headers = {"x-amz-content-sha256": hashlib.sha256(body).hexdigest(), **(extra or {})}
            encoded = urlencode(sorted(query), quote_via=quote, safe="~")
            port = 8333
        url = f"http://127.0.0.1:{port}" + path + ("?" + encoded if encoded else "")
        # json.dumps экранирует конфигурационные строки curl; все тела fixtures — ASCII.
        config = ["url = " + json.dumps(url), "request = " + json.dumps(method),
                  'header = "Expect:"', 'header = "Accept-Encoding: identity"']
        if method == "HEAD":
            config.append("head")
        if not filer:
            custody.require(contract.NONCE.fullmatch(access) and len(secret) == 64 and
                            all(char in "0123456789abcdef" for char in secret), "PROBE_KEY_INVALID")
            config += ['aws-sigv4 = "aws:amz:us-east-1:s3"', "user = " + json.dumps(access + ":" + secret)]
        config += ["header = " + json.dumps(key + ": " + value) for key, value in sorted(headers.items())]
        if body:
            config.append("data = " + json.dumps(body.decode("ascii")))
        payload = ("\n".join(config) + "\n").encode()
        # Не исполняем входной shell и не передаём credentials через argv/environment.
        command = "printf 'KODEX_STATIC_S3_READY\\n'; exec curl -q --config - --silent --show-error --include --http1.1 --max-time 8 --max-filesize 131072 --retry 0 --proto '=http' --noproxy '*' --proxy '' --max-redirs 0"
        args = ["kubectl", "--context", custody.CONTEXT, "--request-timeout=15s", "-n", custody.SOURCE_NAMESPACE,
                "exec", "-i", "seaweedfs-0", "-c", "seaweedfs", "--", "/bin/sh", "-ec", command]
        env = {key: os.environ[key] for key in ("PATH", "HOME", "KUBECONFIG") if key in os.environ}
        process = subprocess.Popen(args, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env)
        try:
            hello = bytearray()
            handshake_deadline = time.monotonic() + 8
            with selectors.DefaultSelector() as selector:
                selector.register(process.stdout, selectors.EVENT_READ)
                while len(hello) < len(HELLO):
                    remaining = handshake_deadline - time.monotonic()
                    custody.require(remaining > 0 and bool(selector.select(remaining)), "PROBE_HANDSHAKE_TIMEOUT")
                    part = os.read(process.stdout.fileno(), len(HELLO) - len(hello))
                    custody.require(bool(part), "PROBE_HANDSHAKE_EOF")
                    hello.extend(part)
                    custody.require(HELLO.startswith(hello), "PROBE_HANDSHAKE_INVALID")
            custody.require(hello == HELLO, "PROBE_HANDSHAKE_INVALID")
            fresh = pod_pin(self.kube.get("pod", "seaweedfs-0"), contract.image_pin())
            custody.require(fresh["uid"] == self.pin["uid"] and fresh["authSecretName"] == self.pin["authSecretName"] and fresh["ready"],
                            "PROBE_POD_CHANGED_BEFORE_CREDENTIAL_STDIN")
            raw, _stderr = process.communicate(payload, timeout=12)
            custody.require(process.returncode == 0, "PROBE_TRANSPORT_FAILED_OR_UNKNOWN")
            return parse_response(raw)
        except (OSError, subprocess.TimeoutExpired):
            raise custody.Rejected("PROBE_TRANSPORT_FAILED_OR_UNKNOWN") from None
        finally:
            if process.poll() is None:
                process.kill()
            process.communicate(timeout=3)


def decode_secret(value):
    custody.require(value.get("immutable") is True and value.get("type") == "Opaque", "IMMUTABLE_SECRET_REQUIRED")
    return {key: base64.b64decode(item, validate=True).decode() for key, item in value["data"].items()}


class MetadataView:
    """Добавляет только read методы к существующему custody Kubernetes port."""

    get = Kubernetes.get
    secret_metadata = Kubernetes.secret_metadata
    tls_preflight = Kubernetes.tls_preflight
    endpoint_ready = Kubernetes.endpoint_ready

    def __init__(self, delegate):
        self.delegate = delegate

    def __getattr__(self, name):
        return getattr(self.delegate, name)
