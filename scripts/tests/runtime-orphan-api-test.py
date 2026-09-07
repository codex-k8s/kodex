#!/usr/bin/env python3
"""Disposable API без agent/workloads, Secret fixture не относится к live данным."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time

root = Path(__file__).resolve().parents[2]
image = os.environ["KODEX_ORPHAN_TEST_IMAGE"]
assert image.startswith("sha256:") and len(image) == 71
name = f"kodex-orphan-api-{os.getpid()}"
volumes = []


def run(args, timeout=30, cwd=None):
    return subprocess.run(args, capture_output=True, text=True, timeout=timeout, cwd=cwd)


with tempfile.TemporaryDirectory() as tmp:
    binary = Path(tmp) / "orphan.test"
    result = run(["go", "test", "-c", "-o", str(binary), "./internal/maintenance/runtimeorphan"],
                 timeout=120, cwd=root / "services/internal/control-plane")
    assert result.returncode == 0, "fixture compilation failed"
    binary.chmod(0o555)
    try:
        assert run(["docker", "run", "-d", "--pull=never", "--network=none", "--name", name,
                    "--tmpfs", "/run", "--mount", f"type=bind,src={binary},dst=/orphan.test,readonly",
                    "--entrypoint", "/bin/k3s", image, "server", "--disable-agent",
                    "--disable=traefik,servicelb,metrics-server,coredns,local-storage",
                    "--disable-helm-controller", "--disable-network-policy", "--flannel-backend=none",
                    "--node-ip=127.0.0.1", "--bind-address=127.0.0.1", "--advertise-address=127.0.0.1"]).returncode == 0
        volumes = [v["Name"] for v in json.loads(run(["docker", "inspect", name]).stdout)[0]["Mounts"] if v["Type"] == "volume"]
        deadline = time.monotonic() + 60
        while run(["docker", "exec", name, "kubectl", "get", "--raw=/readyz"]).returncode:
            assert time.monotonic() < deadline, "API readiness deadline"
            time.sleep(1)
        result = run(["docker", "exec", "-e", "KODEX_ORPHAN_API_FIXTURE=1", "-e", "TZ=UTC", name,
                      "/orphan.test", "-test.run=^TestOrphanActualAPI$", "-test.v", "-test.timeout=100s"], timeout=110)
        print(result.stdout, end="")
        assert result.returncode == 0, "actual orphan API regression failed"
    finally:
        assert run(["docker", "rm", "-fv", name]).returncode == 0
        assert all(run(["docker", "volume", "inspect", v]).returncode != 0 for v in volumes)
