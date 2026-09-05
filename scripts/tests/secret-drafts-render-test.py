#!/usr/bin/env python3
"""Проверяет итоговые profiles без обращения к Kubernetes API."""
import json
from pathlib import Path
import subprocess
import yaml

ROOT = Path(__file__).resolve().parents[2]
for profile in ("web-only", "web-with-mattermost"):
    output = subprocess.run(["kubectl", "kustomize", str(ROOT / "deploy/k8s/profiles" / profile)], capture_output=True, check=True, timeout=60)
    objects = [item for item in yaml.safe_load_all(output.stdout) if item]

    def get(kind, name, namespace=None):
        found = [item for item in objects if item["kind"] == kind and item["metadata"]["name"] == name and (namespace is None or item["metadata"].get("namespace") == namespace)]
        assert len(found) == 1, "render object missing or duplicated"
        return found[0]

    get("Namespace", "kodex-secret-drafts")
    role = get("Role", "secret-broker-encrypted-drafts", "kodex-secret-drafts")
    assert role["rules"] == [
        {"apiGroups": [""], "resources": ["secrets"], "verbs": ["get", "create", "delete"]},
        {"apiGroups": [""], "resources": ["configmaps"], "resourceNames": ["secret-broker-draft-key-guard"], "verbs": ["get", "update"]},
    ]
    binding = get("RoleBinding", "secret-broker-encrypted-drafts", "kodex-secret-drafts")
    assert binding["subjects"] == [{"kind": "ServiceAccount", "name": "secret-broker", "namespace": "kodex-system"}]
    assert binding["roleRef"]["name"] == role["metadata"]["name"]
    policy = get("NetworkPolicy", "encrypted-drafts-deny-all", "kodex-secret-drafts")["spec"]
    assert policy["podSelector"] == {} and policy["ingress"] == [] and policy["egress"] == []
    assert not any(item["kind"] == "ConfigMap" and item["metadata"]["name"] == "secret-broker-draft-key-guard" for item in objects), "ordinary apply would reset durable guard"
    pod = get("Deployment", "secret-broker", "kodex-system")["spec"]["template"]["spec"]
    containers = pod["containers"] + pod.get("initContainers", [])
    broker = next(item for item in containers if item["name"] == "secret-broker")
    environment = {item["name"]: item.get("value") for item in broker["env"]}
    assert environment["SECRET_BROKER_DRAFT_NAMESPACE"] == "kodex-secret-drafts"
    assert environment["SECRET_BROKER_RUNTIME_NAMESPACE"] == "kodex-runtime"
    assert environment["SECRET_BROKER_DRAFT_KEY_GUARD_NAME"] == "secret-broker-draft-key-guard"
    volume = next(item for item in pod["volumes"] if item["name"] == "draft-keyring")
    assert volume["secret"] == {"secretName": "secret-broker-draft-keyring", "defaultMode": 0o440, "items": [{"key": "keyring.json", "path": "keyring.json"}]}
    mounts = [item for item in broker["volumeMounts"] if item["name"] == "draft-keyring"]
    assert mounts == [{"name": "draft-keyring", "mountPath": "/var/run/secrets/kodex/secret-broker/draft-keyring", "readOnly": True}]
    assert all(not any(mount["name"] == "draft-keyring" for mount in item.get("volumeMounts", [])) for item in containers if item["name"] != "secret-broker"), "keyring mounted into unrelated sidecar"
    cp = get("Deployment", "control-plane", "kodex-system")["spec"]["template"]["spec"]
    owner = next(item for item in cp["containers"] if item["name"] == "control-plane")
    assert any(item["name"] == "CONTROL_PLANE_RUNTIME_SECRET_STAGING_NAMESPACE" and item.get("value") == "kodex-secret-drafts" for item in owner["env"])
    assert not any(item["kind"] in ("Pod", "Deployment", "StatefulSet", "Job") and item["metadata"].get("namespace") == "kodex-secret-drafts" for item in objects)

registry = json.loads((ROOT / "tools/install/secret-projections.json").read_text())
entry = next(item for item in registry["secrets"] if item["name"] == "secret-broker-draft-keyring")
assert entry["dynamic"] is False and entry["lifecycle_owner"] == "secret-broker-draft-bootstrap"
assert entry["items"] == [{"key": "keyring.json", "source": {"type": "material", "ref": "kodex/secret-broker-draft-keyring", "field": "keyring.json"}}]
print("Both secret draft profile renders preserve namespace, key mount and durable guard boundaries")
