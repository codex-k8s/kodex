#!/usr/bin/env python3
"""Проверить purpose namespace и минимальные права по итоговым web renders без Kubernetes API."""
from pathlib import Path
import subprocess
import yaml

ROOT = Path(__file__).resolve().parents[2]
NAMESPACE = "kodex-ssh-identities"
for profile in ("web-only", "web-with-mattermost"):
    output = subprocess.run(["kubectl", "kustomize", str(ROOT / "deploy/k8s/profiles" / profile)],
                            capture_output=True, check=True, timeout=60)
    objects = [item for item in yaml.safe_load_all(output.stdout) if item]

    def get(kind, name, namespace=None):
        """Выбрать единственный exact object; дубликат либо отсутствие не дают частичного PASS."""
        found = [item for item in objects if item["kind"] == kind and item["metadata"]["name"] == name
                 and item["metadata"].get("namespace") == namespace]
        assert len(found) == 1, "SSH purpose object missing or duplicated"
        return found[0]

    namespace = get("Namespace", NAMESPACE)
    assert namespace["metadata"]["labels"]["kodex.dev/credential-purpose"] == "ssh-target"
    assert namespace["metadata"]["labels"]["app.kubernetes.io/part-of"] == "kodex"
    assert namespace["metadata"]["labels"]["pod-security.kubernetes.io/enforce"] == "restricted"
    role = get("Role", "secret-broker-ssh-identities", NAMESPACE)
    assert role["rules"] == [{"apiGroups": [""], "resources": ["secrets"], "verbs": ["get", "create", "delete"]}]
    binding = get("RoleBinding", role["metadata"]["name"], NAMESPACE)
    assert binding["subjects"] == [{"kind": "ServiceAccount", "name": "secret-broker", "namespace": "kodex-system"}]
    assert binding["roleRef"] == {"apiGroup": "rbac.authorization.k8s.io", "kind": "Role", "name": role["metadata"]["name"]}
    read_role = get("ClusterRole", "secret-broker-ssh-namespace-readiness")
    assert read_role["rules"] == [{"apiGroups": [""], "resources": ["namespaces"], "resourceNames": [NAMESPACE], "verbs": ["get"]}]
    read_binding = get("ClusterRoleBinding", read_role["metadata"]["name"])
    assert read_binding["subjects"] == binding["subjects"]
    assert read_binding["roleRef"] == {"apiGroup": "rbac.authorization.k8s.io", "kind": "ClusterRole", "name": read_role["metadata"]["name"]}
    policy = get("NetworkPolicy", "ssh-identities-deny-all", NAMESPACE)["spec"]
    assert policy == {"podSelector": {}, "policyTypes": ["Ingress", "Egress"], "ingress": [], "egress": []}
    assert all(item["kind"] in {"Namespace", "Role", "RoleBinding", "NetworkPolicy"}
               for item in objects if item["metadata"].get("namespace") == NAMESPACE)
    # Обычный render не создаёт identity, key placeholder или отдельный workload в private namespace.
    broker = get("Deployment", "secret-broker", "kodex-system")["spec"]["template"]["spec"]
    assert not any(source.get("secret", {}).get("secretName", "").startswith("sshbi-") for source in broker["volumes"])

print("Both SSH purpose namespace renders preserve minimal broker RBAC and deny-all policy")
