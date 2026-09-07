#!/usr/bin/env python3
"""Настоящий disposable API: default/owned strategy, SSA и guarded migration."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time

root = Path(__file__).resolve().parents[2]
image = os.environ["KODEX_STRATEGY_TEST_IMAGE"]
assert image.startswith("sha256:") and len(image) == 71
name = f"kodex-strategy-upgrade-{os.getpid()}"
volumes = []


def run(args, value=None, env=None):
    return subprocess.run(args, input=value, text=True, capture_output=True,
                          timeout=25, env=env)


def kube(args, value=None):
    return run(["docker", "exec", "-i", name, "kubectl"] + args,
               json.dumps(value) if value is not None else None)


def manifest(workload, location):
    spec = {"containers": [{"name": "main", "image": "fixture.invalid/not-run"}]}
    spec.setdefault(location, []).append({"name": "platform-worker-grant-agent",
                                         "image": "fixture.invalid/not-run"})
    return {"apiVersion": "apps/v1", "kind": "Deployment", "metadata": {
        "name": workload, "namespace": "kodex-system"}, "spec": {
        "replicas": 1, "selector": {"matchLabels": {"app": workload}},
        "template": {"metadata": {"labels": {"app": workload,
            "kodex.dev/environment": "staging", "kodex.dev/local-profile": "hot-reload",
            "kodex.dev/profile": "web-only"}}, "spec": spec}}}


try:
    assert run(["docker", "image", "inspect", image]).returncode == 0
    assert run(["docker", "run", "-d", "--pull=never", "--network=none", "--name", name,
                "--tmpfs", "/run", "--entrypoint", "/bin/k3s", image, "server",
                "--disable-agent", "--disable=traefik,servicelb,metrics-server,coredns,local-storage",
                "--disable-helm-controller", "--disable-network-policy", "--flannel-backend=none",
                "--node-ip=127.0.0.1", "--bind-address=127.0.0.1", "--advertise-address=127.0.0.1"]).returncode == 0
    volumes = [mount["Name"] for mount in json.loads(run(["docker", "inspect", name]).stdout)[0]["Mounts"]
               if mount["Type"] == "volume"]
    deadline = time.monotonic() + 60
    while kube(["get", "--raw=/readyz"]).returncode:
        assert time.monotonic() < deadline, "API startup deadline"
        time.sleep(1)
    assert kube(["create", "namespace", "kodex-system"]).returncode == 0
    with tempfile.TemporaryDirectory() as tmp:
        directory = Path(tmp)
        wrapper = directory / "kubectl"
        wrapper.write_text("#!/usr/bin/env python3\nimport os,sys,subprocess,json\n"
                           "a=sys.argv[1:]\n"
                           "if os.environ.get('STRATEGY_RACE') and 'patch' in a:\n"
                           " subprocess.run(['docker','exec',os.environ['STRATEGY_CONTAINER'],'kubectl','-n','kodex-system','scale','deployment/'+a[a.index('deployment')+1],'--replicas=2'],stdout=subprocess.DEVNULL,check=True)\n"
                           "if '--patch-file' in a:\n"
                           " i=a.index('--patch-file'); p=json.load(open(a[i+1]))\n"
                           " if os.environ.get('STRATEGY_UID'): p[0]['value']='wrong-uid'\n"
                           " a[i:i+2]=['--patch',json.dumps(p)]\n"
                           "os.execvp('docker',['docker','exec','-i',os.environ['STRATEGY_CONTAINER'],'kubectl']+a)\n")
        wrapper.chmod(0o700)
        env = dict(os.environ, PATH=str(directory) + ":" + os.environ["PATH"], STRATEGY_CONTAINER=name)
        for workload, location in [("email-bridge", "containers"), ("secret-broker", "initContainers")]:
            original = manifest(workload, location)
            if workload == "secret-broker":
                original["spec"]["strategy"] = {"type": "RollingUpdate", "rollingUpdate": {"maxSurge": 2}}
            assert kube(["create", "--field-manager=legacy", "-f", "-"], original).returncode == 0
            desired = json.loads(json.dumps(original))
            desired["spec"]["strategy"] = {"type": "Recreate"}
            apply = ["apply", "--server-side", "--force-conflicts", "--field-manager=kodex-local-dev", "-f", "-"]
            rejected = kube(apply, desired)
            assert rejected.returncode and "may not be specified" in rejected.stderr
            nullable = json.loads(json.dumps(desired))
            nullable["spec"]["strategy"]["rollingUpdate"] = None
            assert kube(apply, nullable).returncode != 0
            file = directory / "desired.json"
            file.write_text(json.dumps(desired))
            command = ["bash", str(root / "tools/dev/migrate-worker-grant-strategy.sh"), str(file)]
            for key, value in [("kodex.dev/local-profile", "foreign"), ("kodex.dev/environment", "production")]:
                denied = json.loads(json.dumps(desired))
                denied["spec"]["template"]["metadata"]["labels"][key] = value
                file.write_text(json.dumps(denied))
                assert run(command, env=env).returncode != 0
            denied = json.loads(json.dumps(desired))
            denied["spec"]["selector"]["matchLabels"]["app"] = "foreign"
            file.write_text(json.dumps(denied))
            assert run(command, env=env).returncode != 0
            file.write_text(json.dumps(desired))
            foreign = {"spec": {"template": {"metadata": {"labels": {
                "kodex.dev/local-profile": "foreign"}}}}}
            assert kube(["-n", "kodex-system", "patch", "deployment", workload,
                         "--type=merge", "--patch", json.dumps(foreign)]).returncode == 0
            assert run(command, env=env).returncode != 0
            foreign["spec"]["template"]["metadata"]["labels"]["kodex.dev/local-profile"] = "hot-reload"
            assert kube(["-n", "kodex-system", "patch", "deployment", workload,
                         "--type=merge", "--patch", json.dumps(foreign)]).returncode == 0
            assert run(command, env=dict(env, STRATEGY_UID="1")).returncode != 0
            race = run(command, env=dict(env, STRATEGY_RACE="1"))
            assert race.returncode != 0 and "atomic patch failed" in race.stderr
            before = json.loads(kube(["-n", "kodex-system", "get", "deployment", workload, "-o", "json"]).stdout)
            assert before["spec"]["strategy"]["type"] == "RollingUpdate"
            migrated = run(command, env=env)
            assert migrated.returncode == 0, migrated.stderr
            assert kube(apply, desired).returncode == 0
            after = json.loads(kube(["-n", "kodex-system", "get", "deployment", workload, "-o", "json"]).stdout)
            assert after["metadata"]["uid"] == before["metadata"]["uid"]
            assert after["spec"]["strategy"] == {"type": "Recreate"}
            assert after["spec"]["replicas"] == 1
            assert after["spec"]["template"] == before["spec"]["template"]
            assert run(command, env=env).returncode == 0
        fresh = manifest("control-plane", "containers")
        fresh["spec"]["strategy"] = {"type": "Recreate"}
        file.write_text(json.dumps(fresh))
        assert run(command, env=env).returncode == 0
        assert kube(apply, fresh).returncode == 0
        fresh["metadata"]["name"] = "unregistered-worker"
        file.write_text(json.dumps(fresh))
        assert run(command, env=env).returncode != 0
    assert json.loads(kube(["get", "nodes", "-o", "json"]).stdout)["items"] == []
    print("Worker strategy API upgrade passed: default/owned, omitted/null rejection, CAS race, replay, fresh, unknown")
finally:
    assert run(["docker", "rm", "-fv", name]).returncode == 0
    assert all(run(["docker", "volume", "inspect", volume]).returncode != 0 for volume in volumes)
