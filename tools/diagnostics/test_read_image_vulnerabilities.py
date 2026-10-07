import copy
import importlib.util
import io
import json
import pathlib
import unittest
from contextlib import redirect_stdout, redirect_stderr
from types import SimpleNamespace
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


def report():
    matches = []
    for advisory, name, severity, fixed in (
        ("GHSA-abcd-2345-vwx8", "npm", "High", True),
        ("CVE-2026-12345", "golang.org/x/net", "Critical", True),
        ("GO-2026-54321", "openssl", "High", False),
        ("CVE-2026-55555", "expat", "Medium", True),
        ("CVE-2026-66666", "apr", "Low", False),
        ("OTHER-777", "tool", "Negligible", False),
        ("OTHER-888", "tool-unknown", "Unknown", False),
    ):
        matches.append({"artifact":{"name":name,"version":"1.0.0","type":"go-module",
                                    "locations":[{"path":"/private/do-not-output"}]},
                        "vulnerability":{"id":advisory,"severity":severity,
                                         "fix":{"state":"fixed" if fixed else "not-fixed",
                                                "versions":[">= 1.2.3, < 2.0.0"] if fixed else []},
                                         "dataSource":"https://private.invalid/do-not-output"}})
    ignored = [copy.deepcopy(matches[0])]
    return {"matches":matches,"ignoredMatches":ignored,
            "kodexPolicy":{"schema":"kodex.dev/fix-available-high-or-critical/v1",
                           "policyRevision":88,"policySHA256":POLICY,
                           "highOrCriticalMatchCount":3,"blockingMatchCount":2,"unresolvedNoFixMatchCount":1}}


def projection(source, raw):
    groups = {}
    for ignored, rows in ((False, source["matches"]), (True, source.get("ignoredMatches", []))):
        for row in rows:
            artifact, v = row["artifact"], row["vulnerability"]
            kind, link = diagnostic.advisory_link(v["id"])
            fix_state = {"fixed":"FIXED","not-fixed":"NOT_FIXED","wont-fix":"WONT_FIX","unknown":"UNKNOWN"}[v["fix"]["state"]]
            severity = v["severity"].upper()
            item = dict(zip(diagnostic.FINDING_KEYS, (
                "", artifact["name"], artifact["version"], artifact["type"], v["id"], kind, link,
                severity, fix_state, sorted(set(v["fix"]["versions"])),
                not ignored and severity in ("CRITICAL","HIGH") and fix_state == "FIXED" and bool(v["fix"]["versions"]),
                ignored, 0)))
            item["ref"] = diagnostic.finding_ref(item)
            item["occurrences"] = groups.get(item["ref"], {}).get("occurrences", 0) + 1
            groups[item["ref"]] = item
    findings = [groups[key] for key in sorted(groups)]
    severity_counts = [{"severity":s,"matchCount":sum(f["occurrences"] for f in findings if f["severity"] == s)}
                       for s in diagnostic.SEVERITIES]
    values = {"schema":"kodex.dev/image-vulnerability-report/v1","artifactRef":ARTIFACT,"imageDigest":IMAGE,
              "reportSHA256":diagnostic.hash_bytes(raw),"sbomSHA256":diagnostic.hash_bytes(b"{}"),
              "scopeKind":"ORGANIZATION","organizationRef":"org_synthetic123456","projectRef":"",
              "recipeRef":"imgrec_synthetic123456","recipeVersion":1,"recipeGeneration":4,
              "buildRef":"imgbld_synthetic123456","buildVersion":3,"buildAttempt":1,
              "policyRevision":88,"policySHA256":POLICY,
              "matchCount":sum(f["occurrences"] for f in findings),
              "uniqueAdvisoryCount":len({f["advisoryId"] for f in findings}),
              "blockingMatchCount":sum(f["occurrences"] for f in findings if f["blocking"]),
              "unresolvedNoFixMatchCount":sum(f["occurrences"] for f in findings
                                            if not f["ignored"] and f["severity"] in ("HIGH","CRITICAL") and not f["blocking"]),
              "suppressedMatchCount":sum(f["occurrences"] for f in findings if f["ignored"]),
              "severityCounts":severity_counts,"findings":findings}
    return {key:values[key] for key in diagnostic.REPORT_KEYS}


class Evidence(diagnostic.Registry):
    def __init__(self, source=None, risk=False):
        self.blobs, self.reads = {}, []
        self.source = source or report()
        self.raw = (json.dumps(self.source, indent=2)+"\n").encode()
        self.report = projection(self.source, self.raw)
        self.projection = diagnostic.canonical(self.report)
        self.sha = diagnostic.hash_bytes(self.raw)
        self.projection_sha = diagnostic.hash_bytes(self.projection)
        self.risk = {}
        risk_raw = b""
        if risk:
            values = {"schema":"kodex.dev/image-risk-acceptance/v1","decisionRef":"imgrisk_synthetic123456",
                      "decisionVersion":1,"action":"ACCEPT_RISK","projectionSHA256":self.projection_sha,
                      "sourceAdmissionRevision":1,"sourceAdmissionReceiptSHA256":"d"*64,
                      "sourceEvidenceManifestDigest":"sha256:"+"e"*64,
                      "reason":"Private human explanation not for stdout",
                      "decidedByActorRef":"usr_privateactor123456","decidedAt":"2026-10-05T01:02:03.123456789Z"}
            for key in diagnostic.RISK_KEYS:
                if key in self.report and key not in values:
                    values[key] = self.report[key]
            self.risk = {key:values[key] for key in diagnostic.RISK_KEYS}
            risk_raw = diagnostic.canonical(self.risk)
        self.risk_sha = diagnostic.hash_bytes(risk_raw) if risk else ""
        self.verdict = "ACCEPTED" if risk or self.report["blockingMatchCount"] == 0 else "REJECTED"
        identity = diagnostic.hash_bytes(b"synthetic-public-key") if self.verdict == "ACCEPTED" else "not-applicable-rejected"
        self.receipt = {key:"c"*64 for key in diagnostic.RECEIPT_KEYS}
        self.receipt.update(version="v3",artifactId=ARTIFACT,imageDigest=IMAGE,
                            vulnerabilityEvidenceSHA256=self.sha,sbomSHA256=diagnostic.hash_bytes(b"{}"),
                            provenanceSHA256=diagnostic.hash_bytes(b"{}"),toolInventorySHA256=diagnostic.hash_bytes(b"{}"),
                            policyRevision="88",policySHA256=POLICY,verdict=self.verdict,signatureIdentity=identity,
                            admissionAttemptRef="imgadm_synthetic123456",admissionAttempt=2 if risk else 1,fence=3,
                            vulnerabilityReportProjectionSHA256=self.projection_sha,riskAcceptanceSHA256=self.risk_sha)
        binding = {key:self.receipt[key] for key in (
            "imageDigest","policyRevision","policySHA256","signatureIdentity","verdict","admissionAttemptRef",
            "admissionAttempt","fence","vulnerabilityReportProjectionSHA256","riskAcceptanceSHA256")}
        binding.update(version="v2",verification="cosign-key-v1")
        self.binding = encoded(binding)
        self.receipt["signatureSHA256"] = diagnostic.hash_bytes(self.binding)
        self.receipt_raw = encoded(self.receipt)
        self.receipt_sha = diagnostic.hash_bytes(self.receipt_raw)
        self.layers = []
        for title, media_type in diagnostic.ENTRIES:
            files = {"admission.receipt.json":self.receipt_raw,"signature.binding.json":self.binding,
                     "vulnerability.json.part-0":self.raw,"sbom.json.part-0":b"{}",
                     "vulnerability-report.json":self.projection,"risk-acceptance.json":risk_raw,
                     "image-digest.subject":(IMAGE+"\n").encode(),"cosign.pub":b"synthetic-public-key"}
            if title in files:
                data = files[title]
            elif ".part-" in title:
                data = b""
            elif title.endswith(".sigstore.json"):
                data = b"synthetic-not-a-signature-proof" if self.verdict == "ACCEPTED" and (
                    risk or title != "risk-acceptance.sigstore.json") else b""
            else:
                data = b"{}"
            digest = "sha256:"+diagnostic.hash_bytes(data)
            self.blobs[digest] = data
            self.layers.append({"annotations":{"org.opencontainers.image.title":title},
                                "mediaType":media_type,"digest":digest,"size":len(data)})
        self.blobs["sha256:"+diagnostic.hash_bytes(b"{}")] = b"{}"
        self.document = {"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json",
                         "artifactType":"application/vnd.kodex.image-admission-evidence.v5",
                         "config":{"mediaType":"application/vnd.kodex.image-admission-evidence.config.v5+json",
                                   "digest":"sha256:"+diagnostic.hash_bytes(b"{}"),"size":2},
                         "annotations":{"kodex.dev/artifact-id":ARTIFACT,"kodex.dev/image-digest":IMAGE,
                                        "kodex.dev/evidence-schema":"kodex.dev/image-admission-evidence/v5",
                                        "kodex.dev/policy-revision":"88","kodex.dev/policy-sha256":POLICY},
                         "layers":self.layers}

    def manifest(self, artifact, expected):
        data = encoded(self.document)
        return "sha256:"+diagnostic.hash_bytes(data), data

    def manifest_digest(self):
        return "sha256:"+diagnostic.hash_bytes(encoded(self.document))

    def read(self, path, bound):
        digest = "sha256:"+path.split("/")[-2]
        self.reads.append(digest)
        return self.blobs[digest]

    def replace(self, title, data):
        descriptor = next(d for d in self.layers if d["annotations"]["org.opencontainers.image.title"] == title)
        digest = "sha256:"+diagnostic.hash_bytes(data)
        self.blobs[digest] = data
        descriptor.update(digest=digest,size=len(data))

    def replace_receipt(self):
        self.receipt_raw = encoded(self.receipt)
        self.receipt_sha = diagnostic.hash_bytes(self.receipt_raw)
        self.replace("admission.receipt.json",self.receipt_raw)


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
    def read(self, fixture, **kwargs):
        pins = {"expected_manifest":fixture.manifest_digest(),"expected_projection":fixture.projection_sha,
                "expected_receipt":fixture.receipt_sha,"expected_risk":fixture.risk_sha}
        pins.update(kwargs)
        return diagnostic.read_evidence(fixture,ARTIFACT,IMAGE,fixture.sha,**pins)

    def test_v5_complete_counts_allseverity_ignored_and_private_metadata(self):
        fixture = Evidence()
        _, receipt, value = self.read(fixture)
        summary = diagnostic.summary(value,receipt)
        self.assertEqual(len(fixture.layers),26)
        self.assertEqual(len(fixture.reads),27)
        self.assertEqual(summary["matchCount"],8)
        self.assertEqual(summary["blockingMatchCount"],2)
        self.assertEqual(summary["suppressedMatchCount"],1)
        self.assertEqual([c["severity"] for c in summary["severityCounts"]],list(diagnostic.SEVERITIES))
        self.assertEqual(summary["unresolvedNoFixMatchCount"],1)
        self.assertNotIn("private",json.dumps(summary))
        self.assertNotIn("dataSource",json.dumps(summary))
        self.assertTrue(any(a["advisory"].startswith("GHSA") for a in summary["advisories"]))
        self.assertEqual(summary["fixSemantics"],"REPORTED_RANGE_NOT_RUNTIME_PIN")

    def test_no_fix_is_safe_accepted_without_risk_not_suppressed(self):
        source = report()
        source["matches"] = [source["matches"][2]]
        source["ignoredMatches"] = []
        source["kodexPolicy"].update(highOrCriticalMatchCount=1,blockingMatchCount=0,unresolvedNoFixMatchCount=1)
        fixture = Evidence(source)
        _, receipt, value = self.read(fixture)
        summary = diagnostic.summary(value,receipt)
        self.assertEqual(receipt["verdict"],"ACCEPTED")
        self.assertEqual(summary["blockingMatchCount"],0)
        self.assertEqual(summary["unresolvedNoFixMatchCount"],1)
        self.assertEqual(summary["advisories"],[])

    def test_risk_bytes_full_owner_tuple_priorpins_and_no_reason_actor_stdout(self):
        fixture = Evidence(risk=True)
        _, receipt, value = self.read(fixture)
        summary = diagnostic.summary(value,receipt)
        self.assertTrue(summary["riskAcceptancePresent"])
        self.assertNotIn("Private human",json.dumps(summary))
        self.assertNotIn("privateactor",json.dumps(summary))
        for mutation in ("sourceAdmissionReceiptSHA256","sourceEvidenceManifestDigest","recipeGeneration","projectionSHA256"):
            with self.subTest(mutation=mutation):
                risk = dict(fixture.risk)
                risk[mutation] = "invalid" if isinstance(risk[mutation],str) else risk[mutation]+1
                raw = diagnostic.canonical(risk)
                fixture.replace("risk-acceptance.json",raw)
                with self.assertRaises(diagnostic.Rejected):
                    self.read(fixture)

    def test_raw_exact_pretty_newline_hash_not_reserialized(self):
        fixture = Evidence()
        self.read(fixture)
        self.assertNotEqual(diagnostic.hash_bytes(encoded(fixture.source)),fixture.sha)
        fixture.replace("vulnerability.json.part-0",encoded(fixture.source))
        with self.assertRaisesRegex(diagnostic.Rejected,"VULNERABILITY_HASH_MISMATCH"):
            self.read(fixture)

    def test_hash_owner_receipt_projection_risk_and_manifest_closed(self):
        for key in ("expected_manifest","expected_projection","expected_receipt","expected_risk"):
            with self.subTest(key=key):
                fixture = Evidence(risk=True)
                wrong = "sha256:"+"f"*64 if key=="expected_manifest" else "f"*64
                with self.assertRaises(diagnostic.Rejected):
                    self.read(fixture,**{key:wrong})
        fixture = Evidence()
        with self.assertRaises(diagnostic.Rejected):
            self.read(fixture,expected_risk="f"*64)

    def test_v4_receiptv2_signaturev1_and_fakeunavailable_denied(self):
        for mutation in ("manifest","receipt","signature","technical"):
            with self.subTest(mutation=mutation):
                fixture = Evidence()
                if mutation=="manifest":
                    fixture.document["artifactType"]="application/vnd.kodex.image-admission-evidence.v4"
                elif mutation=="receipt":
                    fixture.receipt["version"]="v2"
                    fixture.replace_receipt()
                elif mutation=="signature":
                    binding = diagnostic.decode(fixture.binding)
                    binding["version"]="v1"
                    fixture.replace("signature.binding.json",encoded(binding))
                else:
                    fixture.replace("vulnerability-report.json",encoded({"schema":"kodex.dev/vulnerability-evidence-unavailable/v1","phase":"scan","reason":"predecessor workload failed"}))
                with self.assertRaises(diagnostic.Rejected):
                    self.read(fixture)

    def test_tampered_unknown_layer_size_order_and_chunk_denied(self):
        for mutation in ("hash","title","order","size","urls","chunk","layer_count"):
            with self.subTest(mutation=mutation):
                fixture = Evidence()
                if mutation=="hash":
                    fixture.blobs[fixture.layers[0]["digest"]]=b"xx"
                elif mutation=="title":
                    fixture.layers[0]["annotations"]["org.opencontainers.image.title"]="../../identity/key"
                elif mutation=="order":
                    fixture.layers[0],fixture.layers[1]=fixture.layers[1],fixture.layers[0]
                elif mutation=="size":
                    fixture.layers[0]["size"]=diagnostic.PART+1
                elif mutation=="urls":
                    fixture.layers[0]["urls"]=["https://private.invalid"]
                elif mutation=="chunk":
                    fixture.replace("vulnerability.json.part-1",b"x")
                else:
                    fixture.layers.pop()
                with self.assertRaises(diagnostic.Rejected):
                    self.read(fixture)

    def test_typed_projection_counts_refs_links_bounds_and_unknown_closed(self):
        fixture = Evidence()
        for mutation in ("counts","ignored","link","ref","unknown","scope","severity","noncanonical","findings_bound","bytes_bound"):
            with self.subTest(mutation=mutation):
                value = copy.deepcopy(fixture.report)
                if mutation=="counts":value["matchCount"]+=1
                elif mutation=="ignored":value["suppressedMatchCount"]=0
                elif mutation=="link":value["findings"][0]["advisoryUrl"]="https://private.invalid"
                elif mutation=="ref":value["findings"][0]["ref"]="f"*64
                elif mutation=="unknown":value["callerAuthority"]=True
                elif mutation=="scope":value["projectRef"]="prj_foreign123456"
                elif mutation=="severity":value["findings"][0]["severity"]="UNRECOGNIZED"
                elif mutation=="findings_bound":value["findings"]*=1500
                raw = diagnostic.canonical(value)
                if mutation=="noncanonical":raw+=b"\n"
                elif mutation=="bytes_bound":raw=b" "*(4*1024*1024+1)
                with self.assertRaises(diagnostic.Rejected):
                    diagnostic.validate_report(raw)

    def test_source_full_projection_ignored_rows_and_policy_exact(self):
        fixture = Evidence()
        for mutation in ("missing","ignored","policy","metadata"):
            with self.subTest(mutation=mutation):
                source = copy.deepcopy(fixture.source)
                if mutation=="missing":source["matches"].pop()
                elif mutation=="ignored":source["ignoredMatches"]=[]
                elif mutation=="policy":source["kodexPolicy"]["blockingMatchCount"]=0
                else:source["matches"][0]["artifact"]["name"]="different-package"
                with self.assertRaises(diagnostic.Rejected):
                    diagnostic.validate_source(encoded(source),fixture.report)

    def test_numeric_counts_depth_and_total_bound_are_not_weakened(self):
        fixture = Evidence()
        value = copy.deepcopy(fixture.report)
        value["severityCounts"][0]["matchCount"] = True
        with self.assertRaises(diagnostic.Rejected):
            diagnostic.validate_report(diagnostic.canonical(value))
        with patch.object(diagnostic, "TOTAL", 32):
            with self.assertRaisesRegex(diagnostic.Rejected, "EVIDENCE_TOTAL_EXCEEDS_BOUND"):
                self.read(fixture)
        with self.assertRaisesRegex(diagnostic.Rejected, "JSON_DEPTH_EXCEEDS_BOUND"):
            diagnostic.decode(("["*34+"0"+"]"*34).encode())
        with self.assertRaisesRegex(diagnostic.Rejected, "INVALID_JSON"):
            diagnostic.decode(b'{"text":"\\ud800"}')

    def test_risk_source_pins_and_fields_checked_even_with_matching_new_hash(self):
        fixture = Evidence(risk=True)
        for field in ("sourceAdmissionReceiptSHA256", "sourceEvidenceManifestDigest", "sourceAdmissionRevision",
                      "organizationRef", "buildRef", "policySHA256", "decidedAt", "reason"):
            with self.subTest(field=field):
                risk = copy.deepcopy(fixture.risk)
                risk[field] = 0 if isinstance(risk[field],int) else "invalid"
                if field == "reason":
                    risk[field] = "invalid\nreason"
                with self.assertRaises(diagnostic.Rejected):
                    diagnostic.validate_risk(diagnostic.canonical(risk), fixture.report, fixture.projection_sha)
        risk = copy.deepcopy(fixture.risk)
        risk["callerAuthority"] = True
        with self.assertRaises(diagnostic.Rejected):
            diagnostic.validate_risk(diagnostic.canonical(risk), fixture.report, fixture.projection_sha)

    def test_only_exact_owned_regular_private_kubeconfig_is_accepted(self):
        options = SimpleNamespace(context="k3d-kodex",deployment_uid=DEP_UID,kubeconfig="/home/s/.kube/config")
        valid = SimpleNamespace(st_mode=diagnostic.stat.S_IFREG|0o600,st_uid=diagnostic.os.getuid())
        with patch.object(diagnostic.os,"lstat",return_value=valid), patch.object(diagnostic.Registry,"call",return_value=b'["synthetic-pod"]'), patch.object(diagnostic.Registry,"check"):
            registry = diagnostic.Registry(options)
            self.assertEqual(set(registry.environment),{"PATH","HOME","KUBECONFIG","LANG"})
        for mode,uid in ((diagnostic.stat.S_IFLNK|0o600,valid.st_uid),(diagnostic.stat.S_IFREG|0o644,valid.st_uid),(diagnostic.stat.S_IFREG|0o600,valid.st_uid+1)):
            with patch.object(diagnostic.os,"lstat",return_value=SimpleNamespace(st_mode=mode,st_uid=uid)):
                with self.assertRaisesRegex(diagnostic.Rejected,"INVALID_KUBECONFIG_METADATA"):
                    diagnostic.Registry(options)

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


    def test_duplicates_cli_ownerpins_required_and_safe_output(self):
        with self.assertRaisesRegex(diagnostic.Rejected,"DUPLICATE_JSON_FIELD"):
            diagnostic.decode(b'{"artifactId":1,"artifactId":2}')
        fixture = Evidence(risk=True)
        argv = ["--context","k3d-kodex","--kubeconfig","/home/s/.kube/config","--deployment-uid",DEP_UID,
                "--artifact-ref",ARTIFACT,"--image-digest",IMAGE,"--vulnerability-sha256",fixture.sha,
                "--projection-sha256",fixture.projection_sha,"--admission-receipt-sha256",fixture.receipt_sha,
                "--evidence-manifest-digest",fixture.manifest_digest(),"--risk-acceptance-sha256",fixture.risk_sha]
        out,err = io.StringIO(),io.StringIO()
        with patch.object(diagnostic,"Registry",return_value=fixture),redirect_stdout(out),redirect_stderr(err):
            self.assertEqual(diagnostic.main(argv),0)
        result = json.loads(out.getvalue())
        self.assertEqual(result["artifactRef"],ARTIFACT)
        self.assertIn("NOT_SIGNATURE_VERIFICATION",result["proof"])
        self.assertEqual(err.getvalue(),"")
        for forbidden in ("private.invalid","locations","rawReport","/private","Private human","decidedByActor"):
            self.assertNotIn(forbidden,out.getvalue())


if __name__ == "__main__":
    unittest.main()
