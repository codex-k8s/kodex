"""Exact local CAS двух CEL validations; остальные policy/binding/pins неизменны."""

import argparse
import copy
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import time

import yaml
from trusted_cluster_render import runtime_admission_profile

ROOT = Path(__file__).resolve().parents[2]
NAME = "runtime-role-pod-exact-secret-projection"
POLICY_PATH = "deploy/k8s/base/runtime-controller/runtime-materialization-admission.yaml"
QUERY_PATH = "tools/dev/supply-chain-owner-readback.sql"
QUERY_SHA = "9c147119d49fbf24279ef52affecd9fcd3a9bdc113665bc804a7cb80c6e36ec4"
SHARED = "runtime environment volumes must have exact shared driver and executor mounts"
NEW_SHARED = "runtime environment volumes must have exact shared initializer driver and executor mounts"
AUTHORITY = "managed role Pod authority mount boundary is invalid"
ENV = {"HOME": "/home/s", "PATH": "/home/s/.local/state/kodex-dev/tools/bin:/usr/local/bin:/usr/bin:/bin", "LANG": "C.UTF-8", "KUBECONFIG": "/home/s/.kube/config"}


class Failure(Exception):
    pass


def require(ok, code):
    if not ok:
        raise Failure(code)


def fingerprint(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()).hexdigest()


def expression(value):
    require(isinstance(value, str), "EXPRESSION_INVALID")
    parts = re.split(r'''('(?:\\.|[^'\\])*'|"(?:\\.|[^"\\])*")''', value)
    result = []
    for index, part in enumerate(parts):
        if index % 2:
            result.append(part)
        else:
            require(not any(x in part for x in ("'", '"', "//", "/*", "*/")), "EXPRESSION_INVALID")
            result.append(re.sub(r"[ \t\r\n]+", " ", part))
    return "".join(result).strip()


def canonical(spec):
    result = copy.deepcopy(spec)
    for key in ("matchConstraints", "matchResources"):
        if key in result:
            match = result[key]
            match.setdefault("matchPolicy", "Equivalent")
            match.setdefault("namespaceSelector", {})
            match.setdefault("objectSelector", {})
            for rule in match.get("resourceRules", []) + match.get("excludeResourceRules", []):
                rule.setdefault("scope", "*")
    for key in ("validations", "matchConditions", "variables"):
        for item in result.get(key, []):
            item["expression"] = expression(item["expression"])
    return result


def unique_rule(spec, message):
    found = [i for i, item in enumerate(spec["validations"]) if item.get("message") == message]
    require(len(found) == 1, "SOURCE_RULE_REQUIRED")
    return found[0]


def replace_once(source, previous, following):
    require(source.count(previous) == 1, "SOURCE_RULE_CHANGED")
    return source.replace(previous, following, 1)


def initializer_spec(previous):
    result = canonical(previous)
    require(result.get("failurePolicy") == "Fail", "FAIL_CLOSED_POLICY_REQUIRED")
    shared = result["validations"][unique_rule(result, SHARED)]
    value = replace_once(shared["expression"], "['role-runtime', 'provider-runtime']", "['workspace-init', 'role-runtime', 'provider-runtime']")
    value = replace_once(value,
        "size(variables.providerContainers[0].volumeMounts.filter(mount, mount.name == volume.name)) == 1 &&",
        "size(variables.providerContainers[0].volumeMounts.filter(mount, mount.name == volume.name)) == 1 && size(object.spec.initContainers[1].volumeMounts.filter(mount, mount.name == volume.name)) == 1 &&")
    value = replace_once(value,
        "variables.providerContainers[0].volumeMounts.filter(mount, mount.name == volume.name)[0].mountPath)",
        "variables.providerContainers[0].volumeMounts.filter(mount, mount.name == volume.name)[0].mountPath && variables.roleContainers[0].volumeMounts.filter(mount, mount.name == volume.name)[0].mountPath == object.spec.initContainers[1].volumeMounts.filter(mount, mount.name == volume.name)[0].mountPath)")
    shared.update(expression=value, message=NEW_SHARED)
    authority = result["validations"][unique_rule(result, AUTHORITY)]
    authority["expression"] = replace_once(authority["expression"], "'runtime-egress-trust', 'tmp'])", "'runtime-egress-trust', 'tmp'] || mount.name.matches('^environment-[a-f0-9]{16}$'))")
    return result


def run(command, arguments, payload=None):
    try:
        value = subprocess.run([command, *arguments], input=payload, env=ENV, cwd=ROOT, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=20, check=False)
    except (OSError, subprocess.TimeoutExpired):
        raise Failure("COMMAND_UNAVAILABLE") from None
    require(value.returncode == 0 and len(value.stdout) <= 2 << 20, "COMMAND_FAILED")
    return value.stdout


def kube(*args, payload=None):
    return run("kubectl", ["--kubeconfig", ENV["KUBECONFIG"], "--context", "k3d-kodex", "--request-timeout=10s", *args], payload)


def get(kind, name, namespace=None):
    return json.loads(kube(*(["-n", namespace] if namespace else []), "get", kind, name, "-o", "json"))


def source_policy(raw):
    require(len(raw) <= 1 << 20, "SOURCE_SIZE_INVALID")
    found = [x for x in yaml.safe_load_all(raw) if x and x.get("kind") == "ValidatingAdmissionPolicy" and x.get("metadata", {}).get("name") == NAME]
    require(len(found) == 1, "SOURCE_POLICY_REQUIRED")
    policy = found[0]
    runtime_admission_profile(policy)
    return policy["spec"]


def compiled(policy):
    status = policy.get("status", {})
    generation = policy.get("metadata", {}).get("generation")
    require(type(generation) is int and generation > 0 and status.get("observedGeneration") == generation, "COMPILATION_NOT_CURRENT")
    checking = status.get("typeChecking")
    require(checking is None or isinstance(checking, dict), "COMPILATION_STATUS_INVALID")
    warnings = (checking or {}).get("expressionWarnings")
    require(warnings is None or warnings == [], "COMPILATION_WARNINGS")


def guards():
    binding = get("validatingadmissionpolicybinding", NAME)
    spec = binding["spec"]
    require(spec.get("policyName") == NAME and spec.get("validationActions") == ["Deny"] and spec.get("paramRef", {}).get("namespace") == "kodex-system" and spec["paramRef"].get("parameterNotFoundAction") == "Deny", "EXACT_BINDING_REQUIRED")
    config = get("configmap", spec["paramRef"]["name"], "kodex-system")
    parameters = get("imageadmissionpolicyparameters", config["metadata"]["name"], "kodex-system")
    require(config.get("immutable") is True and fingerprint(config["data"]) == fingerprint(parameters["spec"]), "EXACT_PARAMETERS_REQUIRED")
    controller = get("deployment", "runtime-controller", "kodex-system")
    template = controller["spec"]["template"]
    require(controller["metadata"].get("labels", {}).get("kodex.dev/local-profile") == "hot-reload" and template["metadata"].get("labels", {}).get("kodex.dev/security-profile") == "trusted-cluster", "TRUSTED_PROFILE_REQUIRED")
    query = (ROOT / QUERY_PATH).read_bytes()
    require(hashlib.sha256(query).hexdigest() == QUERY_SHA, "OWNER_QUERY_CHANGED")
    owner = json.loads(kube("-n", "kodex-system", "exec", "-i", "kodex-postgresql-0", "--", "psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", "control_plane", payload=query))
    age = (datetime.now(timezone.utc) - datetime.fromisoformat(owner["at"])).total_seconds()
    require(-5 <= age < 30 and all(owner.get(k) == 0 for k in ("openBuilds", "pendingAdmissions", "pendingPromotions", "activeRuntimeRuns", "claimedRuntimeLeases")), "FRESH_IDLE_REQUIRED")
    return {"clusterUID": get("namespace", "kube-system")["metadata"]["uid"], "namespaceUID": get("namespace", "kodex-system")["metadata"]["uid"],
        "bindingUID": binding["metadata"]["uid"], "bindingSHA256": fingerprint(spec), "parameterUID": parameters["metadata"]["uid"], "parametersSHA256": fingerprint(parameters["spec"]),
        "configUID": config["metadata"]["uid"], "configSHA256": fingerprint(config["data"]), "controllerUID": controller["metadata"]["uid"], "controllerSpecSHA256": fingerprint(controller["spec"]), "promotedArtifactCount": owner["promotedArtifactCount"], "promotedPinsSHA256": owner["promotedPinsSHA256"]}


def private_read(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        info = os.fstat(fd)
        require(stat.S_ISREG(info.st_mode) and info.st_uid == os.getuid() and info.st_mode & 0o777 == 0o600 and info.st_nlink == 1 and 0 < info.st_size < 2 << 20, "PRIVATE_INPUT_REQUIRED")
        return json.load(os.fdopen(fd, "r", closefd=False))
    finally:
        os.close(fd)


def private_write(path, value):
    target = Path(path)
    parent = target.parent
    info = parent.stat()
    require(parent.resolve() == parent and stat.S_ISDIR(info.st_mode) and info.st_uid == os.getuid() and info.st_mode & 0o777 == 0o700, "PRIVATE_DIRECTORY_REQUIRED")
    fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    try:
        os.write(fd, (json.dumps(value, sort_keys=True) + "\n").encode())
        os.fsync(fd)
    finally:
        os.close(fd)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("command", choices=("plan", "apply", "inspect"))
    parser.add_argument("--source-revision", required=True)
    parser.add_argument("--previous-revision", required=True)
    parser.add_argument("--plan", required=True)
    parser.add_argument("--evidence")
    parser.add_argument("--confirm")
    args = parser.parse_args()
    require(all(re.fullmatch(r"[a-f0-9]{40}", x) for x in (args.source_revision, args.previous_revision)), "EXACT_REVISION_REQUIRED")
    require(run("git", ["rev-parse", "HEAD"]).decode().strip() == args.source_revision and run("git", ["status", "--porcelain"]).strip() == b"", "CLEAN_SOURCE_REQUIRED")
    run("git", ["merge-base", "--is-ancestor", args.previous_revision, args.source_revision])
    before = source_policy(run("git", ["show", args.previous_revision + ":" + POLICY_PATH]))
    after = source_policy((ROOT / POLICY_PATH).read_bytes())
    require(fingerprint(initializer_spec(before)) == fingerprint(canonical(after)), "CLOSED_SOURCE_DELTA_REQUIRED")
    current = get("validatingadmissionpolicy", NAME)
    current_guards = guards()
    if args.command == "plan":
        require(not args.confirm and not args.evidence and fingerprint(canonical(current["spec"])) == fingerprint(canonical(before)), "EXACT_PREDECESSOR_REQUIRED")
        compiled(current)
        private_write(args.plan, {"version": 1, "kind": "LOCAL_INITIALIZER_MOUNTS", "sourceRevision": args.source_revision, "previousRevision": args.previous_revision, "policyUID": current["metadata"]["uid"], "resourceVersion": current["metadata"]["resourceVersion"], "beforeSHA256": fingerprint(canonical(before)), "afterSHA256": fingerprint(canonical(after)), "guards": current_guards})
        return {"status": "PLANNED", "sourceRevision": args.source_revision, "guards": current_guards}
    plan = private_read(args.plan)
    require(set(plan) == {"version", "kind", "sourceRevision", "previousRevision", "policyUID", "resourceVersion", "beforeSHA256", "afterSHA256", "guards"} and plan["version"] == 1 and plan["kind"] == "LOCAL_INITIALIZER_MOUNTS", "CLOSED_PLAN_REQUIRED")
    require(plan["sourceRevision"] == args.source_revision and plan["previousRevision"] == args.previous_revision and plan["policyUID"] == current["metadata"]["uid"] and plan["guards"] == current_guards and plan["afterSHA256"] == fingerprint(canonical(after)), "PLAN_PINS_CHANGED")
    if args.command == "apply":
        require(args.confirm == "APPLY-LOCAL-INITIALIZER-MOUNTS" and args.evidence and current["metadata"]["resourceVersion"] == plan["resourceVersion"] and fingerprint(canonical(current["spec"])) == plan["beforeSHA256"], "EXACT_APPLY_REQUIRED")
        patch = [{"op": "test", "path": "/metadata/uid", "value": plan["policyUID"]}, {"op": "test", "path": "/metadata/resourceVersion", "value": plan["resourceVersion"]}]
        for index in (unique_rule(before, SHARED), unique_rule(before, AUTHORITY)):
            patch.extend([{"op": "test", "path": "/spec/validations/" + str(index), "value": current["spec"]["validations"][index]}, {"op": "replace", "path": "/spec/validations/" + str(index), "value": after["validations"][index]}])
        private_write(args.evidence, {"status": "INTENT", "planSHA256": fingerprint(plan), "sourceRevision": args.source_revision})
        kube("patch", "validatingadmissionpolicy", NAME, "--type=json", "--patch", json.dumps(patch))
    else:
        require(not args.confirm and not args.evidence, "INSPECT_ARGUMENTS_INVALID")
    deadline = time.monotonic() + 30
    while True:
        current = get("validatingadmissionpolicy", NAME)
        require(current["metadata"]["uid"] == plan["policyUID"] and fingerprint(canonical(current["spec"])) == plan["afterSHA256"], "POLICY_READBACK_MISMATCH")
        try:
            compiled(current)
            break
        except Failure as error:
            require(str(error) == "COMPILATION_NOT_CURRENT" and time.monotonic() < deadline, "COMPILATION_READBACK_FAILED")
            time.sleep(1)
    require(guards() == plan["guards"], "PRESERVATION_READBACK_FAILED")
    result = {"status": "PASS", "mode": args.command, "sourceRevision": args.source_revision, "policyUID": plan["policyUID"], "policySHA256": plan["afterSHA256"], "generation": current["metadata"]["generation"], "observedGeneration": current["status"]["observedGeneration"], "guards": plan["guards"]}
    if args.command == "apply":
        private_write(args.evidence + ".readback.json", result)
    return result


if __name__ == "__main__":
    try:
        print(json.dumps(main(), sort_keys=True))
    except (Failure, OSError, ValueError, KeyError, TypeError, yaml.YAMLError) as error:
        code = str(error) if isinstance(error, Failure) and re.fullmatch(r"[A-Z_]{1,80}", str(error)) else "TRANSITION_FAILED"
        print(json.dumps({"status": "FAIL_OR_UNKNOWN", "code": code, "effect": "READBACK_REQUIRED"}))
        sys.exit(1)
