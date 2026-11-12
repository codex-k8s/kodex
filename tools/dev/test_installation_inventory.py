"""Проверки metadata-only inventory без credentials и Kubernetes."""

import io
import json
from pathlib import Path
import tempfile
import unittest
import urllib.error
from types import SimpleNamespace
from unittest.mock import patch

import installation_inventory as inventory


class InventoryTests(unittest.TestCase):
    def test_local_path_requires_exact_node_container_claim_and_directory(self):
        snapshot = {"context": "k3d-fixture", "nodes": [{"name": "node", "uid": "node-uid"}],
                    "volumes": [{"identity": {"uid": "volume-uid"}, "claim": {"uid": "claim-uid", "namespace": "kodex-system", "name": "data"},
                                 "localPath": "/var/lib/rancher/k3s/storage/pvc-claim-uid_kodex-system_data", "csiDriver": "",
                                 "nodeAffinity": {"required": {"nodeSelectorTerms": [{"matchExpressions": [
                                     {"key": "kubernetes.io/hostname", "operator": "In", "values": ["node"]}]}]}}}]}
        payloads = [json.dumps([{"name": "node", "role": "agent", "runtimeLabels": {"k3d.cluster": "fixture"}}]).encode(),
                    json.dumps("a" * 64).encode(), b"fixture", snapshot["volumes"][0]["localPath"].encode(), b"10:20:41c0"]
        with patch.object(inventory.subprocess, "run", side_effect=[SimpleNamespace(stdout=value) for value in payloads]):
            result = inventory.k3d_storage(snapshot)
            self.assertEqual(result["volumes"][0]["inode"], "20")
            self.assertEqual(result["volumes"][0]["containerID"], "a" * 64)
        for position, changed in ((2, b"foreign"), (3, b"/foreign/path"), (4, b"10:20:a1ff")):
            outputs = list(payloads)
            outputs[position] = changed
            with patch.object(inventory.subprocess, "run", side_effect=[SimpleNamespace(stdout=value) for value in outputs]):
                with self.assertRaises(ValueError):
                    inventory.k3d_storage(snapshot)
        snapshot["volumes"][0]["localPath"] = "/foreign/data"
        with patch.object(inventory.subprocess, "run", side_effect=[SimpleNamespace(stdout=value) for value in payloads[:3]]):
            with self.assertRaises(ValueError):
                inventory.k3d_storage(snapshot)

    def test_metadata_projection_excludes_secret_and_last_applied(self):
        source = {"metadata": {"name": "fixture", "namespace": "kodex-system", "uid": "fixture-uid",
                               "resourceVersion": "17", "labels": {"app.kubernetes.io/part-of": "kodex", "private": "SENSITIVE"},
                               "annotations": {"kubectl.kubernetes.io/last-applied-configuration": "SENSITIVE",
                                               "meta.helm.sh/release-name": "fixture"},
                               "ownerReferences": [{"uid": "owner-uid", "kind": "Job", "name": "fixture", "private": "SENSITIVE"}]},
                  "data": {"key": "SENSITIVE"}, "spec": {"private": "SENSITIVE"}}
        result = inventory.metadata(source, "secrets", "v1")
        self.assertNotIn("SENSITIVE", json.dumps(result))
        self.assertEqual(result["uid"], "fixture-uid")
        self.assertEqual(result["helm"], {"meta.helm.sh/release-name": "fixture"})
        self.assertEqual(result["ownerReferences"][0]["uid"], "owner-uid")

    def test_identity_fields_and_duplicate_json_fail_closed(self):
        for field in ("name", "uid", "resourceVersion"):
            source = {"metadata": {"name": "fixture", "uid": "uid", "resourceVersion": "1"}}
            del source["metadata"][field]
            with self.assertRaises(ValueError):
                inventory.metadata(source, "pods", "v1")
        with self.assertRaises(ValueError):
            inventory.decode(b'{"uid":"first","uid":"second"}')
        with self.assertRaises(ValueError):
            inventory.decode(b" " * (inventory.MAX_RESPONSE + 1))

    def test_metadata_negotiation_has_no_full_secret_fallback(self):
        requests = []
        class Opener:
            def open(self, request, timeout):
                requests.append(request)
                return io.BytesIO(b'{"kind":"Secret","apiVersion":"v1","data":{"key":"SENSITIVE"}}')
        client = inventory.Client.__new__(inventory.Client)
        client.endpoint = "https://example.invalid"
        client.headers = {}
        client.opener = Opener()
        with self.assertRaises(ValueError):
            client.get("/api/v1/namespaces/kodex-system/secrets/fixture", "PartialObjectMetadata")
        self.assertEqual(requests[0].get_header("Accept"), "application/json;as=PartialObjectMetadata;g=meta.k8s.io;v=v1")
        self.assertEqual(requests[0].get_method(), "GET")
        self.assertNotIn("q=", requests[0].get_header("Accept"))
        for path in ("https://foreign.invalid", "//foreign.invalid", "/api/../secrets"):
            with self.assertRaises(ValueError):
                client.get(path)
        self.assertEqual(len(requests), 1)

    def test_paginated_inventory_and_empty_null_list(self):
        client = inventory.Client.__new__(inventory.Client)
        calls = []
        def get(path, partial):
            calls.append((path, partial))
            if len(calls) == 1:
                return {"metadata": {"continue": "next+token"}, "items": [{"metadata": {"uid": "first"}}]}
            return {"metadata": {}, "items": None}
        client.get = get
        self.assertEqual(len(client.listing("/api/v1/pods")), 1)
        self.assertIn("continue=next%2Btoken", calls[1][0])
        self.assertTrue(all(partial == "PartialObjectMetadataList" for _, partial in calls))

    def test_links_keep_only_exact_references_without_env_values(self):
        item = {"metadata": {"name": "fixture", "namespace": "kodex-system", "uid": "uid", "resourceVersion": "1"},
                "spec": {"template": {"spec": {"serviceAccountName": "fixture-service",
                    "containers": [{"image": "example.invalid/image@sha256:" + "a" * 64,
                        "env": [{"name": "PRIVATE", "value": "SENSITIVE"},
                                {"name": "KEY", "valueFrom": {"secretKeyRef": {"name": "fixture-key", "key": "key"}}}],
                        "envFrom": [{"configMapRef": {"name": "fixture-config"}}]}],
                    "volumes": [{"persistentVolumeClaim": {"claimName": "fixture-data"}},
                                {"secret": {"secretName": "fixture-tls"}}]}}}}
        result = inventory.object_links(item, "deployments", "apps/v1")
        self.assertNotIn("SENSITIVE", json.dumps(result))
        refs = {(value["resource"], value["name"]) for value in result["references"]}
        self.assertEqual(refs, {("serviceaccounts", "fixture-service"), ("secrets", "fixture-key"),
                               ("configmaps", "fixture-config"), ("persistentvolumeclaims", "fixture-data"),
                               ("secrets", "fixture-tls")})
        event = {"metadata": item["metadata"], "message": "SENSITIVE",
                 "involvedObject": {"kind": "Pod", "uid": "pod-uid", "name": "pod"}}
        self.assertNotIn("SENSITIVE", json.dumps(inventory.object_links(event, "events", "v1")))
        certificate = {"metadata": item["metadata"], "spec": {"secretName": "fixture-tls"}}
        self.assertEqual(inventory.object_links(certificate, "certificates", "cert-manager.io/v1")["references"][0]["relation"], "PRODUCES")

    def test_bundle_producer_requires_exact_namespace_selector(self):
        bundle = {"metadata": {"name": "fixture-ca", "uid": "uid", "resourceVersion": "1"},
                  "spec": {"target": {"namespaceSelector": {"matchLabels": {"kubernetes.io/metadata.name": "kodex-system"}},
                                      "configMap": {"key": "ca.pem"}}}}
        result = inventory.object_links(bundle, "bundles", "trust.cert-manager.io/v1alpha1")
        self.assertEqual(result["references"], [{"resource": "configmaps", "name": "fixture-ca", "namespace": "kodex-system", "relation": "PRODUCES"}])
        bundle["spec"]["target"]["namespaceSelector"] = {}
        self.assertEqual(inventory.object_links(bundle, "bundles", "trust.cert-manager.io/v1alpha1")["references"], [])

    def test_output_requires_private_directory_and_never_overwrites(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "inventory.json"
            inventory.write_private(path, {"status": "COMPLETE"})
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            with self.assertRaises(FileExistsError):
                inventory.write_private(path, {})
            self.assertEqual(json.loads(path.read_text()), {"status": "COMPLETE"})
            path.unlink()
            path.symlink_to(Path(directory) / "absent")
            with self.assertRaises(FileExistsError):
                inventory.write_private(path, {})
            self.assertFalse((Path(directory) / "absent").exists())
            Path(directory).chmod(0o755)
            with self.assertRaises(ValueError):
                inventory.write_private(Path(directory) / "new.json", {})
            Path(directory).chmod(0o700)


if __name__ == "__main__":
    unittest.main()
