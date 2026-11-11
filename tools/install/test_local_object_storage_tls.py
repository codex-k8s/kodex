"""Сквозной render S3 TLS, закрытый отказ и доставка CA архивным workers."""
import copy
from pathlib import Path
import subprocess
import tempfile
import unittest

import yaml

import local_object_storage_tls as profile

ROOT = Path(__file__).resolve().parents[2]


def resources(directory):
    return [r for path in sorted(directory.glob("*.yaml")) if path.name != "kustomization.yaml"
            for r in yaml.safe_load_all(path.read_text()) if r]


class LocalStorageTLS(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.directory = tempfile.TemporaryDirectory()
        root = Path(cls.directory.name)
        subprocess.run(["openssl", "req", "-x509", "-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:P-256",
                        "-nodes", "-days", "1", "-subj", "/CN=synthetic.invalid", "-keyout", str(root / "key"),
                        "-out", str(root / "ca")], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        cls.ca = (root / "ca").read_text()

    @classmethod
    def tearDownClass(cls):
        cls.directory.cleanup()

    def test_storage_probes_bootstrap_and_worker_network_share_exact_tls_route(self):
        source = resources(ROOT / "deploy/k8s/base/local-object-storage")
        before = copy.deepcopy(source)
        result = profile.storage(source, self.ca, "synthetic-local")
        self.assertEqual(source, before)
        pod = profile.one(result, "StatefulSet", "seaweedfs")["spec"]["template"]["spec"]
        container = pod["containers"][0]
        self.assertIn("-s3.port.https=0", container["args"])
        for name in ["startupProbe", "readinessProbe"]:
            command = container[name]["exec"]["command"][-1]
            self.assertIn(profile.ENDPOINT, command)
            self.assertIn("--cacert", command)
            self.assertNotIn("http://127.0.0.1:8333", command)
        job = profile.one(result, "Job", "seaweedfs-bucket-bootstrap")["spec"]["template"]["spec"]["containers"][0]
        self.assertIn("endpoint=" + profile.ENDPOINT, job["args"][0])
        self.assertIn("AWS_CA_BUNDLE", [e["name"] for e in job["env"]])
        certificate = profile.one(result, "Certificate", "seaweedfs-s3-tls")
        self.assertEqual(certificate["spec"]["dnsNames"], [profile.HOST])
        self.assertEqual({r["metadata"]["namespace"] for r in result if r["kind"] == "ConfigMap"}, {"kodex-system", "kodex-runtime"})
        peer = profile.one(result, "NetworkPolicy", "seaweedfs-exact-local-paths")["spec"]["ingress"][-1]["from"][0]
        self.assertIn("podSelector", peer)
        self.assertEqual(peer["namespaceSelector"]["matchLabels"]["kubernetes.io/metadata.name"], "kodex-runtime")

    def test_all_platform_consumers_and_ephemeral_archive_workers_get_ca(self):
        source = []
        for component in ["control-plane", "artifact-retention", "backup-controller", "session-archive"]:
            source += resources(ROOT / "deploy/k8s/base" / component)
        result = profile.consumers(source)
        for component in ["control-plane", "artifact-retention", "backup-controller", "session-archive"]:
            pod = profile.one(result, "Deployment", component)["spec"]["template"]["spec"]
            container = next(c for c in pod["containers"] if c["name"] == component)
            self.assertIn({"name": "SSL_CERT_FILE", "value": profile.CA_MOUNT + "/ca.crt"}, container["env"])
        self.assertEqual(profile.one(result, "ConfigMap", "session-archive-runtime")["data"]["SESSION_ARCHIVE_OBJECT_STORAGE_CA_CONFIG_MAP"], profile.CA_NAME)
        with self.assertRaises(ValueError):
            profile.consumers(result)

    def test_missing_consumer_duplicate_resource_and_invalid_ca_fail(self):
        with self.assertRaises(ValueError):
            profile.consumers([])
        source = resources(ROOT / "deploy/k8s/base/local-object-storage")
        with self.assertRaises(Exception):
            profile.storage(source, "invalid certificate", "synthetic-local")
        source.append(copy.deepcopy(profile.one(source, "StatefulSet", "seaweedfs")))
        with self.assertRaises(ValueError):
            profile.storage(source, self.ca, "synthetic-local")


if __name__ == "__main__":
    unittest.main()
