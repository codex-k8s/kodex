#!/usr/bin/env python3
"""Read-only ограниченная диагностика immutable admission evidence; не новый admission verdict."""
import argparse
import hashlib
import json
import os
import re
import stat
import subprocess
import sys
import time

ROOT = "/var/lib/registry/docker/registry/v2"
REPOSITORY = "evidence/role-image-admission"
DEPLOYMENT = "kodex-image-registry-evidence"
NAMESPACE = "kodex-system"
REGISTRY_IMAGE = "registry:2.8.3@sha256:a3d8aaa63ed8681a604f1dea0aa03f100d5895b6a58ace528858a7b332415373"
PART = 16777216
TOTAL = 67108864
SHA = re.compile(r"^[a-f0-9]{64}$")
DIGEST = re.compile(r"^sha256:[a-f0-9]{64}$")
ARTIFACT = re.compile(r"^imgart_[A-Za-z0-9_-]{8,88}$")
UID = re.compile(r"^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$")
NAME = re.compile(r"^[a-z0-9][a-z0-9.-]{0,252}$")
PACKAGE = re.compile(r"^[A-Za-z0-9@][A-Za-z0-9.+_:@/~-]{0,159}$")
VERSION = re.compile(r"^[A-Za-z0-9][A-Za-z0-9.+:~_-]{0,159}$")
FIX = re.compile(r"^[A-Za-z0-9<>=~^*][A-Za-z0-9.+:~_|<>=,^* -]{0,159}$")
ADVISORY = re.compile(r"^(?:CVE-[0-9]{4}-[0-9]{4,12}|GO-[0-9]{4}-[0-9]{4,12}|GHSA-[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{4})$")
TOOL_PATHS = {f"/{directory}/{tool}": tool
              for directory in ("usr/bin", "usr/local/bin", "out/kodex-protected")
              for tool in ("gh", "helm", "kubectl", "buf", "gofumpt", "goimports", "golangci-lint",
                           "goose", "grpcurl", "mockgen", "oapi-codegen", "protoc-gen-go",
                           "protoc-gen-go-grpc", "sqlc", "staticcheck", "yq", "kodex-agent-runner")}
RECEIPT_KEYS = {
    "version", "artifactId", "imageDigest", "specSHA256", "immutableBuildSHA256",
    "provenanceSHA256", "sbomSHA256", "vulnerabilityEvidenceSHA256", "policyRevision",
    "policySHA256", "verdict", "signatureIdentity", "signatureSHA256", "toolInventorySHA256",
}
ENTRIES = [
    ("image-digest.subject", "application/vnd.kodex.image-digest.v1+text"),
    ("image-digest.sigstore.json", "application/vnd.dev.sigstore.bundle.v0.3+json"),
    ("provenance.json", "application/vnd.kodex.provenance-binding.v2+json"),
    ("provenance.sigstore.json", "application/vnd.dev.sigstore.bundle.v0.3+json"),
    ("native-provenance.json", "application/vnd.kodex.native-provenance.v1+json"),
    ("native-provenance.sigstore.json", "application/vnd.dev.sigstore.bundle.v0.3+json"),
    ("tool-inventory.json", "application/vnd.kodex.image-tool-inventory-binding.v1+json"),
    ("tool-inventory.sigstore.json", "application/vnd.dev.sigstore.bundle.v0.3+json"),
    *[(f"sbom.json.part-{i}", "application/vnd.kodex.sbom-byte-part.v1+octet-stream") for i in range(4)],
    ("sbom.sigstore.json", "application/vnd.dev.sigstore.bundle.v0.3+json"),
    *[(f"vulnerability.json.part-{i}", "application/vnd.kodex.vulnerability-byte-part.v1+octet-stream") for i in range(4)],
    ("vulnerability.sigstore.json", "application/vnd.dev.sigstore.bundle.v0.3+json"),
    ("signature.binding.json", "application/vnd.kodex.signature-binding.v1+json"),
    ("admission.receipt.json", "application/vnd.kodex.admission-receipt.v2+json"),
    ("cosign.pub", "application/vnd.dev.cosign.public-key.v1+pem"),
]


class Rejected(Exception):
    pass


class ClosedParser(argparse.ArgumentParser):
    def error(self, message):
        self.exit(2, "Image vulnerability diagnostic failed: INVALID_ARGUMENTS\n")


def require(condition, code):
    if not condition:
        raise Rejected(code)


def exact(value, keys):
    return isinstance(value, dict) and set(value) == set(keys)


def pairs(items):
    result = {}
    for key, value in items:
        require(key not in result, "DUPLICATE_JSON_FIELD")
        result[key] = value
    return result


def decode(data):
    try:
        return json.loads(data, object_pairs_hook=pairs,
                          parse_constant=lambda _: (_ for _ in ()).throw(Rejected("INVALID_JSON")))
    except (ValueError, UnicodeError, RecursionError):
        raise Rejected("INVALID_JSON") from None


def hash_bytes(data):
    return hashlib.sha256(data).hexdigest()


COMMON = (
    '"name":{{printf "%q" .metadata.name}},"namespace":{{printf "%q" .metadata.namespace}},'
    '"uid":{{printf "%q" .metadata.uid}},"deleting":{{if .metadata.deletionTimestamp}}true{{else}}false{{end}},'
    '"labelName":{{printf "%q" (index .metadata.labels "app.kubernetes.io/name")}},'
    '"component":{{printf "%q" (index .metadata.labels "app.kubernetes.io/component")}},'
    '"scope":{{printf "%q" (index .metadata.labels "kodex.dev/registry-scope")}},'
    '"owners":[{{range $i,$o := .metadata.ownerReferences}}{{if $i}},{{end}}'
    '{"kind":{{printf "%q" $o.kind}},"name":{{printf "%q" $o.name}},"uid":{{printf "%q" $o.uid}},'
    '"controller":{{if $o.controller}}true{{else}}false{{end}}}{{end}}]'
)
OBJECT_TEMPLATE = "{" + COMMON + "}"
POD_TEMPLATE = (
    "{" + COMMON + ',"phase":{{printf "%q" .status.phase}},'
    '"ready":[{{range .status.conditions}}{{if eq .type "Ready"}}{{printf "%q" .status}}{{end}}{{end}}],'
    '"serviceAccount":{{printf "%q" .spec.serviceAccountName}},'
    '"registry":[{{range .spec.containers}}{{if eq .name "registry"}}'
    '{"name":{{printf "%q" .name}},"image":{{printf "%q" .image}},'
    '"dataMount":[{{range .volumeMounts}}{{if eq .name "data"}}{{printf "%q" .mountPath}}{{end}}{{end}}]}'
    '{{end}}{{end}}],"registryReady":[{{range .status.containerStatuses}}{{if eq .name "registry"}}'
    '{{if .ready}}true{{else}}false{{end}}{{end}}{{end}}],'
    '"dataPVC":[{{range .spec.volumes}}{{if eq .name "data"}}'
    '{{printf "%q" .persistentVolumeClaim.claimName}}{{end}}{{end}}]}'
)
LIST_TEMPLATE = '[{{range $i,$p := .items}}{{if $i}},{{end}}{{printf "%q" $p.metadata.name}}{{end}}]'


def resource(value):
    require(isinstance(value, dict) and value.get("namespace") == NAMESPACE and
            NAME.fullmatch(value.get("name", "")) and UID.fullmatch(value.get("uid", "")) and
            value.get("deleting") is False and value.get("labelName") == "kodex-image-registry" and
            value.get("component") == "image-registry" and value.get("scope") == "evidence",
            "REGISTRY_RESOURCE_IDENTITY_MISMATCH")


def owner(value, kind):
    owners = value.get("owners")
    require(isinstance(owners, list) and len(owners) == 1 and
            exact(owners[0], ["kind", "name", "uid", "controller"]) and
            owners[0]["kind"] == kind and owners[0]["controller"] is True and
            NAME.fullmatch(owners[0]["name"]) and UID.fullmatch(owners[0]["uid"]),
            "REGISTRY_OWNER_CHAIN_MISMATCH")
    return owners[0]


def validate_chain(pod, replica_set, deployment, expected_uid):
    for value in (pod, replica_set, deployment):
        resource(value)
    require(deployment["name"] == DEPLOYMENT and deployment["uid"] == expected_uid,
            "REGISTRY_DEPLOYMENT_UID_MISMATCH")
    po, ro = owner(pod, "ReplicaSet"), owner(replica_set, "Deployment")
    require((po["name"], po["uid"]) == (replica_set["name"], replica_set["uid"]) and
            (ro["name"], ro["uid"]) == (deployment["name"], deployment["uid"]),
            "REGISTRY_OWNER_CHAIN_MISMATCH")
    require(pod.get("phase") == "Running" and pod.get("ready") == ["True"] and
            pod.get("registryReady") == [True] and pod.get("serviceAccount") == DEPLOYMENT and
            pod.get("registry") == [{"name": "registry", "image": REGISTRY_IMAGE,
                                    "dataMount": ["/var/lib/registry"]}] and
            pod.get("dataPVC") == [DEPLOYMENT], "REGISTRY_POD_NOT_READY")


class Registry:
    def __init__(self, options):
        require(options.context == "k3d-kodex" and UID.fullmatch(options.deployment_uid) and
                options.kubeconfig == "/home/s/.kube/config", "INVALID_REGISTRY_BINDING")
        config = os.lstat(options.kubeconfig)
        require(stat.S_ISREG(config.st_mode) and config.st_uid == os.getuid() and
                stat.S_IMODE(config.st_mode) == 0o600,
                "INVALID_KUBECONFIG_METADATA")
        self.expected_uid = options.deployment_uid
        self.command = ["kubectl", "--context", options.context, "--namespace", NAMESPACE,
                        "--request-timeout=10s"]
        self.environment = {"PATH": os.environ.get("PATH", "/usr/bin:/bin"),
                            "HOME": "/home/s", "KUBECONFIG": options.kubeconfig, "LANG": "C"}
        self.deadline = time.monotonic() + 180
        names = decode(self.call(["get", "pods", "--selector",
                                 "app.kubernetes.io/name=kodex-image-registry,kodex.dev/registry-scope=evidence",
                                 "-o", "go-template=" + LIST_TEMPLATE], 16384))
        require(isinstance(names, list) and len(names) == 1 and isinstance(names[0], str) and
                NAME.fullmatch(names[0]), "REGISTRY_POD_SELECTION_NOT_EXACT")
        self.pod_name = names[0]
        self.pod_uid = None
        self.check()

    def call(self, arguments, bound):
        timeout = min(20, self.deadline - time.monotonic())
        require(timeout > 0, "REGISTRY_READBACK_TIMEOUT")
        try:
            result = subprocess.run(self.command + arguments, env=self.environment,
                                    stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                                    stderr=subprocess.DEVNULL, timeout=timeout, check=False)
        except (OSError, subprocess.TimeoutExpired):
            raise Rejected("REGISTRY_READBACK_FAILED") from None
        require(result.returncode == 0 and len(result.stdout) <= bound, "REGISTRY_READBACK_FAILED")
        return result.stdout

    def get(self, kind, name, template):
        require(NAME.fullmatch(name), "INVALID_REGISTRY_RESOURCE_NAME")
        return decode(self.call(["get", kind, name, "-o", "go-template=" + template], 32768))

    def check(self):
        pod = self.get("pod", self.pod_name, POD_TEMPLATE)
        resource(pod)
        require(self.pod_uid is None or self.pod_uid == pod["uid"], "REGISTRY_POD_UID_CHANGED")
        po = owner(pod, "ReplicaSet")
        rs = self.get("replicaset", po["name"], OBJECT_TEMPLATE)
        dep = self.get("deployment", DEPLOYMENT, OBJECT_TEMPLATE)
        validate_chain(pod, rs, dep, self.expected_uid)
        self.pod_uid = pod["uid"]

    def read(self, path, bound):
        require(path.startswith(ROOT + "/") and ".." not in path and
                re.fullmatch(r"[A-Za-z0-9_./:-]+", path) and 0 <= bound <= PART,
                "INVALID_REGISTRY_PATH")
        self.check()
        # Не следуем symlink и не читаем identity/env; stdout остаётся только в памяти.
        script = ('set -eu; p=$1; b=$2; [ -f "$p" ] && [ ! -L "$p" ]; '
                  '[ "$(readlink -f "$p")" = "$p" ]; n=$(wc -c <"$p"); '
                  '[ "$n" -le "$b" ]; head -c "$((b + 1))" "$p"')
        data = self.call(["exec", self.pod_name, "--container", "registry", "--",
                          "/bin/sh", "-ec", script, "evidence-readback", path, str(bound)], bound)
        self.check()
        return data

    def blob(self, digest, size):
        require(DIGEST.fullmatch(digest) and type(size) is int and 0 <= size <= PART,
                "INVALID_BLOB_DESCRIPTOR")
        data = self.read(f"{ROOT}/blobs/sha256/{digest[7:9]}/{digest[7:]}/data", size)
        require(len(data) == size and "sha256:" + hash_bytes(data) == digest,
                "BLOB_DESCRIPTOR_MISMATCH")
        return data

    def manifest(self, artifact):
        path = f"{ROOT}/repositories/{REPOSITORY}/_manifests/tags/artifact-{artifact}/current/link"
        link = self.read(path, 72)
        require(re.fullmatch(rb"sha256:[a-f0-9]{64}", link), "INVALID_ARTIFACT_LINK")
        digest = link.decode("ascii")
        revision = self.read(f"{ROOT}/repositories/{REPOSITORY}/_manifests/revisions/sha256/{digest[7:]}/link", 72)
        require(revision == link, "IMMUTABLE_MANIFEST_LINK_MISMATCH")
        data = self.read(f"{ROOT}/blobs/sha256/{digest[7:9]}/{digest[7:]}/data", 65536)
        require("sha256:" + hash_bytes(data) == digest, "MANIFEST_DIGEST_MISMATCH")
        return digest, data


def read_evidence(registry, artifact, image, expected_sha, expected_manifest=None):
    require(ARTIFACT.fullmatch(artifact) and DIGEST.fullmatch(image) and SHA.fullmatch(expected_sha),
            "INVALID_EVIDENCE_BINDING")
    manifest_digest, data = registry.manifest(artifact)
    require(expected_manifest is None or manifest_digest == expected_manifest,
            "EXPECTED_MANIFEST_DIGEST_MISMATCH")
    manifest = decode(data)
    require(exact(manifest, ["annotations", "artifactType", "config", "layers", "mediaType", "schemaVersion"]) and
            manifest["schemaVersion"] == 2 and manifest["mediaType"] == "application/vnd.oci.image.manifest.v1+json" and
            manifest["artifactType"] == "application/vnd.kodex.image-admission-evidence.v4", "INVALID_EVIDENCE_MANIFEST")
    annotations = manifest["annotations"]
    require(exact(annotations, ["kodex.dev/artifact-id", "kodex.dev/evidence-schema", "kodex.dev/image-digest",
                                "kodex.dev/policy-revision", "kodex.dev/policy-sha256"]) and
            annotations["kodex.dev/artifact-id"] == artifact and annotations["kodex.dev/image-digest"] == image and
            annotations["kodex.dev/evidence-schema"] == "kodex.dev/image-admission-evidence/v4" and
            re.fullmatch(r"[1-9][0-9]{0,8}", annotations["kodex.dev/policy-revision"]) and
            SHA.fullmatch(annotations["kodex.dev/policy-sha256"]), "EVIDENCE_MANIFEST_BINDING_MISMATCH")
    config = manifest["config"]
    require(config == {"mediaType": "application/vnd.kodex.image-admission-evidence.config.v4+json",
                       "digest": "sha256:" + hash_bytes(b"{}"), "size": 2}, "INVALID_EVIDENCE_CONFIG")
    require(registry.blob(config["digest"], 2) == b"{}", "INVALID_EVIDENCE_CONFIG")
    layers = manifest["layers"]
    require(isinstance(layers, list) and len(layers) == len(ENTRIES), "INVALID_EVIDENCE_LAYERS")
    saved, total = {}, 0
    for descriptor, (title, media_type) in zip(layers, ENTRIES):
        require(exact(descriptor, ["annotations", "digest", "mediaType", "size"]) and
                descriptor["annotations"] == {"org.opencontainers.image.title": title} and
                descriptor["mediaType"] == media_type and type(descriptor["size"]) is int and
                0 <= descriptor["size"] <= PART and DIGEST.fullmatch(descriptor["digest"]),
                "INVALID_EVIDENCE_DESCRIPTOR")
        total += descriptor["size"]
        require(total <= TOTAL, "EVIDENCE_TOTAL_EXCEEDS_BOUND")
        blob = registry.blob(descriptor["digest"], descriptor["size"])
        if title.startswith("vulnerability.json.part-") or title == "admission.receipt.json":
            saved[title] = blob
    for prefix in ("sbom", "vulnerability"):
        sizes = [layers[[title for title, _ in ENTRIES].index(f"{prefix}.json.part-{i}")]["size"] for i in range(4)]
        require(sizes[0] > 0 and all(sizes[i] == 0 or sizes[i - 1] == PART for i in range(1, 4)),
                "NONCANONICAL_EVIDENCE_CHUNKS")
    logical = b"".join(saved[f"vulnerability.json.part-{i}"] for i in range(4))
    require(hash_bytes(logical) == expected_sha, "VULNERABILITY_HASH_MISMATCH")
    receipt = decode(saved["admission.receipt.json"])
    require(exact(receipt, RECEIPT_KEYS) and receipt["version"] == "v2" and
            receipt["artifactId"] == artifact and receipt["imageDigest"] == image and
            receipt["vulnerabilityEvidenceSHA256"] == expected_sha and receipt["verdict"] == "REJECTED" and
            receipt["policyRevision"] == annotations["kodex.dev/policy-revision"] and
            receipt["policySHA256"] == annotations["kodex.dev/policy-sha256"] and
            receipt["signatureIdentity"] == "not-applicable-rejected" and
            all(isinstance(receipt[k], str) and SHA.fullmatch(receipt[k])
                for k in RECEIPT_KEYS if k.endswith("SHA256")), "ADMISSION_RECEIPT_BINDING_MISMATCH")
    return manifest_digest, receipt, decode(logical)


def summary(report, receipt):
    require(isinstance(report, dict) and isinstance(report.get("matches"), list) and
            len(report["matches"]) <= 1000000, "INVALID_VULNERABILITY_REPORT")
    high = []
    for match in report["matches"]:
        require(isinstance(match, dict) and isinstance(match.get("vulnerability"), dict),
                "INVALID_VULNERABILITY_MATCH")
        severity = match["vulnerability"].get("severity")
        require(isinstance(severity, str), "INVALID_VULNERABILITY_MATCH")
        if severity.lower() in ("high", "critical"):
            high.append(match)
    blocked = [m for m in high if isinstance(m["vulnerability"].get("fix"), dict) and
               m["vulnerability"]["fix"].get("state") == "fixed" and
               isinstance(m["vulnerability"]["fix"].get("versions"), list) and m["vulnerability"]["fix"]["versions"]]
    policy = report.get("kodexPolicy")
    require(isinstance(policy, dict) and policy.get("schema") == "kodex.dev/fix-available-high-or-critical/v1" and
            all(type(policy.get(key)) is int and 0 <= policy[key] <= 1000000 for key in
                ("highOrCriticalMatchCount", "blockingMatchCount", "unresolvedNoFixMatchCount")) and
            str(policy.get("policyRevision")) == receipt["policyRevision"] and
            policy.get("policySHA256") == receipt["policySHA256"] and
            policy.get("highOrCriticalMatchCount") == len(high) and
            policy.get("blockingMatchCount") == len(blocked) and
            policy.get("unresolvedNoFixMatchCount") == len(high) - len(blocked) and len(blocked) > 0,
            "VULNERABILITY_POLICY_COUNTS_MISMATCH")
    rows, packages, suppressed, reasons, tools = {}, {}, 0, {}, {}
    for match in blocked:
        vulnerability, artifact = match["vulnerability"], match.get("artifact", {})
        advisory, name, version = vulnerability.get("id"), artifact.get("name"), artifact.get("version")
        fixes = vulnerability["fix"]["versions"]
        checks = {
            "INVALID_ADVISORY": isinstance(advisory, str) and ADVISORY.fullmatch(advisory),
            "INVALID_PACKAGE": isinstance(name, str) and PACKAGE.fullmatch(name) and "://" not in name,
            "INVALID_VERSION": isinstance(version, str) and VERSION.fullmatch(version),
            "INVALID_FIX_EXPRESSIONS": 0 < len(fixes) <= 32 and all(isinstance(f, str) and FIX.fullmatch(f) for f in fixes),
        }
        if not all(checks.values()):
            suppressed += 1
            for code, valid in checks.items():
                if not valid:
                    reasons[code] = reasons.get(code, 0) + 1
            continue
        fixes = tuple(sorted(set(fixes)))
        rows[(advisory, name, version, fixes)] = {"advisory": advisory, "package": name,
                                                "version": version, "fixExpressions": list(fixes)}
        key = (name, version)
        packages[key] = packages.get(key, 0) + 1
        locations = artifact.get("locations", [])
        if locations is None:
            locations = []
        require(isinstance(locations, list) and len(locations) <= 4096, "INVALID_PACKAGE_LOCATIONS")
        for location in locations:
            if isinstance(location, dict):
                tool = TOOL_PATHS.get(location.get("path"))
                if tool:
                    tools.setdefault(key, set()).add(tool)
    ordered_rows = [rows[key] for key in sorted(rows)]
    ordered_packages = [{"package": name, "version": version, "blockingMatchCount": count,
                         "knownTools": sorted(tools.get((name, version), set()))}
                        for (name, version), count in sorted(packages.items())]
    require(len(ordered_rows) <= 512 and len(ordered_packages) <= 256, "SAFE_SUMMARY_EXCEEDS_BOUND")
    return {"highOrCriticalMatchCount": len(high), "blockingMatchCount": len(blocked),
            "unresolvedNoFixMatchCount": len(high) - len(blocked), "suppressedUnsafeMatchCount": suppressed,
            "suppressedReasonCounts": reasons,
            "packageCount": len(packages), "packages": ordered_packages, "advisories": ordered_rows,
            "fixSemantics": "REPORTED_RANGE_NOT_RUNTIME_PIN"}


def main(argv=None):
    parser = ClosedParser(description="Bounded read-only admission vulnerability evidence diagnostic")
    parser.add_argument("--context", required=True)
    parser.add_argument("--kubeconfig", required=True)
    parser.add_argument("--deployment-uid", required=True)
    parser.add_argument("--artifact-ref", required=True)
    parser.add_argument("--image-digest", required=True)
    parser.add_argument("--vulnerability-sha256", required=True)
    parser.add_argument("--evidence-manifest-digest")
    options = parser.parse_args(argv)
    try:
        require(ARTIFACT.fullmatch(options.artifact_ref) and DIGEST.fullmatch(options.image_digest) and
                SHA.fullmatch(options.vulnerability_sha256) and
                (options.evidence_manifest_digest is None or DIGEST.fullmatch(options.evidence_manifest_digest)),
                "INVALID_EVIDENCE_BINDING")
        registry = Registry(options)
        manifest, receipt, report = read_evidence(registry, options.artifact_ref, options.image_digest,
                                                 options.vulnerability_sha256, options.evidence_manifest_digest)
        output = {"schema": "kodex.dev/image-vulnerability-readback/v1", "artifactRef": options.artifact_ref,
                  "imageDigest": options.image_digest, "vulnerabilityEvidenceSha256": options.vulnerability_sha256,
                  "evidenceManifestDigest": manifest, "proof": "OWNER_EXPECTED_CONTENT_HASH_BOUND_DIAGNOSTIC",
                  **summary(report, receipt)}
        encoded = json.dumps(output, separators=(",", ":"), sort_keys=True)
        require(len(encoded) <= 262144, "SAFE_SUMMARY_EXCEEDS_BOUND")
        print(encoded)
        return 0
    except Rejected as error:
        print("Image vulnerability diagnostic failed: " + str(error), file=sys.stderr)
        return 1
    except Exception:
        print("Image vulnerability diagnostic failed: INVALID_EVIDENCE", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
