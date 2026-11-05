"""Проверки optional authority mode на обоих настоящих Kubernetes overlays, без живой среды и credentials."""
import copy
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile
import unittest

import yaml
from deployment_profile import Rejected
from oidc_authority_profile import policy_bytes, read_profile, transform, validate

ROOT = Path(__file__).resolve().parents[2]


def fixture():
    return {"schema": "kodex.oidc-authority-profile/v1", "managed_by": "git", "mode": "platform-managed",
            "policy": {"schema": "kodex.oidc-identity-policy/v1", "revision": 1,
                       "issuer": "https://identity.example.invalid/realm/synthetic",
                       "bootstrap_subjects": ["20000000-0000-4000-8000-000000000001"], "session_lifetime_seconds": 3600}}


class OIDCAuthorityProfileTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.renders = {}
        for name in ("web-only", "web-with-mattermost"):
            raw = subprocess.check_output(["kubectl", "kustomize", str(ROOT / "deploy/k8s/profiles" / name)], text=True, timeout=60)
            resources = [v for v in yaml.safe_load_all(raw) if v]
            for resource in resources:
                if resource.get("kind") == "Deployment" and resource.get("metadata", {}).get("name") in ("control-plane", "control-api-gateway"):
                    for container in resource["spec"]["template"]["spec"]["containers"]:
                        for item in container.get("env", []):
                            if item["name"] in ("CONTROL_PLANE_OIDC_ISSUER", "CONTROL_API_GATEWAY_OIDC_ISSUER"):
                                item["value"] = fixture()["policy"]["issuer"]
            cls.renders[name] = resources

    def test_claims_is_unchanged_in_both_overlays(self):
        profile = {"schema": "kodex.oidc-authority-profile/v1", "managed_by": "git", "mode": "claims"}
        for resources in self.renders.values():
            self.assertEqual(transform(resources, profile), resources)

    def test_platform_changes_only_two_workloads_and_immutable_policy(self):
        for resources in self.renders.values():
            original = copy.deepcopy(resources)
            result = transform(resources, fixture())
            self.assertEqual(resources, original)
            self.assertEqual(transform(resources, fixture()), result)
            self.assertEqual(len(result), len(resources) + 1)
            policy = result[-1]
            self.assertTrue(policy["immutable"])
            self.assertEqual(policy["kind"], "ConfigMap")
            raw = policy["data"]["policy.json"].encode()
            self.assertEqual(json.loads(raw), fixture()["policy"])
            digest = hashlib.sha256(raw).hexdigest()
            self.assertEqual(policy["metadata"]["annotations"]["kodex.dev/oidc-identity-policy-sha256"], digest)
            for before, after in zip(resources, result):
                name = before.get("metadata", {}).get("name")
                if before.get("kind") != "Deployment" or name not in ("control-plane", "control-api-gateway"):
                    self.assertEqual(before, after)
                    continue
                pod = after["spec"]["template"]["spec"]
                container = next(c for c in pod["containers"] if c["name"] == name)
                prefix = "CONTROL_PLANE" if name == "control-plane" else "CONTROL_API_GATEWAY"
                env = {v["name"]: v for v in container["env"]}
                self.assertEqual(env[prefix + "_OIDC_AUTHORITY_MODE"]["value"], "platform-managed")
                self.assertEqual(after["spec"]["template"]["metadata"]["annotations"]["kodex.dev/oidc-identity-policy-sha256"], digest)
                if name == "control-plane":
                    volume = next(v for v in pod["volumes"] if v["name"] == "oidc-identity-policy")
                    self.assertEqual(volume["configMap"]["name"], policy["metadata"]["name"])
                    mount = next(v for v in container["volumeMounts"] if v["name"] == "oidc-identity-policy")
                    self.assertTrue(mount["readOnly"])
                    self.assertEqual(env[prefix + "_OIDC_IDENTITY_POLICY_FILE"]["value"], mount["mountPath"] + "/policy.json")
                else:
                    self.assertNotIn(prefix + "_OIDC_IDENTITY_POLICY_FILE", env)
            self.assertEqual(list(yaml.safe_load_all(yaml.safe_dump_all(result))), result)

    def test_unknown_authority_and_ambiguous_owner_inputs_fail_closed(self):
        invalid = []
        value = fixture(); value["mode"] = "automatic"; invalid.append(value)
        value = fixture(); value["policy"]["organization_id"] = "client-selected"; invalid.append(value)
        value = fixture(); value["policy"]["revision"] = True; invalid.append(value)
        value = fixture(); value["policy"]["revision"] = 0; invalid.append(value)
        value = fixture(); value["policy"]["bootstrap_subjects"] = []; invalid.append(value)
        value = fixture(); value["policy"]["bootstrap_subjects"] *= 2; invalid.append(value)
        value = fixture(); value["policy"]["bootstrap_subjects"] = ["synthetic-owner-name"]; invalid.append(value)
        value = fixture(); value["policy"]["session_lifetime_seconds"] = 43201; invalid.append(value)
        value = fixture(); value["policy"]["issuer"] = "http://identity.example.invalid"; invalid.append(value)
        for value in invalid:
            with self.assertRaises(Rejected):
                validate(value)
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "profile.json"
            path.write_text('{"schema":"one","schema":"two"}')
            with self.assertRaises(Rejected):
                read_profile(path)

    def test_issuer_and_existing_config_collisions_are_not_overwritten(self):
        for resources in self.renders.values():
            profile = fixture(); profile["policy"]["issuer"] = "https://other.example.invalid"
            with self.assertRaises(Rejected):
                transform(resources, profile)
            result = transform(resources, fixture())
            with self.assertRaises(Rejected):
                transform(result, fixture())

    def test_policy_digest_matches_the_go_domain_fixture(self):
        document = json.loads((ROOT / "services/internal/control-plane/internal/domain/types/value/testdata/oidc-identity-policy.json").read_text())
        canonical = validate({"schema": "kodex.oidc-authority-profile/v1", "managed_by": "git", "mode": "platform-managed", "policy": document["policy"]})["policy"]
        self.assertEqual(hashlib.sha256(policy_bytes(canonical)).hexdigest(), document["sha256"])


if __name__ == "__main__":
    unittest.main()
