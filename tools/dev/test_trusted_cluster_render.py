"""Проверки удаления authority и сохранения закрытого сетевого допуска."""

import copy
import unittest

from trusted_cluster_render import materialize, verify, PROFILE


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
        self.assertEqual(result[-1]["spec"]["egress"], [])

    def test_label_without_exact_application_profile_is_rejected(self):
        result = materialize(self.resources, PROFILE)
        for entries in ([], [{"name": "KODEX_RPC_PROFILE", "value": "protected"}],
                        [{"name": "KODEX_RPC_PROFILE", "value": PROFILE}] * 2):
            candidate = copy.deepcopy(result)
            candidate[0]["spec"]["template"]["spec"]["containers"][0]["env"] = entries
            with self.assertRaisesRegex(ValueError, "EXPLICIT_RPC_PROFILE_REQUIRED"):
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
        self.resources.pop(3)
        with self.assertRaisesRegex(ValueError, "DENY_BY_DEFAULT_REQUIRED"):
            materialize(self.resources, PROFILE)


if __name__ == "__main__":
    unittest.main()
