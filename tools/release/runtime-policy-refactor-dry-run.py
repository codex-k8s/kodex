"""Проверяет единственный CEL переход без постоянных изменений кластера."""

import argparse
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tarfile
import tempfile

import yaml


ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "tools/dev"))
from trusted_cluster_render import runtime_admission_profile  # noqa: E402

_spec = importlib.util.spec_from_file_location("runtime_policy_plan", Path(__file__).with_name("runtime-policy-refactor-plan.py"))
plan = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(plan)
_private_spec = importlib.util.spec_from_file_location("runtime_policy_private", Path(__file__).with_name("runtime-policy-refactor-private.py"))
private = importlib.util.module_from_spec(_private_spec)
_private_spec.loader.exec_module(private)
write_private = private.create_private
CONTEXT = "k3d-kodex"
SOURCE_PATH = "deploy/k8s/base/runtime-controller/runtime-materialization-admission.yaml"
ENV = {"HOME": os.path.expanduser("~"), "PATH": "/usr/local/bin:/usr/bin:/bin", "LANG": "C.UTF-8"}


def run(arguments, data=None, cwd=ROOT):
    """Не отражает вывод subprocess: он может включать данные policy."""
    try:
        result = subprocess.run(arguments, cwd=cwd, env=ENV, input=data, stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE, timeout=90, check=False)
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise plan.model.PolicyTransitionError("READ_ONLY_COMMAND_FAILED") from exc
    plan.model.require(result.returncode == 0 and len(result.stdout) <= 32 * 1024 * 1024, "READ_ONLY_COMMAND_FAILED")
    return result.stdout


def git(*args):
    return run(["git", *args]).decode().strip()


def source_policy(data):
    resources = [value for value in yaml.safe_load_all(data) if value and isinstance(value, dict) and
                 value.get("kind") == "ValidatingAdmissionPolicy" and value.get("metadata", {}).get("name") == plan.model.POLICY]
    plan.model.require(len(resources) == 1, "SOURCE_POLICY_INVALID")
    runtime_admission_profile(resources[0])
    return resources[0]["spec"]


def inputs():
    """Формирует render обоих pinned commits, исключая локальные env и untracked."""
    executor = git("rev-parse", "HEAD")
    plan.model.require(re.fullmatch(r"[0-9a-f]{40}", executor) and not git("status", "--porcelain", "--untracked-files=normal"),
                       "CLEAN_EXECUTOR_REQUIRED")
    run(["git", "merge-base", "--is-ancestor", plan.TARGET_REVISION, executor])
    sources = []
    with tempfile.TemporaryDirectory(prefix="kodex-policy-dry-run-") as directory:
        root = Path(directory)
        for number, revision in enumerate((plan.BASE_REVISION, plan.TARGET_REVISION)):
            raw = run(["git", "show", revision + ":" + SOURCE_PATH])
            archive_path = root / f"source-{number}.tar"
            # Файлы берутся только из Git object database, без .git и рабочих env.
            with archive_path.open("wb") as archive:
                try:
                    result = subprocess.run(["git", "archive", "--format=tar", revision], cwd=ROOT, env=ENV,
                                            stdout=archive, stderr=subprocess.PIPE, timeout=90, check=False)
                except (OSError, subprocess.TimeoutExpired) as exc:
                    raise plan.model.PolicyTransitionError("SOURCE_ARCHIVE_FAILED") from exc
            plan.model.require(result.returncode == 0 and archive_path.stat().st_size <= 2 * 1024 ** 3, "SOURCE_ARCHIVE_FAILED")
            destination = root / str(number)
            destination.mkdir(mode=0o700)
            with tarfile.open(archive_path, "r:") as archive:
                archive.extractall(destination, filter="data")
            rendered = run(["kubectl", "kustomize", str(destination / "deploy/k8s/profiles/web-only")])
            sources.append((source_policy(rendered), source_policy(raw)))
    return executor, sources[0], sources[1]


def get(*args):
    return json.loads(run(["kubectl", "--context", CONTEXT, "--request-timeout=30s", *args, "-o", "json"]))


def snapshot():
    """Читает только policy, binding, публичные параметры и namespace metadata."""
    cluster = get("get", "namespace", "kube-system")
    namespace = get("get", "namespace", "kodex-system")
    policy = get("get", "validatingadmissionpolicy", plan.model.POLICY)
    binding = get("get", "validatingadmissionpolicybinding", plan.model.POLICY)
    plan.binding_state(binding)
    name = binding["spec"]["paramRef"]["name"]
    plan.model.require(re.fullmatch(r"[a-z0-9]([-a-z0-9.]*[a-z0-9])?", name) and len(name) <= 253, "PARAMETER_NAME_INVALID")
    config = get("-n", "kodex-system", "get", "configmap", name)
    parameters = get("-n", "kodex-system", "get", "imageadmissionpolicyparameters", name)
    plan.parameters_state(binding, config, parameters)
    cluster_uid, namespace_uid = cluster.get("metadata", {}).get("uid"), namespace.get("metadata", {}).get("uid")
    plan.model.require(all(isinstance(uid, str) and re.fullmatch(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}", uid)
                           for uid in (cluster_uid, namespace_uid)), "NAMESPACE_IDENTITY_INVALID")
    return {"clusterUID": cluster_uid, "namespaceUID": namespace_uid, "policy": policy,
            "binding": binding, "config": config, "parameters": parameters}


def prepare(before, candidate, after, patch, executor):
    """Проверяет ответ API и выдаёт только безопасные commitments."""
    plan.model.require(isinstance(executor, str) and re.fullmatch(r"[0-9a-f]{40}", executor) and executor != "0" * 40,
                       "EXECUTOR_IDENTITY_INVALID")
    plan.model.require(before["clusterUID"] == after["clusterUID"] and before["namespaceUID"] == after["namespaceUID"],
                       "NAMESPACE_IDENTITY_DRIFT")
    result = plan.confirm_dry_run(before["policy"], before["binding"], before["config"], before["parameters"],
                                  candidate, after["policy"], after["binding"], after["config"], after["parameters"], patch)
    result.update({"executorSHA": executor, "context": CONTEXT, "clusterUID": before["clusterUID"],
                   "namespaceUID": before["namespaceUID"], "patchSHA256": plan.model.fingerprint(patch),
                   "schema": "kodex.runtime-policy-refactor-dry-run/v1"})
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    executor, old, new = inputs()
    before = snapshot()
    patch = plan.make_patch(*old, *new, before["policy"], before["binding"], before["config"], before["parameters"])
    candidate = json.loads(run(["kubectl", "--context", CONTEXT, "--request-timeout=30s", "patch",
                                "validatingadmissionpolicy", plan.model.POLICY, "--type=json", "--patch-file=/dev/stdin",
                                "--dry-run=server", "-o", "json"], json.dumps(patch).encode()))
    after = snapshot()
    result = prepare(before, candidate, after, patch, executor)
    write_private(args.output, result)
    print(json.dumps({"status": result["status"], "ownerReadiness": result["ownerReadiness"], "apply": result["apply"]}))


if __name__ == "__main__":
    try:
        main()
    except (plan.model.PolicyTransitionError, private.PrivateArtifactError, OSError, ValueError, KeyError, TypeError, tarfile.TarError, yaml.YAMLError) as error:
        code = str(error) if isinstance(error, (plan.model.PolicyTransitionError, private.PrivateArtifactError)) and re.fullmatch(r"[A-Z_]+", str(error)) else "DRY_RUN_FAILED"
        print(json.dumps({"status": "FAIL", "code": code}))
        sys.exit(1)
