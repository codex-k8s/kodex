"""Отрицательные проверки адресного plan без Kubernetes и реальных credentials."""

import copy
import unittest

import installation_cleanup as cleanup


def item(resource, name, namespace="kodex-system", uid=None, labels=None, managers=None, owners=None):
    """Создать только synthetic identity с явными ownership markers."""
    return {"resource": resource, "apiVersion": "v1", "name": name, "namespace": namespace,
            "uid": uid or name + "-uid", "resourceVersion": "17", "labels": labels or {}, "managers": managers or [],
            "helm": {}, "ownerReferences": owners or [], "deleting": False}


def fixture():
    """Отдельная synthetic установка имеет одну node и сохраняемый kube-system."""
    return {"schema": "kodex.installation-inventory/v1", "status": "COMPLETE", "incomplete": [],
            "context": "fixture", "clusterUID": "cluster-uid", "endpoint": "https://example.invalid", "caSHA256": "b" * 64, "nodes": [item("nodes", "node", "", uid="node-uid")],
            "namespaces": [item("namespaces", "kodex-system", "", managers=["kodex-install"]),
                           item("namespaces", "kube-system", "")], "volumes": [], "objects": [], "links": []}


def plan(value):
    """Передать exact synthetic target независимо от содержимого inventory."""
    return cleanup.plan_inventory(value, "fixture", "cluster-uid", ["node-uid"], "a" * 40)


class CleanupTests(unittest.TestCase):
    def test_preserved_namespace_retains_owned_objects_volumes_and_helm_resources(self):
        value = fixture()
        value["namespaces"].append(item("namespaces", "kodex-trust", "", managers=["kodex-install"]))
        value["namespaces"].append(item("namespaces", "cert-manager", ""))
        application = item("deployments", "application", managers=["kodex-install"])
        service = item("services", "registry", "kodex-trust", managers=["kodex-install"])
        claim = item("persistentvolumeclaims", "data", "kodex-trust", managers=["kodex-install"])
        child = item("pods", "child", "kodex-trust", owners=[{"uid": service["uid"]}])
        helm = item("clusterroles", "trust-role", "")
        helm["helm"] = {"meta.helm.sh/release-name": "kodex-trust-manager", "meta.helm.sh/release-namespace": "cert-manager"}
        value["objects"] = [application, service, claim, child, helm]
        value["volumes"] = [{"claim": {"uid": claim["uid"], "namespace": "kodex-trust", "name": "data"}, "reclaimPolicy": "Delete"}]
        result = cleanup.plan_inventory(value, "fixture", "cluster-uid", ["node-uid"], "a" * 40,
                                        preserve_namespaces=["kodex-trust", "cert-manager"])
        self.assertEqual(result["status"], "OWNERSHIP_CLASSIFIED")
        self.assertEqual(result["preservedNamespaces"], ["cert-manager", "kodex-trust"])
        self.assertEqual([row["name"] for row in result["namespaces"]], ["kodex-system"])
        self.assertEqual({row["identity"]["uid"] for row in result["deleteCandidates"]}, {application["uid"]})
        self.assertEqual(result["volumes"], [])

    def test_preserved_volume_is_excluded_from_physical_erase_plan(self):
        value = fixture()
        value["namespaces"].append(item("namespaces", "kodex-trust", "", managers=["kodex-install"]))
        retained = item("persistentvolumeclaims", "registry", "kodex-trust", managers=["kodex-install"])
        selected = item("persistentvolumeclaims", "database", managers=["kodex-install"])
        value["objects"] = [retained, selected]
        value["volumes"] = [{"identity": item("persistentvolumes", name, "", uid=name),
                             "claim": {"uid": claim["uid"], "namespace": claim["namespace"], "name": claim["name"]},
                             "reclaimPolicy": "Delete"} for name, claim in [("keep", retained), ("erase", selected)]]
        value["physicalStorage"] = {"schema": "kodex.k3d-local-storage/v1", "containers": {"node": "b" * 64},
                                    "volumes": [{"volumeUID": "keep"}, {"volumeUID": "erase"}]}
        original = copy.deepcopy(value)
        result = cleanup.plan_inventory(value, "fixture", "cluster-uid", ["node-uid"], "a" * 40,
                                        preserve_namespaces=["kodex-trust"])
        self.assertEqual(result["physicalStorage"]["volumes"], [{"volumeUID": "erase"}])
        self.assertEqual(value, original)

    def test_preserved_namespace_consumer_still_blocks_shared_application_secret(self):
        value = fixture()
        value["namespaces"].append(item("namespaces", "kodex-trust", "", managers=["kodex-install"]))
        secret = item("secrets", "shared", managers=["kodex-install"])
        consumer = item("deployments", "consumer", "kodex-trust", managers=["kodex-install"])
        value["objects"] = [secret, consumer]
        value["links"] = [{"identity": consumer, "references": [{"namespace": "kodex-system", "resource": "secrets", "name": "shared", "relation": "CONSUMES"}]}]
        result = cleanup.plan_inventory(value, "fixture", "cluster-uid", ["node-uid"], "a" * 40,
                                        preserve_namespaces=["kodex-trust"])
        self.assertEqual(result["status"], "OWNERSHIP_REVIEW_REQUIRED")
        self.assertEqual(result["shared"][0]["foreignConsumerUIDs"], [consumer["uid"]])

    def test_preserved_certificate_consumer_blocks_cluster_issuer_deletion(self):
        value = fixture()
        value["namespaces"].append(item("namespaces", "kodex-trust", "", managers=["kodex-install"]))
        issuer = item("clusterissuers", "installation-ca", "", managers=["kodex-install"],
                      labels={"app.kubernetes.io/part-of": "kodex"})
        certificate = item("certificates", "service-tls", "kodex-trust", managers=["kodex-install"])
        value["objects"] = [issuer, certificate]
        value["links"] = [{"identity": certificate, "references": [{"namespace": "", "resource": "clusterissuers", "name": "installation-ca", "relation": "CONSUMES"}]}]
        result = cleanup.plan_inventory(value, "fixture", "cluster-uid", ["node-uid"], "a" * 40,
                                        preserve_namespaces=["kodex-trust"])
        self.assertEqual(result["status"], "OWNERSHIP_REVIEW_REQUIRED")
        self.assertEqual(result["shared"][0]["identity"]["uid"], issuer["uid"])

    def test_preserved_scope_is_exact_observed_and_digest_bound(self):
        value = fixture()
        for names in [["unknown"], ["kube-system", "kube-system"], ["../kube-system"], [""]]:
            with self.subTest(names=names), self.assertRaises(ValueError):
                cleanup.plan_inventory(value, "fixture", "cluster-uid", ["node-uid"], "a" * 40,
                                       preserve_namespaces=names)
        default = plan(value)
        self.assertNotIn("preservedNamespaces", default)
        narrowed = cleanup.plan_inventory(value, "fixture", "cluster-uid", ["node-uid"], "a" * 40,
                                          preserve_namespaces=["kube-system"])
        self.assertNotEqual(default["digest"], narrowed["digest"])

    def test_orphaned_ingress_certificate_requires_both_recorded_deletions_and_exact_references(self):
        value = fixture()
        ingress = item("ingresses", "web", managers=["kodex-install"])
        previous = item("certificates", "web-tls", uid="old-cert", managers=["kodex-install"])
        replacement = item("certificates", "web-tls", uid="new-cert", managers=["cert-manager-ingress-shim"])
        secret = {"namespace": "kodex-system", "resource": "secrets", "name": "web-tls", "relation": "PRODUCES"}
        issuer = {"namespace": "", "resource": "clusterissuers", "name": "fixture", "relation": "CONSUMES"}
        value["objects"] = [ingress, previous, replacement]
        value["links"] = [{"identity": ingress, "references": [{**secret, "relation": "CONSUMES"}]},
                          {"identity": previous, "references": [secret, issuer]},
                          {"identity": replacement, "references": copy.deepcopy([secret, issuer])}]
        for retired in (set(), {ingress["uid"]}, {previous["uid"]}):
            result = cleanup.plan_inventory(value, "fixture", "cluster-uid", ["node-uid"], "a" * 40, retired)
            self.assertEqual(result["status"], "OWNERSHIP_REVIEW_REQUIRED")
        retired = {ingress["uid"], previous["uid"]}
        result = cleanup.plan_inventory(value, "fixture", "cluster-uid", ["node-uid"], "a" * 40, retired)
        self.assertEqual(result["status"], "OWNERSHIP_CLASSIFIED")
        second = item("ingresses", "auth", managers=["kodex-install"])
        value["objects"].append(second)
        value["links"].append({"identity": second, "references": [{**secret, "relation": "CONSUMES"}]})
        retired.add(second["uid"])
        result = cleanup.plan_inventory(value, "fixture", "cluster-uid", ["node-uid"], "a" * 40, retired)
        self.assertEqual(result["status"], "OWNERSHIP_CLASSIFIED")
        for change in ("issuer", "foreign-owner", "foreign-namespace"):
            changed = copy.deepcopy(value)
            if change == "issuer":
                changed["links"][2]["references"][-1]["name"] = "foreign"
            elif change == "foreign-owner":
                changed["objects"][2]["ownerReferences"] = [{"uid": "foreign"}]
            else:
                changed["objects"][2]["namespace"] = "foreign"
            result = cleanup.plan_inventory(changed, "fixture", "cluster-uid", ["node-uid"], "a" * 40, retired)
            if change == "foreign-namespace":
                self.assertNotIn("new-cert", {entry["identity"]["uid"] for entry in result["deleteCandidates"]})
            else:
                self.assertEqual(result["status"], "OWNERSHIP_REVIEW_REQUIRED")

    def test_foreign_object_blocks_owned_namespace(self):
        value = fixture()
        value["objects"] = [item("deployments", "kodex-component", managers=["kodex-local-dev"]),
                            item("deployments", "foreign-component")]
        result = plan(value)
        self.assertEqual(result["status"], "OWNERSHIP_REVIEW_REQUIRED")
        self.assertEqual([x["identity"]["name"] for x in result["unresolved"]], ["foreign-component"])
        self.assertEqual(result["applyStatus"], "NOT_EXECUTED")

    def test_shared_cluster_capabilities_and_foreign_namespace_are_preserved(self):
        value = fixture()
        value["objects"] = [item("nodes", "node", "", labels={"app.kubernetes.io/part-of": "kodex"}, managers=["kodex-install"]),
                            item("customresourcedefinitions", "certificates.cert-manager.io", "", managers=["kodex-install"]),
                            item("deployments", "kodex-named", "foreign", managers=["kodex-local-dev"])]
        self.assertEqual(plan(value)["deleteCandidates"], [])
        self.assertEqual(plan(value)["preserveNamespaces"][0]["name"], "kube-system")

    def test_owner_uid_closure_does_not_cross_namespace(self):
        value = fixture()
        parent = item("deployments", "controller", managers=["kodex-local-dev"])
        value["objects"] = [parent, item("pods", "owned", owners=[{"uid": parent["uid"]}]),
                            item("pods", "foreign", "foreign", owners=[{"uid": parent["uid"]}]),
                            item("pods", "mixed", owners=[{"uid": parent["uid"]}, {"uid": "foreign-owner"}])]
        names = {x["identity"]["name"] for x in plan(value)["deleteCandidates"]}
        self.assertEqual(names, {"controller", "owned"})

    def test_foreign_consumer_blocks_shared_secret(self):
        value = fixture()
        secret = item("secrets", "configured", managers=["kodex-install"])
        foreign = item("pods", "foreign")
        value["objects"] = [secret, foreign]
        value["links"] = [{"identity": foreign, "references": [{"namespace": "kodex-system", "resource": "secrets", "name": "configured", "relation": "CONSUMES"}]}]
        result = plan(value)
        self.assertEqual(result["shared"][0]["identity"]["uid"], secret["uid"])
        self.assertEqual(result["status"], "OWNERSHIP_REVIEW_REQUIRED")

    def test_consumption_does_not_invent_resource_origin(self):
        value = fixture()
        owner = item("deployments", "component", managers=["kodex-local-dev"])
        foreign = item("secrets", "unknown-origin")
        value["objects"] = [owner, foreign]
        value["links"] = [{"identity": owner, "references": [{"namespace": "kodex-system", "resource": "secrets", "name": "unknown-origin", "relation": "CONSUMES"}]}]
        self.assertEqual(plan(value)["unresolved"][0]["identity"]["name"], "unknown-origin")

    def test_target_identity_and_incomplete_inventory_fail_closed(self):
        for key, changed in (("context", "other"), ("clusterUID", "other"), ("status", "INCOMPLETE"), ("nodes", [])):
            value = fixture()
            value[key] = changed
            with self.assertRaises(ValueError):
                plan(value)
        value = fixture()
        volume = item("persistentvolumeclaims", "own", managers=["kodex-install"])
        value["objects"] = [volume]
        value["volumes"] = [{"claim": {"uid": volume["uid"], "name": "foreign", "namespace": "kodex-system"}, "reclaimPolicy": "Delete"}]
        with self.assertRaises(ValueError):
            plan(value)

    def test_foreign_custom_resource_blocks_crd_cascade(self):
        value = fixture()
        crd = item("customresourcedefinitions", "settings.fixture.kodex.dev", "",
                   labels={"app.kubernetes.io/part-of": "kodex"}, managers=["kodex-install"])
        foreign = item("settings", "foreign-settings", "foreign")
        foreign["apiVersion"] = "fixture.kodex.dev/v1"
        value["objects"] = [crd, foreign]
        result = plan(value)
        self.assertEqual(result["shared"][0]["foreignConsumerUIDs"], [foreign["uid"]])
        self.assertEqual(result["status"], "OWNERSHIP_REVIEW_REQUIRED")

    def test_stateful_claim_requires_exact_existing_template_and_no_foreign_reader(self):
        value = fixture()
        controller = item("statefulsets", "database", managers=["kodex-local-dev"])
        claim = item("persistentvolumeclaims", "data-database-0", managers=["k3s"])
        value["objects"] = [controller, claim]
        value["links"] = [{"identity": controller, "references": [], "claimTemplates": ["data"]}]
        result = plan(value)
        self.assertEqual(result["unresolved"], [])
        foreign = item("pods", "foreign")
        value["objects"].append(foreign)
        value["links"].append({"identity": foreign, "references": [{"resource": "persistentvolumeclaims", "namespace": "kodex-system", "name": claim["name"], "relation": "CONSUMES"}]})
        self.assertIn(claim["uid"], {x["identity"]["uid"] for x in plan(value)["unresolved"]})

    def test_selector_policy_rejects_foreign_matching_pod_and_broad_selector(self):
        value = fixture()
        owner = item("deployments", "component", managers=["kodex-local-dev"])
        policy = item("networkpolicies", "policy", managers=["kubectl-create"])
        selector = {"matchLabels": {"app.kubernetes.io/name": "component"}}
        value["objects"] = [owner, policy]
        value["links"] = [{"identity": owner, "references": [], "controllerSelector": selector},
                          {"identity": policy, "references": [], "policySelector": selector}]
        self.assertEqual(plan(value)["unresolved"], [])
        foreign = item("pods", "foreign", labels=selector["matchLabels"])
        value["objects"].append(foreign)
        self.assertIn(policy["uid"], {x["identity"]["uid"] for x in plan(value)["unresolved"]})
        value["objects"].pop()
        value["links"][1]["policySelector"] = {}
        self.assertIn(policy["uid"], {x["identity"]["uid"] for x in plan(value)["unresolved"]})

    def test_known_private_state_still_requires_exact_owned_deployment(self):
        value = fixture()
        owner = item("deployments", "control-plane", managers=["kodex-local-dev"])
        secret = item("secrets", "kodex-ssh-compose-signing", managers=["kubectl-create"])
        value["objects"] = [owner, secret]
        value["links"] = [{"identity": owner, "references": [{"resource": "secrets", "namespace": "kodex-system", "name": secret["name"], "relation": "CONSUMES"}]}]
        self.assertEqual(plan(value)["unresolved"], [])
        owner["name"] = "unrelated"
        self.assertEqual(plan(value)["unresolved"][0]["identity"]["uid"], secret["uid"])

    def test_exact_helm_release_receipt_outside_application_namespace(self):
        value = fixture()
        own = item("secrets", "sh.helm.release.v1.kodex-trust-manager.v1", "cert-manager", labels={"owner": "helm", "name": "kodex-trust-manager"})
        foreign = copy.deepcopy(own)
        foreign.update(name="foreign", uid="foreign-uid", labels={"owner": "helm", "name": "cert-manager"})
        value["objects"] = [own, foreign]
        self.assertEqual([x["identity"]["name"] for x in plan(value)["deleteCandidates"]], [own["name"]])


if __name__ == "__main__":
    unittest.main()
