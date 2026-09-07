#!/usr/bin/env python3
"""Проверка singleton rollout без обращения к кластеру."""
import json
from pathlib import Path
import subprocess

root = Path(__file__).resolve().parents[2]
rule = root / "tools/dev/worker-grant-rollout.jq"


def render(value):
    return subprocess.run(["jq", "-f", str(rule)], input=json.dumps(value),
                          text=True, capture_output=True, check=False)


names = ["control-plane", "secret-broker", "email-bridge", "runtime-controller",
         "integration-gateway", "interaction-gateway", "automation-scheduler",
         "session-archive", "role-image-builder"]
for location in ("containers", "initContainers"):
    for name in names:
        item = {"kind": "Deployment", "metadata": {"name": name}, "spec": {
            "replicas": 3, "strategy": {"type": "RollingUpdate", "rollingUpdate": {
                "maxSurge": 1}}, "template": {"spec": {location: [{
                    "name": "control-plane-platform-worker-grant-agent"}]}}}}
        result = render([item])
        assert result.returncode == 0, result.stderr
        actual = json.loads(result.stdout)[0]
        assert actual["spec"]["replicas"] == 1
        assert actual["spec"]["strategy"] == {"type": "Recreate"}
        assert actual["spec"]["template"] == item["spec"]["template"]
        item["metadata"]["name"] = "unknown-worker"
        assert render([item]).returncode != 0
        item["spec"]["template"]["spec"][location] = [{"name": "renamed",
            "command": ["/workspace/tools/dev/run-go-hot-reload.sh"],
            "args": ["./cmd/internal-rpc-authority-platform-worker-grant-agent"]}]
        assert render([item]).returncode != 0
        item["metadata"]["name"] = name
        assert render([item]).returncode == 0
unrelated = {"kind": "Deployment", "metadata": {"name": "unrelated"},
             "spec": {"replicas": 3, "strategy": {"type": "RollingUpdate"}}}
assert json.loads(render([unrelated]).stdout) == [unrelated]
print("Worker grant rollout contract passed")
