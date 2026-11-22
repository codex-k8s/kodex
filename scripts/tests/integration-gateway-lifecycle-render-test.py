#!/usr/bin/env python3
"""Проверка join budget и закрытого capture spool в фактических локальных renders."""
import json
import os
from pathlib import Path
import subprocess
import yaml


def main():
    """Сверить оба публичных профиля без API-запросов и вывода содержимого ConfigMap."""
    root = Path(__file__).resolve().parents[2]
    for profile in ("web-only", "web-with-mattermost"):
        environment = dict(os.environ, KUBECONFIG="/dev/null")
        raw = subprocess.check_output(
            ["kubectl", "--kubeconfig=/dev/null", "--context=render-only", "kustomize", str(root / "deploy/k8s/profiles" / profile)],
            env=environment, timeout=45,
        )
        objects = list(yaml.safe_load_all(raw))
        deployment = next(o for o in objects if o.get("kind") == "Deployment" and o["metadata"]["name"] == "integration-gateway")
        config = next(o for o in objects if o.get("kind") == "ConfigMap" and o["metadata"]["name"] == "integration-gateway-runtime")
        assert config["data"]["INTEGRATION_GATEWAY_SHUTDOWN_TIMEOUT"] == "20s", "canonical join budget changed"
        grace = deployment["spec"]["template"]["spec"]["terminationGracePeriodSeconds"]
        assert grace == 60 and grace >= 2 * 20 + 10 + 10, "Pod grace does not cover joined cleanup"
        control = next(o for o in objects if o.get("kind") == "Deployment" and o["metadata"]["name"] == "control-plane")
        pod = control["spec"]["template"]["spec"]
        main = next(c for c in pod["containers"] if c["name"] == "control-plane")
        spool_name = "kubernetes-capture-spool"
        spool_path = "/work/kubernetes-capture-spool"
        assert next(v for v in pod["volumes"] if v["name"] == spool_name) == {
            "name": spool_name, "emptyDir": {"sizeLimit": "512Mi"},
        }, "capture spool must use bounded node storage without host paths or memory medium"
        assert next(e for e in main["env"] if e["name"] == "CONTROL_PLANE_KUBERNETES_CAPTURE_SPOOL_ROOT") == {
            "name": "CONTROL_PLANE_KUBERNETES_CAPTURE_SPOOL_ROOT", "value": spool_path,
        }, "capture owner must receive the exact private mount"
        assert next(m for m in main["volumeMounts"] if m["name"] == spool_name) == {
            "name": spool_name, "mountPath": spool_path,
        }, "capture spool must remain writable only through its fixed mount"
        assert main["securityContext"]["runAsUser"] == 10001 and main["securityContext"]["runAsNonRoot"] is True
        assert main["securityContext"]["readOnlyRootFilesystem"] is True and main["securityContext"]["runAsGroup"] == 10001
        assert pod["securityContext"]["fsGroup"] == 29000, "shared authority volume group must remain unchanged"
        for container in pod.get("initContainers", []) + pod["containers"]:
            if container["name"] != "control-plane":
                assert not any(m["name"] == spool_name for m in container.get("volumeMounts", [])), "capture spool is shared with another container"
        print(json.dumps({"profile": profile, "join_seconds": 20, "grace_seconds": grace, "result": "PASS"}))


if __name__ == "__main__":
    main()
