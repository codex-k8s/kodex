import copy
import hashlib
import importlib.util
import io
import json
import pathlib
import unittest
from types import SimpleNamespace
from contextlib import redirect_stdout, redirect_stderr
from unittest.mock import patch

path = pathlib.Path(__file__).with_name("read-image-vulnerabilities.py")
spec = importlib.util.spec_from_file_location("image_vulnerability_diagnostic", path)
diagnostic = importlib.util.module_from_spec(spec)
spec.loader.exec_module(diagnostic)
ARTIFACT = "imgart_synthetic12345678"
IMAGE = "sha256:" + "a" * 64
POLICY = "b" * 64
DEP_UID = "11111111-1111-1111-1111-111111111111"


def encoded(value):
    return json.dumps(value, separators=(",", ":")).encode()


class Evidence(diagnostic.Registry):
    def __init__(self, report):
        self.blobs = {}
        self.report = report
        logical = encoded(report)
        self.sha = diagnostic.hash_bytes(logical)
        self.receipt = {key: "c" * 64 for key in diagnostic.RECEIPT_KEYS}
        self.receipt.update(version="v2", artifactId=ARTIFACT, imageDigest=IMAGE,
                            vulnerabilityEvidenceSHA256=self.sha, policyRevision="88",
                            policySHA256=POLICY, verdict="REJECTED",
                            signatureIdentity="not-applicable-rejected")
        self.layers = []
        for title, media_type in diagnostic.ENTRIES:
            if title == "admission.receipt.json":
                data = encoded(self.receipt)
            elif title == "vulnerability.json.part-0":
                data = logical
            elif title == "sbom.json.part-0":
                data = b"{}"
            elif ".part-" in title or title.endswith(".sigstore.json"):
                data = b""
            else:
                data = b"{}"
            digest = "sha256:" + diagnostic.hash_bytes(data)
            self.blobs[digest] = data
            self.layers.append({"annotations": {"org.opencontainers.image.title": title},
                                "mediaType": media_type, "digest": digest, "size": len(data)})
        self.blobs["sha256:" + diagnostic.hash_bytes(b"{}")] = b"{}"
        self.document = {
            "schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json",
            "artifactType": "application/vnd.kodex.image-admission-evidence.v4",
            "config": {"mediaType": "application/vnd.kodex.image-admission-evidence.config.v4+json",
                       "digest": "sha256:" + diagnostic.hash_bytes(b"{}"), "size": 2},
            "annotations": {"kodex.dev/artifact-id": ARTIFACT, "kodex.dev/image-digest": IMAGE,
                            "kodex.dev/evidence-schema": "kodex.dev/image-admission-evidence/v4",
                            "kodex.dev/policy-revision": "88", "kodex.dev/policy-sha256": POLICY},
            "layers": self.layers,
        }
        self.reads = []

    def manifest(self, artifact):
        data = encoded(self.document)
        return "sha256:" + diagnostic.hash_bytes(data), data

    def read(self, path, bound):
        digest = "sha256:" + path.split("/")[-2]
        self.reads.append(digest)
        return self.blobs[digest]

    def replace_receipt(self):
        data = encoded(self.receipt)
        descriptor = next(d for d in self.layers if d["annotations"]["org.opencontainers.image.title"] == "admission.receipt.json")
        digest = "sha256:" + diagnostic.hash_bytes(data)
        self.blobs[digest] = data
        descriptor.update(digest=digest, size=len(data))


def report():
    matches = []
    for advisory, name, fixed in (("GHSA-abcd-2345-vwx8", "npm", True),
                                  ("CVE-2026-12345", "golang.org/x/net", True),
                                  ("CVE-2026-54321", "openssl", False)):
        matches.append({"vulnerability": {"id": advisory, "severity": "High",
                          "fix": {"state": "fixed" if fixed else "not-fixed",
                                  "versions": [">= 1.2.3, < 2.0.0"] if fixed else []},
                          "dataSource": "https://private.invalid/do-not-output"},
                        "artifact": {"name": name, "version": "1.0.0",
                                     "locations": [{"path": "/private/do-not-output"}]}})
    return {"matches": matches, "kodexPolicy": {"schema": "kodex.dev/fix-available-high-or-critical/v1",
            "policyRevision": 88, "policySHA256": POLICY, "highOrCriticalMatchCount": 3,
            "blockingMatchCount": 2, "unresolvedNoFixMatchCount": 1}}


def chain():
    base = {"namespace": "kodex-system", "deleting": False, "labelName": "kodex-image-registry",
            "component": "image-registry", "scope": "evidence"}
    dep = dict(base, name=diagnostic.DEPLOYMENT, uid=DEP_UID, owners=[])
    rs = dict(base, name="kodex-image-registry-evidence-12345",
              uid="22222222-2222-2222-2222-222222222222",
              owners=[{"kind": "Deployment", "name": dep["name"], "uid": dep["uid"], "controller": True}])
    pod = dict(base, name=rs["name"] + "-abcde", uid="33333333-3333-3333-3333-333333333333",
               owners=[{"kind": "ReplicaSet", "name": rs["name"], "uid": rs["uid"], "controller": True}],
               phase="Running", ready=["True"], registryReady=[True], serviceAccount=diagnostic.DEPLOYMENT,
               registry=[{"name": "registry", "image": diagnostic.REGISTRY_IMAGE, "dataMount": ["/var/lib/registry"]}],
               dataPVC=[diagnostic.DEPLOYMENT])
    return pod, rs, dep


class DiagnosticTest(unittest.TestCase):
    def test_locations_only_emit_closed_tool_names_not_paths(self):
        value = report()
        value["matches"][0]["artifact"]["locations"] = [
            {"path": "/usr/local/bin/helm"}, {"path": "/private/do-not-output/gh"},
            {"path": "/usr/bin/gh"}, {"path": "/usr/local/bin/helm"}]
        fixture = Evidence(value)
        _, receipt, loaded = self.read(fixture)
        result = diagnostic.summary(loaded, receipt)
        npm = next(row for row in result["packages"] if row["package"] == "npm")
        self.assertEqual(npm["knownTools"], ["gh", "helm"])
        self.assertNotIn("/usr", json.dumps(result))
        self.assertNotIn("private", json.dumps(result))
        loaded["matches"][0]["artifact"]["locations"] = None
        result = diagnostic.summary(loaded, receipt)
        self.assertEqual(result["blockingMatchCount"], 2)
        self.assertEqual(result["suppressedUnsafeMatchCount"], 0)

    def test_closed_go_advisory_and_suppression_reason_counts(self):
        value = report()
        value["matches"][0]["vulnerability"]["id"] = "GO-2026-12345"
        fixture = Evidence(value)
        _, receipt, loaded = self.read(fixture)
        result = diagnostic.summary(loaded, receipt)
        self.assertEqual(result["suppressedUnsafeMatchCount"], 0)
        self.assertEqual(result["suppressedReasonCounts"], {})
        self.assertTrue(any(row["advisory"] == "GO-2026-12345" for row in result["advisories"]))
        loaded["matches"][0]["vulnerability"]["id"] = "https://private.invalid/secret"
        loaded["matches"][0]["artifact"]["version"] = "private/secret"
        result = diagnostic.summary(loaded, receipt)
        self.assertEqual(result["suppressedUnsafeMatchCount"], 1)
        self.assertEqual(result["suppressedReasonCounts"], {"INVALID_ADVISORY": 1, "INVALID_VERSION": 1})
        self.assertNotIn("private", json.dumps(result))

    def test_only_exact_owned_regular_private_kubeconfig_is_accepted(self):
        options = SimpleNamespace(context="k3d-kodex", deployment_uid=DEP_UID,
                                  kubeconfig="/home/s/.kube/config")
        valid = SimpleNamespace(st_mode=diagnostic.stat.S_IFREG | 0o600,
                                st_uid=diagnostic.os.getuid())
        with patch.object(diagnostic.os, "lstat", return_value=valid), \
                patch.object(diagnostic.Registry, "call", return_value=b'["synthetic-pod"]'), \
                patch.object(diagnostic.Registry, "check"):
            registry = diagnostic.Registry(options)
            self.assertEqual(set(registry.environment), {"PATH", "HOME", "KUBECONFIG", "LANG"})
            self.assertEqual(registry.environment["HOME"], "/home/s")
        for mode, uid in ((diagnostic.stat.S_IFLNK | 0o600, valid.st_uid),
                          (diagnostic.stat.S_IFREG | 0o644, valid.st_uid),
                          (diagnostic.stat.S_IFREG | 0o600, valid.st_uid + 1)):
            with patch.object(diagnostic.os, "lstat", return_value=SimpleNamespace(st_mode=mode, st_uid=uid)):
                with self.assertRaisesRegex(diagnostic.Rejected, "INVALID_KUBECONFIG_METADATA"):
                    diagnostic.Registry(options)
        options.kubeconfig = "/private/other-config"
        with self.assertRaisesRegex(diagnostic.Rejected, "INVALID_REGISTRY_BINDING"):
            diagnostic.Registry(options)

    def read(self, fixture):
        return diagnostic.read_evidence(fixture, ARTIFACT, IMAGE, fixture.sha)

    def test_all_descriptors_hash_checked_and_ghsa_ranges_are_safe_data(self):
        fixture = Evidence(report())
        manifest, receipt, result = self.read(fixture)
        summary = diagnostic.summary(result, receipt)
        self.assertEqual(len(fixture.reads), len(diagnostic.ENTRIES) + 1)
        self.assertEqual(summary["blockingMatchCount"], 2)
        self.assertEqual(summary["packageCount"], 2)
        self.assertEqual(summary["suppressedUnsafeMatchCount"], 0)
        self.assertTrue(any(row["advisory"].startswith("GHSA-") for row in summary["advisories"]))
        self.assertEqual(summary["advisories"][0]["fixExpressions"], [">= 1.2.3, < 2.0.0"])
        text = json.dumps(summary)
        for forbidden in ("private.invalid", "locations", "/private", "dataSource"):
            self.assertNotIn(forbidden, text)

    def test_corrupt_nonreport_descriptor_is_rejected(self):
        fixture = Evidence(report())
        digest = fixture.layers[0]["digest"]
        fixture.blobs[digest] = b"xx"
        with self.assertRaisesRegex(diagnostic.Rejected, "BLOB_DESCRIPTOR_MISMATCH"):
            self.read(fixture)

    def test_hash_receipt_and_manifest_binding_fail_closed(self):
        for mutation in ("artifact", "image", "vuln", "unknown", "accepted", "manifest"):
            with self.subTest(mutation=mutation):
                fixture = Evidence(report())
                if mutation == "manifest":
                    fixture.document["annotations"]["kodex.dev/artifact-id"] = "imgart_wrong12345678"
                else:
                    key = {"artifact": "artifactId", "image": "imageDigest", "vuln": "vulnerabilityEvidenceSHA256",
                           "unknown": "callerAuthority", "accepted": "verdict"}[mutation]
                    fixture.receipt[key] = "ACCEPTED" if mutation == "accepted" else "wrong"
                    fixture.replace_receipt()
                with self.assertRaises(diagnostic.Rejected):
                    self.read(fixture)
        fixture = Evidence(report())
        with self.assertRaisesRegex(diagnostic.Rejected, "VULNERABILITY_HASH_MISMATCH"):
            diagnostic.read_evidence(fixture, ARTIFACT, IMAGE, "f" * 64)
        with self.assertRaisesRegex(diagnostic.Rejected, "EXPECTED_MANIFEST_DIGEST_MISMATCH"):
            diagnostic.read_evidence(fixture, ARTIFACT, IMAGE, fixture.sha, "sha256:" + "f" * 64)

    def test_descriptor_titles_sizes_and_chunk_sequence(self):
        for mutation in ("title", "order", "size", "field", "noncanonical"):
            with self.subTest(mutation=mutation):
                fixture = Evidence(report())
                if mutation == "title":
                    fixture.layers[0]["annotations"]["org.opencontainers.image.title"] = "../../identity/key"
                elif mutation == "order":
                    fixture.layers[0], fixture.layers[1] = fixture.layers[1], fixture.layers[0]
                elif mutation == "size":
                    fixture.layers[0]["size"] = diagnostic.PART + 1
                elif mutation == "field":
                    fixture.layers[0]["urls"] = ["https://private.invalid"]
                else:
                    d = fixture.layers[14]
                    d.update(digest="sha256:" + diagnostic.hash_bytes(b"x"), size=1)
                    fixture.blobs[d["digest"]] = b"x"
                with self.assertRaises(diagnostic.Rejected):
                    self.read(fixture)

    def test_unsafe_values_suppressed_counts_not_hidden(self):
        value = report()
        for bad in ("https://private.invalid", "1.2.3\nsecret", "$(unsafe)", "1.2.3/secret"):
            with self.subTest(bad=bad):
                candidate = copy.deepcopy(value)
                candidate["matches"][0]["vulnerability"]["fix"]["versions"] = [bad]
                fixture = Evidence(candidate)
                _, receipt, loaded = self.read(fixture)
                result = diagnostic.summary(loaded, receipt)
                self.assertEqual(result["blockingMatchCount"], 2)
                self.assertEqual(result["suppressedUnsafeMatchCount"], 1)
                self.assertNotIn(bad, json.dumps(result))
        fixture = Evidence(value)
        _, receipt, loaded = self.read(fixture)
        loaded["kodexPolicy"]["blockingMatchCount"] = 0
        with self.assertRaisesRegex(diagnostic.Rejected, "POLICY_COUNTS_MISMATCH"):
            diagnostic.summary(loaded, receipt)

    def test_ready_exact_owner_chain_and_negative_boundaries(self):
        diagnostic.validate_chain(*chain(), DEP_UID)
        for mutation in ("uid", "owner", "namespace", "scope", "ready", "container", "pvc", "deleting"):
            with self.subTest(mutation=mutation):
                pod, rs, dep = chain()
                if mutation == "uid":
                    dep["uid"] = "44444444-4444-4444-4444-444444444444"
                elif mutation == "owner":
                    rs["owners"][0]["controller"] = False
                elif mutation == "namespace":
                    pod["namespace"] = "foreign"
                elif mutation == "scope":
                    rs["scope"] = "promotion"
                elif mutation == "ready":
                    pod["registryReady"] = [False]
                elif mutation == "container":
                    pod["registry"][0]["image"] = "foreign.invalid/image"
                elif mutation == "pvc":
                    pod["dataPVC"] = ["other"]
                else:
                    pod["deleting"] = True
                with self.assertRaises(diagnostic.Rejected):
                    diagnostic.validate_chain(pod, rs, dep, DEP_UID)

    def test_projection_no_whole_metadata_labels_or_env_and_readonly_exec(self):
        for template in (diagnostic.COMMON, diagnostic.POD_TEMPLATE, diagnostic.LIST_TEMPLATE):
            self.assertNotIn(".spec.containers.env", template)
            self.assertNotIn("range .metadata.labels", template)
            self.assertNotIn("printf \"%v\" .metadata", template)
        registry = diagnostic.Registry.__new__(diagnostic.Registry)
        registry.pod_name = "synthetic-pod"
        calls = []
        registry.check = lambda: calls.append("check")
        registry.call = lambda args, bound: calls.append(args) or b"{}"
        self.assertEqual(registry.read(diagnostic.ROOT + "/blobs/sha256/aa/" + "a" * 64 + "/data", 2), b"{}")
        args = calls[1]
        self.assertEqual(args[:5], ["exec", "synthetic-pod", "--container", "registry", "--"])
        self.assertNotIn("-i", args)
        self.assertNotIn("-t", args)
        self.assertIn('readlink -f "$p"', args[7])
        self.assertEqual(calls[0], "check")
        self.assertEqual(calls[-1], "check")

    def test_duplicates_and_cli_no_raw_output(self):
        with self.assertRaisesRegex(diagnostic.Rejected, "DUPLICATE_JSON_FIELD"):
            diagnostic.decode(b'{"artifactId":1,"artifactId":2}')
        fixture = Evidence(report())
        out, err = io.StringIO(), io.StringIO()
        argv = ["--context", "k3d-kodex", "--kubeconfig", "/private/kube/config",
                "--deployment-uid", DEP_UID, "--artifact-ref", ARTIFACT,
                "--image-digest", IMAGE, "--vulnerability-sha256", fixture.sha]
        with patch.object(diagnostic, "Registry", return_value=fixture), redirect_stdout(out), redirect_stderr(err):
            self.assertEqual(diagnostic.main(argv), 0)
        result = json.loads(out.getvalue())
        self.assertEqual(result["artifactRef"], ARTIFACT)
        self.assertEqual(result["proof"], "OWNER_EXPECTED_CONTENT_HASH_BOUND_DIAGNOSTIC")
        self.assertNotIn("recipeRef", result)
        self.assertEqual(err.getvalue(), "")
        for forbidden in ("private.invalid", "locations", "rawReport", "/private"):
            self.assertNotIn(forbidden, out.getvalue())


if __name__ == "__main__":
    unittest.main()
