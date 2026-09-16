"""Материализация и закрытая проверка hostPath-контракта trusted-cluster."""

import argparse
import copy
import json
import os
from pathlib import Path
import re
import sys


PROFILE = "trusted-cluster"
PROFILE_LABEL = "kodex.dev/security-profile"
MASK_NAME = "kodex-local-private-source-mask"
PRIVATE_DIRECTORIES = (".git", ".agents", ".kodex-dev")


def require(condition, code):
    if not condition:
        raise ValueError(code)


def pod_specs(resources):
    for resource in resources:
        if resource.get("kind") in ("Deployment", "StatefulSet", "Job"):
            yield resource, resource["spec"]["template"]


def canonical_root(raw):
    path = Path(raw)
    require(path.is_absolute() and str(path) == raw and str(path.resolve()) == raw,
            "HOST_PATH_NOT_CANONICAL")
    require(path != Path("/") and len(path.parts) >= 4, "HOST_PATH_TOO_BROAD")
    return path


def parameters(source, cache, uid, gid):
    source, cache = canonical_root(source), canonical_root(cache)
    require(source != cache and source not in cache.parents and cache not in source.parents,
            "SOURCE_CACHE_BOUNDARY_INVALID")
    require(type(uid) is int and type(gid) is int and 0 < uid < 2**31 and 0 < gid < 2**31,
            "HOST_IDENTITY_INVALID")
    return source, cache


def allowed_host_path(raw, source, cache):
    path = Path(raw)
    if str(path) != raw or not path.is_absolute() or str(path.resolve()) != raw:
        return False
    if path in (source, source / "services/staff/control-center",
                source / "tools/dev/run-frontend.sh",
                source / "tools/dev/frontend-cache-identity.sh"):
        return True
    try:
        relative = path.relative_to(cache).as_posix()
    except ValueError:
        return False
    return relative in ("go-mod-v2", "go-sumdb", "go-tools") or bool(
        re.fullmatch(r"go-build-v2/[a-z0-9][a-z0-9-]{0,125}", relative) or
        re.fullmatch(r"frontend-v1/[a-f0-9]{64}/node_modules", relative))


def private_files(source):
    # Содержимое не читается. Все варианты env скрываются, включая ignored-файлы.
    return sorted({".env"} | {path.name for path in source.glob(".env*") if not path.is_dir()})


def verify_mask_targets(source):
    for name in PRIVATE_DIRECTORIES:
        target = source / name
        require(not target.is_symlink() and target.is_dir(), "PRIVATE_DIRECTORY_MOUNTPOINT_REQUIRED")
    for name in private_files(source):
        target = source / name
        require(not target.is_symlink() and target.is_file(), "PRIVATE_FILE_MOUNTPOINT_REQUIRED")


def prepare_mask_targets(source):
    source = canonical_root(source)
    # Git и env уже принадлежат существующему клону; не создаём их и не читаем.
    require((source / ".git").is_dir() and not (source / ".git").is_symlink(),
            "EXISTING_CHECKOUT_REQUIRED")
    for name in private_files(source):
        target = source / name
        require(not target.is_symlink() and target.is_file(), "PRIVATE_FILE_MOUNTPOINT_REQUIRED")
    for name in PRIVATE_DIRECTORIES:
        target = source / name
        require(not target.is_symlink(), "PRIVATE_DIRECTORY_SYMLINK_FORBIDDEN")
        target.mkdir(mode=0o700, exist_ok=True)
    verify_mask_targets(source)


def materialize(resources, source, cache, uid, gid):
    source, cache = parameters(source, cache, uid, gid)
    verify_mask_targets(source)
    resources = copy.deepcopy(resources)
    masked_namespaces = set()
    for resource, template in pod_specs(resources):
        spec = template["spec"]
        require(template.get("metadata", {}).get("labels", {}).get(PROFILE_LABEL) == PROFILE,
                "TRUSTED_PROFILE_LABEL_REQUIRED")
        mounted_source = False
        for container in spec.get("containers", []) + spec.get("initContainers", []):
            mounts = container.get("volumeMounts", [])
            if not any(mount["mountPath"].startswith("/workspace") for mount in mounts):
                continue
            require("authority" not in container["name"] and "grant-agent" not in container["name"],
                    "AUTHORITY_MUST_BE_REMOVED_BEFORE_HOST_IDENTITY")
            mounted_source = True
            container["securityContext"] = {
                "runAsUser": uid, "runAsGroup": gid, "runAsNonRoot": True,
                "allowPrivilegeEscalation": False, "readOnlyRootFilesystem": True,
                "capabilities": {"drop": ["ALL"]},
            }
            if not any(mount["mountPath"] == "/tmp" for mount in mounts):
                mounts.append({"name": "kodex-dev-tmp", "mountPath": "/tmp"})
                volumes = spec.setdefault("volumes", [])
                if not any(volume["name"] == "kodex-dev-tmp" for volume in volumes):
                    volumes.append({"name": "kodex-dev-tmp", "emptyDir": {"sizeLimit": "4Gi"}})
            if any(mount["mountPath"] == "/workspace" for mount in mounts):
                namespace = resource["metadata"].get("namespace", "default")
                masked_namespaces.add(namespace)
                mounts[:] = [mount for mount in mounts if mount["name"] != MASK_NAME]
                mounts.extend({"name": MASK_NAME, "mountPath": "/workspace/" + name,
                               "subPath": "empty", "readOnly": True} for name in private_files(source))
                mounts.extend({"name": MASK_NAME, "mountPath": "/workspace/" + name,
                               "readOnly": True} for name in PRIVATE_DIRECTORIES)
                volumes = spec.setdefault("volumes", [])
                if not any(volume["name"] == MASK_NAME for volume in volumes):
                    volumes.append({"name": MASK_NAME, "configMap": {"name": MASK_NAME, "defaultMode": 292}})
        if mounted_source:
            spec.setdefault("securityContext", {}).update({
                "runAsUser": uid, "runAsGroup": gid, "runAsNonRoot": True,
                "fsGroup": gid, "fsGroupChangePolicy": "OnRootMismatch",
                "seccompProfile": {"type": "RuntimeDefault"},
            })
            template.setdefault("metadata", {}).setdefault("annotations", {}).update({
                "kodex.dev/host-uid": str(uid), "kodex.dev/host-gid": str(gid),
                "kodex.dev/source-root": str(source), "kodex.dev/cache-root": str(cache),
            })
    for namespace in sorted(masked_namespaces):
        if not any(resource.get("kind") == "ConfigMap" and resource["metadata"].get("name") == MASK_NAME and
                   resource["metadata"].get("namespace") == namespace for resource in resources):
            resources.append({"apiVersion": "v1", "kind": "ConfigMap", "metadata": {
                "name": MASK_NAME, "namespace": namespace,
                "labels": {"app.kubernetes.io/part-of": "kodex", PROFILE_LABEL: PROFILE}},
                "immutable": True, "data": {"empty": ""}})
    verify(resources, str(source), str(cache), uid, gid)
    return resources


def verify(resources, source, cache, uid, gid):
    source, cache = parameters(source, cache, uid, gid)
    verify_mask_targets(source)
    for resource, template in pod_specs(resources):
        spec = template["spec"]
        volumes = {volume["name"]: volume for volume in spec.get("volumes", [])}
        for volume in volumes.values():
            if "hostPath" in volume:
                require(allowed_host_path(volume["hostPath"]["path"], source, cache), "HOST_PATH_NOT_ALLOWED")
        for container in spec.get("containers", []) + spec.get("initContainers", []):
            mounts = container.get("volumeMounts", [])
            host_mounts = [mount for mount in mounts if "hostPath" in volumes.get(mount["name"], {})]
            if not host_mounts:
                continue
            require(template.get("metadata", {}).get("labels", {}).get(PROFILE_LABEL) == PROFILE,
                    "TRUSTED_PROFILE_LABEL_REQUIRED")
            security = {**spec.get("securityContext", {}), **container.get("securityContext", {})}
            require(security.get("runAsUser") == uid and security.get("runAsGroup") == gid and
                    security.get("runAsNonRoot") is True and security.get("allowPrivilegeEscalation") is False and
                    security.get("readOnlyRootFilesystem") is True and
                    security.get("capabilities") == {"drop": ["ALL"]} and not security.get("privileged", False),
                    "HOST_PATH_CONTAINER_IDENTITY_INVALID")
            for mount in host_mounts:
                raw = volumes[mount["name"]]["hostPath"]["path"]
                if Path(raw) == source or source in Path(raw).parents or mount["mountPath"].startswith("/workspace"):
                    require(mount.get("readOnly") is True, "WRITABLE_WORKSPACE_FORBIDDEN")
            if any(mount["mountPath"] == "/workspace" for mount in mounts):
                mask = volumes.get(MASK_NAME, {}).get("configMap", {})
                require(mask.get("name") == MASK_NAME, "PRIVATE_SOURCE_MASK_REQUIRED")
                for name in [*private_files(source), *PRIVATE_DIRECTORIES]:
                    require(any(mount["mountPath"] == "/workspace/" + name and
                                mount["name"] == MASK_NAME and mount.get("readOnly") is True for mount in mounts),
                            "PRIVATE_SOURCE_MASK_REQUIRED")
                namespace = resource["metadata"].get("namespace", "default")
                require(any(item.get("kind") == "ConfigMap" and item["metadata"].get("name") == MASK_NAME and
                            item["metadata"].get("namespace") == namespace and item.get("data") == {"empty": ""}
                            for item in resources), "PRIVATE_SOURCE_MASK_CONTENT_INVALID")


def main():
    parser = argparse.ArgumentParser(description="Validate trusted-cluster hostPath and identity contract")
    parser.add_argument("mode", choices=("prepare-source-mask", "materialize", "verify"))
    parser.add_argument("--source-root", required=True)
    parser.add_argument("--cache-root", required=True)
    parser.add_argument("--host-uid", required=True, type=int)
    parser.add_argument("--host-gid", required=True, type=int)
    args = parser.parse_args()
    if args.mode == "prepare-source-mask":
        parameters(args.source_root, args.cache_root, args.host_uid, args.host_gid)
        prepare_mask_targets(args.source_root)
        return
    resources = json.load(sys.stdin)
    require(isinstance(resources, list), "RENDER_DOCUMENT_ARRAY_REQUIRED")
    if args.mode == "materialize":
        json.dump(materialize(resources, args.source_root, args.cache_root, args.host_uid, args.host_gid), sys.stdout)
    else:
        verify(resources, args.source_root, args.cache_root, args.host_uid, args.host_gid)


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, TypeError, OSError):
        print("Trusted cluster hostPath contract rejected", file=sys.stderr)
        sys.exit(1)
