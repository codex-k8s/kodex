"""Проверки удаления authority и сохранения закрытого сетевого допуска."""

import copy
import unittest

from trusted_cluster_render import materialize, verify, PROFILE, PROFILE_LABEL, selector_matches


def workload(name):
    return {"kind": "Deployment", "metadata": {"name": name, "namespace": "kodex-system"},
            "spec": {"template": {"metadata": {"labels": {"app.kubernetes.io/name": name}}, "spec": {
                "containers": [{"name": name}, {"name": "internal-rpc-authority-issuer"}],
                "initContainers": [{"name": "internal-rpc-authority-socket-init"}],
                "volumes": [{"name": "internal-rpc-authority-sockets", "emptyDir": {}}]}}}}


def policy(name, target, **rules):
    return {"kind": "NetworkPolicy", "metadata": {"name": name, "namespace": "kodex-system"},
            "spec": {"podSelector": {"matchLabels": {"app.kubernetes.io/name": target}},
                     "policyTypes": ["Ingress", "Egress"], **rules}}


def peer(name):
    return {"podSelector": {"matchLabels": {"app.kubernetes.io/name": name}}}


class TrustedClusterRenderTest(unittest.TestCase):
    def test_stt_removes_internal_identity_but_preserves_spool_and_external_trust(self):
        service = workload("stt-tts-service")
        spec = service["spec"]["template"]["spec"]
        names = ("workload-tls", "internal-ca", "authority-sockets", "stt-spool", "external-ca")
        spec["volumes"] = [{"name": name, "emptyDir": {}} for name in names]
        spec["containers"][0]["volumeMounts"] = [
            {"name": name, "mountPath": "/" + name} for name in names]
        before = copy.deepcopy(service)
        result = materialize([service], PROFILE)
        rendered = result[0]["spec"]["template"]["spec"]
        self.assertEqual(service, before)
        self.assertEqual({volume["name"] for volume in rendered["volumes"]},
                         {"stt-spool", "external-ca"})
        self.assertEqual({mount["name"] for mount in rendered["containers"][0]["volumeMounts"]},
                         {"stt-spool", "external-ca"})
        self.assertEqual(materialize(result, PROFILE), result)

    def test_database_bootstrap_profile_is_explicit_and_required(self):
        job = workload("kodex-postgresql-runtime-credentials")
        job["kind"] = "Job"
        job["spec"]["template"]["spec"]["containers"][0]["name"] = "reconcile"
        result = materialize([job], PROFILE)
        container = result[0]["spec"]["template"]["spec"]["containers"][0]
        self.assertEqual(container["env"], [{"name": "KODEX_RPC_PROFILE", "value": PROFILE}])
        self.assertEqual(materialize(result, PROFILE), result)
        container["env"] = []
        with self.assertRaisesRegex(ValueError, "EXPLICIT_DATABASE_PROFILE_REQUIRED"):
            verify(result, PROFILE)

    def setUp(self):
        self.resources = [workload("control-api-gateway"), workload("control-plane"),
                          workload("internal-rpc-authority-publisher"),
                          policy("gateway-deny", "control-api-gateway"),
                          policy("cp-deny", "control-plane"),
                          policy("gateway-egress", "control-api-gateway", egress=[{
                              "to": [peer("control-plane")], "ports": [{"port": 8443}]}]),
                          policy("cp-ingress", "control-plane", ingress=[{
                              "from": [peer("control-api-gateway")], "ports": [{"port": 8443}]}])]

    def test_explicit_idempotent_transform_preserves_input(self):
        before = copy.deepcopy(self.resources)
        result = materialize(self.resources, PROFILE)
        self.assertEqual(self.resources, before)
        self.assertEqual(materialize(result, PROFILE), result)
        self.assertFalse(any(resource["metadata"]["name"].startswith("internal-rpc-authority") for resource in result))
        spec = result[0]["spec"]["template"]["spec"]
        self.assertEqual(len(spec["containers"]), 1)
        self.assertEqual(spec["initContainers"], [])
        self.assertIn({"name": "KODEX_RPC_PROFILE", "value": PROFILE}, spec["containers"][0]["env"])
        with self.assertRaisesRegex(ValueError, "EXPLICIT_TRUSTED_PROFILE_REQUIRED"):
            materialize(self.resources, "")

    def test_each_direction_requires_exact_peer_and_port(self):
        for index, direction, key in ((5, "egress", "to"), (6, "ingress", "from")):
            for replacement in ({}, {"namespaceSelector": {}}, {"podSelector": {}}):
                resources = copy.deepcopy(self.resources)
                resources[index]["spec"][direction][0][key] = [replacement]
                with self.assertRaisesRegex(ValueError, "EXACT_RPC_NETWORK_EDGE_REQUIRED"):
                    materialize(resources, PROFILE)
            resources = copy.deepcopy(self.resources)
            resources[index]["spec"][direction][0]["ports"] = [{"port": 443}]
            with self.assertRaisesRegex(ValueError, "EXACT_RPC_NETWORK_EDGE_REQUIRED"):
                materialize(resources, PROFILE)

    def test_removing_authority_peer_does_not_allow_all(self):
        self.resources.append(policy("authority-egress", "control-plane", egress=[{
            "to": [peer("internal-rpc-authority-publisher")], "ports": [{"port": 8443}]}]))
        result = materialize(self.resources, PROFILE)
        remaining = next(item for item in result if item["metadata"]["name"] == "authority-egress")
        self.assertEqual(remaining["spec"]["egress"], [])

    def test_label_without_exact_application_profile_is_rejected(self):
        result = materialize(self.resources, PROFILE)
        for entries in ([], [{"name": "KODEX_RPC_PROFILE", "value": "protected"}],
                        [{"name": "KODEX_RPC_PROFILE", "value": PROFILE}] * 2):
            candidate = copy.deepcopy(result)
            candidate[0]["spec"]["template"]["spec"]["containers"][0]["env"] = entries
            with self.assertRaisesRegex(ValueError, "EXPLICIT_RPC_PROFILE_REQUIRED"):
                verify(candidate, PROFILE)

    def test_control_plane_provider_bootstrap_metadata_is_all_optional_or_absent(self):
        provider_entries = [{"name": name, "valueFrom": {"configMapKeyRef": {
            "name": "runtime-provider-openai-default-metadata", "key": key, "optional": True}}}
                            for name, key in {
                                "CONTROL_PLANE_DEFAULT_PROVIDER_SECRET_NAME": "secretName",
                                "CONTROL_PLANE_DEFAULT_PROVIDER_SECRET_UID": "secretUID",
                                "CONTROL_PLANE_DEFAULT_PROVIDER_SECRET_RESOURCE_VERSION": "secretResourceVersion",
                                "CONTROL_PLANE_DEFAULT_PROVIDER_CREDENTIAL_SHA256": "contentSHA256",
                            }.items()]
        resources = materialize(self.resources, PROFILE)
        control_plane = next(item for item in resources if item["metadata"]["name"] == "control-plane")
        container = control_plane["spec"]["template"]["spec"]["containers"][0]
        container["env"].extend(provider_entries)
        verify(resources, PROFILE)
        candidate = copy.deepcopy(resources)
        current = next(item for item in candidate if item["metadata"]["name"] == "control-plane")
        current_container = current["spec"]["template"]["spec"]["containers"][0]
        current_container["env"] = [entry for entry in current_container["env"]
                                    if entry["name"] != "CONTROL_PLANE_DEFAULT_PROVIDER_CREDENTIAL_SHA256"]
        with self.assertRaisesRegex(ValueError, "PARTIAL_PROVIDER_BOOTSTRAP_ENV_FORBIDDEN"):
            verify(candidate, PROFILE)
        candidate = copy.deepcopy(resources)
        current = next(item for item in candidate if item["metadata"]["name"] == "control-plane")
        current_container = current["spec"]["template"]["spec"]["containers"][0]
        next(entry for entry in current_container["env"]
             if entry["name"] == "CONTROL_PLANE_DEFAULT_PROVIDER_SECRET_NAME")["valueFrom"]["configMapKeyRef"].pop("optional")
        with self.assertRaisesRegex(ValueError, "OPTIONAL_PROVIDER_BOOTSTRAP_REFERENCE_REQUIRED"):
            verify(candidate, PROFILE)

    def test_runtime_projection_requires_both_network_directions(self):
        self.resources.extend([
            workload("runtime-controller"), workload("secret-broker"),
            policy("runtime-deny", "runtime-controller"), policy("broker-deny", "secret-broker")])
        edges = [("runtime-controller", "control-plane"), ("runtime-controller", "secret-broker"),
                 ("secret-broker", "control-plane"), ("control-plane", "secret-broker"),
                 ("control-api-gateway", "secret-broker")]
        for source, destination in edges:
            self.resources.extend([
                policy(source + "-to-" + destination, source, egress=[{
                    "to": [peer(destination)], "ports": [{"port": 8443}]}]),
                policy(destination + "-from-" + source, destination, ingress=[{
                    "from": [peer(source)], "ports": [{"port": 8443}]}])])
        result = materialize(self.resources, PROFILE)
        for policy_name in ("runtime-controller-to-secret-broker", "secret-broker-from-runtime-controller"):
            candidate = [resource for resource in result if resource["metadata"]["name"] != policy_name]
            with self.assertRaisesRegex(ValueError, "EXACT_RPC_NETWORK_EDGE_REQUIRED:runtime-controller:secret-broker"):
                verify(candidate, PROFILE)

    def test_public_service_and_missing_deny_are_rejected(self):
        resources = materialize(self.resources, PROFILE)
        resources.append({"kind": "Service", "metadata": {"name": "control-plane"}, "spec": {"type": "NodePort"}})
        with self.assertRaisesRegex(ValueError, "PUBLIC_INTERNAL_SERVICE_FORBIDDEN"):
            verify(resources, PROFILE)
        resources = [item for item in materialize(self.resources, PROFILE)
                     if item["metadata"]["name"] not in ("gateway-deny", "kodex-trusted-cluster-default-deny")]
        with self.assertRaisesRegex(ValueError, "DENY_BY_DEFAULT_REQUIRED"):
            verify(resources, PROFILE)

    def test_explicit_deny_covers_only_profile_and_rejects_collision(self):
        self.resources.pop(3)
        resources = materialize(self.resources, PROFILE)
        deny = next(item for item in resources if item["metadata"]["name"] == "kodex-trusted-cluster-default-deny")
        self.assertTrue(selector_matches(deny["spec"]["podSelector"], {PROFILE_LABEL: PROFILE}))
        self.assertFalse(selector_matches(deny["spec"]["podSelector"], {"app.kubernetes.io/name": "other"}))
        self.assertEqual(deny["spec"]["ingress"], [])
        self.assertEqual(deny["spec"]["egress"], [])
        deny["spec"]["podSelector"] = {}
        with self.assertRaisesRegex(ValueError, "PROFILE_DEFAULT_DENY_CONTRACT_INVALID"):
            materialize(resources, PROFILE)


if __name__ == "__main__":
    unittest.main()
