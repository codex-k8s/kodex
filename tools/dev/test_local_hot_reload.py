"""Герметичные проверки identity/source/cache без доступа к Kubernetes."""

import copy
from pathlib import Path
import tempfile
import unittest

from local_hot_reload import materialize, verify, prepare_mask_targets


class HotReloadContractTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="kodex-host-contract-")
        self.addCleanup(self.temporary.cleanup)
        root = Path(self.temporary.name)
        self.source = str(root / "source")
        self.cache = str(root / "cache")
        Path(self.source).mkdir()
        (Path(self.source) / ".git").mkdir()
        (Path(self.source) / ".env").touch()
        prepare_mask_targets(self.source)
        Path(self.cache).mkdir()
        self.arguments = (self.source, self.cache, 1000, 1000)
        self.resources = [{"apiVersion": "apps/v1", "kind": "Deployment",
                           "metadata": {"name": "example", "namespace": "kodex-system"},
                           "spec": {"template": {"metadata": {"labels": {
                               "kodex.dev/security-profile": "trusted-cluster"}}, "spec": {
                                   "containers": [{"name": "example", "volumeMounts": [
                                       {"name": "source", "mountPath": "/workspace", "readOnly": True},
                                       {"name": "cache", "mountPath": "/go/build-cache"}]}],
                                   "volumes": [
                                       {"name": "source", "hostPath": {"path": self.source}},
                                       {"name": "cache", "hostPath": {"path": self.cache + "/go-build-v2/example"}}],
                               }}}}]

    def test_materialize_and_verify_do_not_mutate_input(self):
        before = copy.deepcopy(self.resources)
        result = materialize(self.resources, *self.arguments)
        self.assertEqual(self.resources, before)
        verify(result, *self.arguments)
        self.assertEqual(materialize(result, *self.arguments), result)
        security = result[0]["spec"]["template"]["spec"]["containers"][0]["securityContext"]
        self.assertEqual(security["runAsUser"], 1000)
        self.assertFalse(security["allowPrivilegeEscalation"])
        spec = result[0]["spec"]["template"]["spec"]
        self.assertTrue(security["readOnlyRootFilesystem"])
        self.assertIn({"name": "kodex-dev-tmp", "mountPath": "/tmp"}, spec["containers"][0]["volumeMounts"])
        self.assertIn({"name": "kodex-dev-tmp", "emptyDir": {"sizeLimit": "4Gi"}}, spec["volumes"])

    def test_scanner_socket_identity_matches_source_container(self):
        self.resources[0]["spec"]["template"]["spec"]["containers"].append({
            "name": "skill-scanner", "securityContext": {
                "runAsUser": 10001, "runAsGroup": 10001, "runAsNonRoot": True,
                "allowPrivilegeEscalation": False, "readOnlyRootFilesystem": True,
                "capabilities": {"drop": ["ALL"]},
            }})
        result = materialize(self.resources, *self.arguments)
        scanner = result[0]["spec"]["template"]["spec"]["containers"][1]
        self.assertEqual(scanner["securityContext"]["runAsUser"], 1000)
        self.assertEqual(scanner["securityContext"]["runAsGroup"], 1000)
        self.assertTrue(scanner["securityContext"]["readOnlyRootFilesystem"])
        self.assertEqual(materialize(result, *self.arguments), result)
        for mutation in (
            lambda item: item["securityContext"].update(runAsUser=0),
            lambda item: item["securityContext"].update(runAsGroup=10001),
            lambda item: item["securityContext"].update(allowPrivilegeEscalation=True),
            lambda item: item.update(volumeMounts=[{"name": "cache", "mountPath": "/cache"}]),
        ):
            with self.subTest(mutation=mutation):
                invalid = copy.deepcopy(result)
                mutation(invalid[0]["spec"]["template"]["spec"]["containers"][1])
                with self.assertRaises(ValueError):
                    verify(invalid, *self.arguments)

    def test_rejects_root_writable_source_escape_and_missing_mask(self):
        mutations = (
            lambda spec: spec["containers"][0]["securityContext"].update(runAsUser=0),
            lambda spec: spec["containers"][0]["securityContext"].update(allowPrivilegeEscalation=True),
            lambda spec: spec["containers"][0]["securityContext"].update(readOnlyRootFilesystem=False),
            lambda spec: spec["containers"][0]["securityContext"].update(capabilities={"drop": ["ALL"], "add": ["CHOWN"]}),
            lambda spec: spec["containers"][0]["volumeMounts"][0].update(readOnly=False),
            lambda spec: spec["volumes"][1]["hostPath"].update(path="/home/s"),
            lambda spec: spec["volumes"][1]["hostPath"].update(path=self.cache + "/go-build-v2/../../outside"),
            lambda spec: spec["containers"][0]["volumeMounts"].pop(),
        )
        for mutate in mutations:
            with self.subTest(mutation=mutate):
                result = materialize(self.resources, *self.arguments)
                mutate(result[0]["spec"]["template"]["spec"])
                with self.assertRaises(ValueError):
                    verify(result, *self.arguments)

    def test_rejects_implicit_profile_and_authority_container(self):
        self.resources[0]["spec"]["template"]["metadata"]["labels"].clear()
        with self.assertRaises(ValueError):
            materialize(self.resources, *self.arguments)
        self.resources[0]["spec"]["template"]["metadata"]["labels"]["kodex.dev/security-profile"] = "trusted-cluster"
        self.resources[0]["spec"]["template"]["spec"]["containers"][0]["name"] = "internal-rpc-authority-issuer"
        with self.assertRaises(ValueError):
            materialize(self.resources, *self.arguments)

    def test_rejects_cache_inside_source_and_root_identity(self):
        with self.assertRaises(ValueError):
            materialize(self.resources, self.source, self.source + "/cache", 1000, 1000)
        with self.assertRaises(ValueError):
            materialize(self.resources, self.source, self.cache, 0, 1000)

    def test_prepare_is_idempotent_and_does_not_change_existing_permissions(self):
        directory = Path(self.source) / ".agents"
        directory.chmod(0o750)
        prepare_mask_targets(self.source)
        self.assertEqual(directory.stat().st_mode & 0o777, 0o750)
        self.assertEqual((Path(self.source) / ".kodex-dev").stat().st_mode & 0o777, 0o700)

    def test_rejects_missing_or_symlinked_mountpoints(self):
        target = Path(self.source) / ".kodex-dev"
        target.rmdir()
        with self.assertRaises(ValueError):
            materialize(self.resources, *self.arguments)
        target.symlink_to(self.cache, target_is_directory=True)
        with self.assertRaises(ValueError):
            prepare_mask_targets(self.source)
        with self.assertRaises(ValueError):
            materialize(self.resources, *self.arguments)


if __name__ == "__main__":
    unittest.main()
