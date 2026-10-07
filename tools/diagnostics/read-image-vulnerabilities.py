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
import unicodedata
from datetime import datetime

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
RECEIPT_KEYS = {
    "version", "artifactId", "imageDigest", "specSHA256", "immutableBuildSHA256",
    "provenanceSHA256", "sbomSHA256", "vulnerabilityEvidenceSHA256", "policyRevision",
    "policySHA256", "verdict", "signatureIdentity", "signatureSHA256", "toolInventorySHA256",
    "admissionAttemptRef", "admissionAttempt", "fence",
    "vulnerabilityReportProjectionSHA256", "riskAcceptanceSHA256",
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
    ("vulnerability-report.json", "application/vnd.kodex.image-vulnerability-report.v1+json"),
    ("vulnerability-report.sigstore.json", "application/vnd.dev.sigstore.bundle.v0.3+json"),
    ("risk-acceptance.json", "application/vnd.kodex.image-risk-acceptance.v1+json"),
    ("risk-acceptance.sigstore.json", "application/vnd.dev.sigstore.bundle.v0.3+json"),
    ("signature.binding.json", "application/vnd.kodex.signature-binding.v2+json"),
    ("admission.receipt.json", "application/vnd.kodex.admission-receipt.v3+json"),
    ("admission.receipt.sigstore.json", "application/vnd.dev.sigstore.bundle.v0.3+json"),
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
        value = json.loads(data, object_pairs_hook=pairs,
                           parse_constant=lambda _: (_ for _ in ()).throw(Rejected("INVALID_JSON")))
        def check(item, depth):
            require(depth <= 32, "JSON_DEPTH_EXCEEDS_BOUND")
            if isinstance(item, dict):
                for key, child in item.items():
                    key.encode("utf-8")
                    check(child, depth + 1)
            elif isinstance(item, list):
                for child in item:
                    check(child, depth + 1)
            elif isinstance(item, str):
                item.encode("utf-8")
        check(value, 0)
        return value
    except (ValueError, UnicodeError, RecursionError):
        raise Rejected("INVALID_JSON") from None


def hash_bytes(data):
    return hashlib.sha256(data).hexdigest()


SEVERITIES = ("CRITICAL", "HIGH", "MEDIUM", "LOW", "NEGLIGIBLE", "UNKNOWN")
REPORT_KEYS = ("schema", "artifactRef", "imageDigest", "reportSHA256", "sbomSHA256", "scopeKind",
               "organizationRef", "projectRef", "recipeRef", "recipeVersion", "recipeGeneration",
               "buildRef", "buildVersion", "buildAttempt", "policyRevision", "policySHA256",
               "matchCount", "uniqueAdvisoryCount", "blockingMatchCount", "unresolvedNoFixMatchCount",
               "suppressedMatchCount", "severityCounts", "findings")
FINDING_KEYS = ("ref", "packageName", "installedVersion", "ecosystem", "advisoryId", "advisoryKind",
                "advisoryUrl", "severity", "fixState", "fixedVersions", "blocking", "ignored", "occurrences")
RISK_KEYS = ("schema", "decisionRef", "decisionVersion", "action", "scopeKind", "organizationRef",
             "projectRef", "artifactRef", "imageDigest", "reportSHA256", "projectionSHA256",
             "sourceAdmissionRevision", "sourceAdmissionReceiptSHA256", "sourceEvidenceManifestDigest",
             "recipeRef", "recipeVersion", "recipeGeneration", "buildRef", "buildVersion", "buildAttempt",
             "policyRevision", "policySHA256", "reason", "decidedByActorRef", "decidedAt")
IDENTIFIER = re.compile(r"^[A-Za-z0-9@][A-Za-z0-9.+_:@/~-]{0,319}$")
SAFE_VERSION = re.compile(r"^[A-Za-z0-9<>=~^*][A-Za-z0-9.+:~_|<>=,^* -]{0,159}$")
SAFE_ADVISORY = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._+-]{0,159}$")


def pin(value):
    return type(value) is int and 0 < value <= 9007199254740991


def reference(value, prefix=None):
    return isinstance(value, str) and re.fullmatch(r"[a-z][a-z0-9]{1,11}_[A-Za-z0-9_-]{8,84}", value) and (
        prefix is None or value.startswith(prefix + "_"))


def safe_text(value, bound):
    return isinstance(value, str) and 0 < len(value.encode("utf-8")) <= bound and not any(
        unicodedata.category(char) == "Cc" for char in value)


def canonical(value):
    # Совпадает с encoding/json: фиксированный порядок struct fields и HTML escaping.
    text = json.dumps(value, separators=(",", ":"), ensure_ascii=False, allow_nan=False)
    for old, new in (("&", r"\u0026"), ("<", r"\u003c"), (">", r"\u003e"),
                     ("\u2028", r"\u2028"), ("\u2029", r"\u2029")):
        text = text.replace(old, new)
    return text.encode("utf-8")


def ordered(value, keys):
    require(exact(value, keys), "INVALID_TYPED_FIELDS")
    return {key: value[key] for key in keys}


def advisory_link(identifier):
    for pattern, kind, base in ((r"CVE-[0-9]{4}-[0-9]{4,12}", "CVE", "https://nvd.nist.gov/vuln/detail/"),
                                (r"GHSA-[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{4}", "GHSA", "https://github.com/advisories/"),
                                (r"GO-[0-9]{4}-[0-9]{4,12}", "GO", "https://pkg.go.dev/vuln/")):
        if re.fullmatch(pattern, identifier):
            return kind, base + identifier
    return "OTHER", ""


def finding_ref(value):
    copy = ordered(value, FINDING_KEYS)
    copy.update(ref="", occurrences=0)
    return hash_bytes(canonical(copy))


def report_counts(report):
    counts = {key: 0 for key in SEVERITIES}
    total = blocked = no_fix = ignored = 0
    advisories, previous = set(), ""
    for finding in report["findings"]:
        f = ordered(finding, FINDING_KEYS)
        fixes = f["fixedVersions"]
        kind, link = advisory_link(f["advisoryId"])
        require(isinstance(fixes, list) and len(fixes) <= 64 and
                all(isinstance(version, str) and SAFE_VERSION.fullmatch(version) for version in fixes) and
                fixes == sorted(set(fixes)) and isinstance(f["packageName"], str) and IDENTIFIER.fullmatch(f["packageName"]) and
                safe_text(f["installedVersion"], 160) and SAFE_VERSION.fullmatch(f["installedVersion"]) and
                isinstance(f["ecosystem"], str) and IDENTIFIER.fullmatch(f["ecosystem"]) and
                isinstance(f["advisoryId"], str) and SAFE_ADVISORY.fullmatch(f["advisoryId"]) and
                f["advisoryKind"] == kind and f["advisoryUrl"] == link and f["severity"] in SEVERITIES and
                f["fixState"] in ("FIXED", "NOT_FIXED", "WONT_FIX", "UNKNOWN") and
                (f["fixState"] != "FIXED" or len(fixes) > 0) and
                type(f["ignored"]) is bool and type(f["blocking"]) is bool and
                type(f["occurrences"]) is int and 0 < f["occurrences"] <= 4294967295,
                "INVALID_TYPED_FINDING")
        high = f["severity"] in ("HIGH", "CRITICAL")
        fixed = f["fixState"] == "FIXED" and bool(fixes)
        require(f["blocking"] == (not f["ignored"] and high and fixed) and
                f["ref"] == finding_ref(f) and f["ref"] > previous, "INVALID_FINDING_IDENTITY")
        previous = f["ref"]
        count = f["occurrences"]
        total += count; counts[f["severity"]] += count; advisories.add(f["advisoryId"])
        blocked += count if f["blocking"] else 0
        ignored += count if f["ignored"] else 0
        no_fix += count if high and not fixed and not f["ignored"] else 0
    return {"matchCount": total, "uniqueAdvisoryCount": len(advisories), "blockingMatchCount": blocked,
            "unresolvedNoFixMatchCount": no_fix, "suppressedMatchCount": ignored,
            "severityCounts": [{"severity": key, "matchCount": counts[key]} for key in SEVERITIES]}


def validate_report(data):
    require(0 < len(data) <= 4 << 20, "REPORT_PROJECTION_EXCEEDS_BOUND")
    value = ordered(decode(data), REPORT_KEYS)
    require(value["schema"] == "kodex.dev/image-vulnerability-report/v1" and
            reference(value["artifactRef"], "imgart") and DIGEST.fullmatch(value["imageDigest"]) and
            SHA.fullmatch(value["reportSHA256"]) and SHA.fullmatch(value["sbomSHA256"]) and
            reference(value["organizationRef"], "org") and reference(value["recipeRef"], "imgrec") and
            reference(value["buildRef"], "imgbld") and
            (value["scopeKind"] == "ORGANIZATION" and value["projectRef"] == "" or
             value["scopeKind"] == "PROJECT" and reference(value["projectRef"], "prj")) and
            all(pin(value[key]) for key in ("recipeVersion", "recipeGeneration", "buildVersion", "policyRevision")) and
            type(value["buildAttempt"]) is int and 0 < value["buildAttempt"] <= 4294967295 and
            SHA.fullmatch(value["policySHA256"]) and isinstance(value["findings"], list) and
            len(value["findings"]) <= 10000, "INVALID_REPORT_PROJECTION")
    value["findings"] = [ordered(row, FINDING_KEYS) for row in value["findings"]]
    require(isinstance(value["severityCounts"], list), "INVALID_REPORT_COUNTS")
    value["severityCounts"] = [ordered(row, ("severity", "matchCount")) for row in value["severityCounts"]]
    require(all(type(row["matchCount"]) is int and 0 <= row["matchCount"] <= 4294967295
                for row in value["severityCounts"]), "INVALID_REPORT_COUNTS")
    counts = report_counts(value)
    require(all(type(value[key]) is int and 0 <= value[key] <= 4294967295 and value[key] == counts[key]
                for key in counts if key != "severityCounts") and value["severityCounts"] == counts["severityCounts"],
            "REPORT_COUNTS_MISMATCH")
    require(canonical(value) == data, "NONCANONICAL_REPORT_PROJECTION")
    return value


def validate_source(raw, report):
    source = decode(raw)
    require(isinstance(source, dict) and isinstance(source.get("matches"), list) and
            (source.get("ignoredMatches") is None or isinstance(source["ignoredMatches"], list)),
            "INVALID_VULNERABILITY_SOURCE")
    groups = {}
    for ignored, matches in ((False, source["matches"]), (True, source.get("ignoredMatches") or [])):
        for match in matches:
            require(isinstance(match, dict) and isinstance(match.get("artifact"), dict) and
                    isinstance(match.get("vulnerability"), dict), "INVALID_VULNERABILITY_MATCH")
            artifact, vuln = match["artifact"], match["vulnerability"]
            fix = vuln.get("fix")
            require(isinstance(fix, dict) and (fix.get("versions") is None or
                    isinstance(fix["versions"], list) and all(isinstance(version, str) for version in fix["versions"])) and
                    isinstance(vuln.get("id"), str) and isinstance(vuln.get("severity"), str),
                    "INVALID_VULNERABILITY_MATCH")
            kind, link = advisory_link(vuln.get("id", ""))
            state = {"fixed":"FIXED", "not-fixed":"NOT_FIXED", "wont-fix":"WONT_FIX", "unknown":"UNKNOWN"}.get(fix.get("state"), "")
            versions = sorted(set(fix.get("versions") or []))
            severity = vuln.get("severity", "").upper()
            f = dict(zip(FINDING_KEYS, ("", artifact.get("name", ""), artifact.get("version", ""), artifact.get("type", ""),
                        vuln.get("id", ""), kind, link, severity, state, versions,
                        not ignored and severity in ("HIGH","CRITICAL") and state == "FIXED" and bool(versions), ignored, 0)))
            f["ref"] = finding_ref(f); f["occurrences"] = groups.get(f["ref"], {}).get("occurrences", 0) + 1
            groups[f["ref"]] = f
            require(len(groups) <= 10000, "REPORT_FINDINGS_EXCEEDS_BOUND")
    require([groups[key] for key in sorted(groups)] == report["findings"], "REPORT_SOURCE_PROJECTION_MISMATCH")
    p = source.get("kodexPolicy", {})
    require(isinstance(p, dict) and p.get("schema") == "kodex.dev/fix-available-high-or-critical/v1" and
            type(p.get("policyRevision")) is int and p["policyRevision"] == report["policyRevision"] and
            p.get("policySHA256") == report["policySHA256"] and
            all(type(p.get(key)) is int for key in ("highOrCriticalMatchCount", "blockingMatchCount", "unresolvedNoFixMatchCount")) and
            p["highOrCriticalMatchCount"] == report["blockingMatchCount"]+report["unresolvedNoFixMatchCount"] and
            p["blockingMatchCount"] == report["blockingMatchCount"] and
            p["unresolvedNoFixMatchCount"] == report["unresolvedNoFixMatchCount"], "VULNERABILITY_POLICY_COUNTS_MISMATCH")


def validate_risk(raw, report, projection_sha):
    require(0 < len(raw) <= 16384, "RISK_ACCEPTANCE_EXCEEDS_BOUND")
    risk = ordered(decode(raw), RISK_KEYS)
    require(risk["schema"] == "kodex.dev/image-risk-acceptance/v1" and reference(risk["decisionRef"], "imgrisk") and
            type(risk["decisionVersion"]) is int and risk["decisionVersion"] == 1 and risk["action"] == "ACCEPT_RISK" and
            risk["projectionSHA256"] == projection_sha and pin(risk["sourceAdmissionRevision"]) and
            SHA.fullmatch(risk["sourceAdmissionReceiptSHA256"]) and DIGEST.fullmatch(risk["sourceEvidenceManifestDigest"]) and
            safe_text(risk["reason"], 2048) and risk["reason"].strip() == risk["reason"] and
            reference(risk["decidedByActorRef"]) and isinstance(risk["decidedAt"], str) and
            re.fullmatch(r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{0,8}[1-9])?Z", risk["decidedAt"]) and
            report["blockingMatchCount"] > 0, "INVALID_RISK_ACCEPTANCE")
    try:
        datetime.fromisoformat(risk["decidedAt"].replace("Z", "+00:00"))
    except ValueError:
        raise Rejected("INVALID_RISK_ACCEPTANCE") from None
    require(all(risk[key] == report[key] and type(risk[key]) is type(report[key]) for key in
                ("scopeKind", "organizationRef", "projectRef", "artifactRef", "imageDigest", "reportSHA256",
                 "recipeRef", "recipeVersion", "recipeGeneration", "buildRef", "buildVersion", "buildAttempt",
                 "policyRevision", "policySHA256")) and canonical(risk) == raw, "RISK_REPORT_BINDING_MISMATCH")
    return risk


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

    def manifest(self, artifact, digest):
        require(ARTIFACT.fullmatch(artifact) and DIGEST.fullmatch(digest), "INVALID_EVIDENCE_BINDING")
        link = digest.encode("ascii")
        revision = self.read(f"{ROOT}/repositories/{REPOSITORY}/_manifests/revisions/sha256/{digest[7:]}/link", 72)
        require(revision == link, "IMMUTABLE_MANIFEST_LINK_MISMATCH")
        data = self.read(f"{ROOT}/blobs/sha256/{digest[7:9]}/{digest[7:]}/data", 65536)
        require("sha256:" + hash_bytes(data) == digest, "MANIFEST_DIGEST_MISMATCH")
        return digest, data


def read_evidence(registry, artifact, image, expected_sha, expected_manifest,
                  expected_projection, expected_receipt, expected_risk=""):
    require(ARTIFACT.fullmatch(artifact) and DIGEST.fullmatch(image) and SHA.fullmatch(expected_sha) and
            DIGEST.fullmatch(expected_manifest) and SHA.fullmatch(expected_projection) and
            SHA.fullmatch(expected_receipt) and (expected_risk == "" or SHA.fullmatch(expected_risk)),
            "INVALID_EVIDENCE_BINDING")
    manifest_digest, data = registry.manifest(artifact, expected_manifest)
    require(manifest_digest == expected_manifest, "EXPECTED_MANIFEST_DIGEST_MISMATCH")
    manifest = decode(data)
    require(exact(manifest, ["annotations", "artifactType", "config", "layers", "mediaType", "schemaVersion"]) and
            manifest["schemaVersion"] == 2 and manifest["mediaType"] == "application/vnd.oci.image.manifest.v1+json" and
            manifest["artifactType"] == "application/vnd.kodex.image-admission-evidence.v5", "INVALID_EVIDENCE_MANIFEST")
    annotations = manifest["annotations"]
    require(exact(annotations, ["kodex.dev/artifact-id", "kodex.dev/evidence-schema", "kodex.dev/image-digest",
                                "kodex.dev/policy-revision", "kodex.dev/policy-sha256"]) and
            annotations["kodex.dev/artifact-id"] == artifact and annotations["kodex.dev/image-digest"] == image and
            annotations["kodex.dev/evidence-schema"] == "kodex.dev/image-admission-evidence/v5" and
            re.fullmatch(r"[1-9][0-9]{0,8}", annotations["kodex.dev/policy-revision"]) and
            SHA.fullmatch(annotations["kodex.dev/policy-sha256"]), "EVIDENCE_MANIFEST_BINDING_MISMATCH")
    config = manifest["config"]
    require(config == {"mediaType": "application/vnd.kodex.image-admission-evidence.config.v5+json",
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
        saved[title] = registry.blob(descriptor["digest"], descriptor["size"])
    for prefix in ("sbom", "vulnerability"):
        sizes = [len(saved[f"{prefix}.json.part-{i}"]) for i in range(4)]
        require(sizes[0] > 0 and all(sizes[i] == 0 or sizes[i - 1] == PART for i in range(1, 4)),
                "NONCANONICAL_EVIDENCE_CHUNKS")
        saved[prefix + ".json"] = b"".join(saved[f"{prefix}.json.part-{i}"] for i in range(4))
    require(hash_bytes(saved["vulnerability.json"]) == expected_sha, "VULNERABILITY_HASH_MISMATCH")
    require(hash_bytes(saved["admission.receipt.json"]) == expected_receipt, "RECEIPT_HASH_MISMATCH")
    receipt = decode(saved["admission.receipt.json"])
    hashes = RECEIPT_KEYS - {"version", "artifactId", "imageDigest", "policyRevision", "verdict",
                             "signatureIdentity", "admissionAttemptRef", "admissionAttempt", "fence", "riskAcceptanceSHA256"}
    require(exact(receipt, RECEIPT_KEYS) and receipt["version"] == "v3" and
            receipt["artifactId"] == artifact and receipt["imageDigest"] == image and
            receipt["vulnerabilityEvidenceSHA256"] == expected_sha and
            receipt["vulnerabilityReportProjectionSHA256"] == expected_projection and
            receipt["riskAcceptanceSHA256"] == expected_risk and receipt["verdict"] in ("ACCEPTED", "REJECTED") and
            receipt["policyRevision"] == annotations["kodex.dev/policy-revision"] and
            receipt["policySHA256"] == annotations["kodex.dev/policy-sha256"] and
            reference(receipt["admissionAttemptRef"], "imgadm") and pin(receipt["admissionAttempt"]) and pin(receipt["fence"]) and
            all(isinstance(receipt[k], str) and SHA.fullmatch(receipt[k]) for k in hashes),
            "ADMISSION_RECEIPT_BINDING_MISMATCH")
    signature = decode(saved["signature.binding.json"])
    signature_keys = ("imageDigest", "policyRevision", "policySHA256", "signatureIdentity", "verdict",
                      "verification", "version", "admissionAttemptRef", "admissionAttempt", "fence",
                      "vulnerabilityReportProjectionSHA256", "riskAcceptanceSHA256")
    require(exact(signature, signature_keys) and signature["version"] == "v2" and
            signature["verification"] == "cosign-key-v1" and
            all(signature[key] == receipt[key] and type(signature[key]) is type(receipt[key])
                for key in signature_keys if key not in ("version", "verification")),
            "SIGNATURE_BINDING_MISMATCH")
    for title, key in (("signature.binding.json", "signatureSHA256"), ("provenance.json", "provenanceSHA256"),
                       ("sbom.json", "sbomSHA256"), ("tool-inventory.json", "toolInventorySHA256"),
                       ("vulnerability-report.json", "vulnerabilityReportProjectionSHA256")):
        require(hash_bytes(saved[title]) == receipt[key], "LOGICAL_EVIDENCE_HASH_MISMATCH")
    require(isinstance(decode(saved["sbom.json"]), dict), "INVALID_SBOM")
    report = validate_report(saved["vulnerability-report.json"])
    require(report["artifactRef"] == artifact and report["imageDigest"] == image and report["reportSHA256"] == expected_sha and
            report["sbomSHA256"] == receipt["sbomSHA256"] and str(report["policyRevision"]) == receipt["policyRevision"] and
            report["policySHA256"] == receipt["policySHA256"], "REPORT_RECEIPT_BINDING_MISMATCH")
    validate_source(saved["vulnerability.json"], report)
    require(saved["image-digest.subject"] == (image+"\n").encode(), "IMAGE_SUBJECT_MISMATCH")
    if expected_risk:
        require(hash_bytes(saved["risk-acceptance.json"]) == expected_risk, "RISK_HASH_MISMATCH")
        validate_risk(saved["risk-acceptance.json"], report, expected_projection)
    else:
        require(saved["risk-acceptance.json"] == b"" and saved["risk-acceptance.sigstore.json"] == b"",
                "UNEXPECTED_RISK_ACCEPTANCE")
    if receipt["verdict"] == "REJECTED":
        require(receipt["signatureIdentity"] == "not-applicable-rejected" and report["blockingMatchCount"] > 0 and
                expected_risk == "" and all(not saved[title] for title, _ in ENTRIES if title.endswith(".sigstore.json")),
                "REJECTED_EVIDENCE_MISMATCH")
    else:
        require(SHA.fullmatch(receipt["signatureIdentity"]) and
                hash_bytes(saved["cosign.pub"]) == receipt["signatureIdentity"] and
                (report["blockingMatchCount"] == 0 or expected_risk != "") and
                all(saved[title] for title, _ in ENTRIES if title.endswith(".sigstore.json") and
                    (expected_risk != "" or title != "risk-acceptance.sigstore.json")),
                "ACCEPTED_EVIDENCE_MISMATCH")
    # Диагностика проверяет owner-пины и полные content hashes, но не заменяет cosign/admission verifier.
    return manifest_digest, receipt, report


def summary(report, receipt):
    packages, advisories = {}, {}
    for finding in report["findings"]:
        if not finding["blocking"]:
            continue
        key = (finding["packageName"], finding["installedVersion"], finding["ecosystem"])
        packages[key] = packages.get(key, 0) + finding["occurrences"]
        row_key = (finding["advisoryId"], *key, tuple(finding["fixedVersions"]))
        advisories[row_key] = {"advisory": finding["advisoryId"], "package": finding["packageName"],
                               "version": finding["installedVersion"], "ecosystem": finding["ecosystem"],
                               "fixExpressions": finding["fixedVersions"]}
    require(len(packages) <= 256 and len(advisories) <= 512, "SAFE_SUMMARY_EXCEEDS_BOUND")
    return {"matchCount": report["matchCount"], "uniqueAdvisoryCount": report["uniqueAdvisoryCount"],
            "highOrCriticalMatchCount": report["blockingMatchCount"]+report["unresolvedNoFixMatchCount"],
            "blockingMatchCount": report["blockingMatchCount"], "unresolvedNoFixMatchCount": report["unresolvedNoFixMatchCount"],
            "suppressedMatchCount": report["suppressedMatchCount"], "severityCounts": report["severityCounts"],
            "packageCount": len(packages), "packages": [
                {"package": name, "version": version, "ecosystem": ecosystem, "blockingMatchCount": count}
                for (name, version, ecosystem), count in sorted(packages.items())],
            "advisories": [advisories[key] for key in sorted(advisories)],
            "riskAcceptancePresent": receipt["riskAcceptanceSHA256"] != "",
            "fixSemantics": "REPORTED_RANGE_NOT_RUNTIME_PIN"}


def main(argv=None):
    parser = ClosedParser(description="Bounded read-only admission vulnerability evidence diagnostic")
    parser.add_argument("--context", required=True)
    parser.add_argument("--kubeconfig", required=True)
    parser.add_argument("--deployment-uid", required=True)
    parser.add_argument("--artifact-ref", required=True)
    parser.add_argument("--image-digest", required=True)
    parser.add_argument("--vulnerability-sha256", required=True)
    parser.add_argument("--evidence-manifest-digest", required=True)
    parser.add_argument("--projection-sha256", required=True)
    parser.add_argument("--admission-receipt-sha256", required=True)
    parser.add_argument("--risk-acceptance-sha256", default="")
    options = parser.parse_args(argv)
    try:
        require(ARTIFACT.fullmatch(options.artifact_ref) and DIGEST.fullmatch(options.image_digest) and
                SHA.fullmatch(options.vulnerability_sha256) and
                (options.evidence_manifest_digest is None or DIGEST.fullmatch(options.evidence_manifest_digest)),
                "INVALID_EVIDENCE_BINDING")
        registry = Registry(options)
        manifest, receipt, report = read_evidence(registry, options.artifact_ref, options.image_digest,
                                                 options.vulnerability_sha256, options.evidence_manifest_digest,
                                                 options.projection_sha256, options.admission_receipt_sha256,
                                                 options.risk_acceptance_sha256)
        output = {"schema": "kodex.dev/image-vulnerability-readback/v1", "artifactRef": options.artifact_ref,
                  "imageDigest": options.image_digest, "vulnerabilityEvidenceSha256": options.vulnerability_sha256,
                  "evidenceManifestDigest": manifest, "projectionSha256": options.projection_sha256,
                  "admissionReceiptSha256": options.admission_receipt_sha256,
                  "proof": "OWNER_EXPECTED_CONTENT_HASH_BOUND_DIAGNOSTIC_NOT_SIGNATURE_VERIFICATION",
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
