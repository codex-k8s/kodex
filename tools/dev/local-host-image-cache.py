#!/usr/bin/env python3
"""Адресная очистка устаревших host Docker images только своего toolchain."""

import argparse
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile


SCRIPT = Path(__file__).with_name("local-oci-cache.py")
SPEC = importlib.util.spec_from_file_location("local_oci_cache", SCRIPT)
OCI = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = OCI
SPEC.loader.exec_module(OCI)
DIGEST = re.compile(r"sha256:[a-f0-9]{64}")
TAG = re.compile(r"kodex-local/image-admission-tools:[a-f0-9]{64}")
REPOSITORY = "kodex-local/image-admission-tools"
LIMIT = 128


class Failure(Exception):
    pass


def require(value, code):
    if not value:
        raise Failure(code)


class Docker:
    def run(self, arguments):
        # Читаются только выбранные public поля; config/labels/env не запрашиваются.
        try:
            result = subprocess.run(
                ["docker", "--host", "unix:///var/run/docker.sock", *arguments],
                env={"PATH": "/usr/local/bin:/usr/bin:/bin", "HOME": "/nonexistent",
                     "DOCKER_CONFIG": "/nonexistent"}, capture_output=True,
                timeout=30, check=False,
            )
        except (OSError, subprocess.TimeoutExpired):
            raise Failure("DOCKER_UNAVAILABLE") from None
        require(result.returncode == 0, "DOCKER_COMMAND_FAILED")
        require(len(result.stdout) <= 2 << 20, "DOCKER_OUTPUT_EXCEEDED")
        return result.stdout.decode("utf-8", "strict")

    def image_ids(self):
        values = self.run(["image", "ls", "--quiet", "--no-trunc", "--filter",
                           "reference=" + REPOSITORY + ":*"]).splitlines()
        require(len(values) <= LIMIT and all(DIGEST.fullmatch(value) for value in values),
                "IMAGE_LIST_INVALID")
        return sorted(set(values))

    def used_images(self):
        ids = self.run(["container", "ls", "--all", "--quiet", "--no-trunc"]).splitlines()
        require(len(ids) <= 256 and all(re.fullmatch(r"[a-f0-9]{64}", value) for value in ids),
                "CONTAINER_LIST_INVALID")
        if not ids:
            return set()
        values = self.run(["container", "inspect", "--format", "{{.Image}}", *ids]).splitlines()
        require(len(values) == len(ids) and all(DIGEST.fullmatch(value) for value in values),
                "CONTAINER_IMAGES_INVALID")
        return set(values)

    def image(self, image_id):
        require(DIGEST.fullmatch(image_id), "TARGET_INVALID")
        raw = self.run(["image", "inspect", "--format",
                        '{"id":{{json .Id}},"tags":{{json .RepoTags}},'
                        '"digests":{{json .RepoDigests}},"bytes":{{.Size}}}', image_id])
        try:
            value = OCI.decode(raw)
        except OCI.Failure:
            raise Failure("IMAGE_INVALID") from None
        require(isinstance(value, dict) and set(value) == {"id", "tags", "digests", "bytes"} and
                value["id"] == image_id and isinstance(value["tags"], list) and
                0 < len(value["tags"]) <= 16 and all(isinstance(tag, str) and TAG.fullmatch(tag)
                                                  for tag in value["tags"]) and
                isinstance(value["digests"], list) and 0 < len(value["digests"]) <= 16 and
                all(isinstance(ref, str) and ref.startswith(REPOSITORY + "@") and
                    DIGEST.fullmatch(ref.partition("@")[2]) for ref in value["digests"]) and
                type(value["bytes"]) is int and 0 < value["bytes"] <= 16 << 30,
                "IMAGE_OUTSIDE_SCOPE")
        value["tags"] = sorted(value["tags"])
        value["digests"] = sorted(value["digests"])
        return value

    def remove(self, image_id):
        # Без force: появившийся между readback и удалением контейнер сохранит image.
        self.run(["image", "rm", image_id])

    def runtime_images(self):
        """Защищаем live Pod refs и CRI pinned manifests обоих exact local nodes."""
        references = set()
        for node in ("k3d-kodex-agent-0", "k3d-kodex-server-0"):
            try:
                data = OCI.decode(self.run(["exec", node, "crictl", "images", "-o", "json"]))
                images = data["images"]
            except (KeyError, TypeError, OCI.Failure):
                raise Failure("CRI_IMAGES_INVALID") from None
            require(isinstance(images, list) and len(images) <= 2048, "CRI_IMAGES_INVALID")
            for image in images:
                require(isinstance(image, dict) and isinstance(image.get("pinned"), bool) and
                        isinstance(image.get("repoDigests"), list), "CRI_IMAGES_INVALID")
                if image["pinned"]:
                    for ref in image["repoDigests"]:
                        digest = ref.partition("@")[2] if isinstance(ref, str) else ""
                        require(DIGEST.fullmatch(digest), "CRI_IMAGES_INVALID")
                        references.add(digest)
        # kubectl получает собственный одноразовый discovery cache, не cwd/.kube.
        with tempfile.TemporaryDirectory(prefix="image-cleanup-kube-", dir=OCI.STATE) as cache:
            try:
                result = subprocess.run(
                    ["kubectl", "--kubeconfig", "/home/s/.kube/config", "--context", "k3d-kodex",
                     "--cache-dir", cache, "get", "pods", "--all-namespaces", "--field-selector",
                     "status.phase!=Succeeded,status.phase!=Failed", "-o",
                     "jsonpath={range .items[*]}{range .spec.initContainers[*]}{.image}{'\\n'}{end}"
                     "{range .spec.containers[*]}{.image}{'\\n'}{end}{range .status.containerStatuses[*]}"
                     "{.imageID}{'\\n'}{end}{end}"],
                    env={"PATH": "/usr/local/bin:/usr/bin:/bin", "HOME": "/nonexistent"},
                    capture_output=True, timeout=30, check=False,
                )
            except (OSError, subprocess.TimeoutExpired):
                raise Failure("POD_READBACK_FAILED") from None
            require(result.returncode == 0 and len(result.stdout) <= 1 << 20, "POD_READBACK_FAILED")
            for ref in result.stdout.decode("utf-8", "strict").splitlines():
                if "@" in ref:
                    digest = ref.partition("@")[2]
                    require(DIGEST.fullmatch(digest), "POD_IMAGE_INVALID")
                    references.add(digest)
        return references


class Images:
    def __init__(self, docker=None, cache=None):
        self.docker = docker or Docker()
        self.cache = cache or OCI.Cache()
        self.removed = []

    def pins(self):
        try:
            return self.cache.pins()
        except (OCI.Failure, OSError):
            raise Failure("PINS_UNAVAILABLE") from None

    def pending_build(self):
        path = self.cache.path / "image-supply-chain"
        # Не удаляем или меняем build lock; даже неизвестный pending output закрывает prune.
        require(not any(name.endswith(".next") for name in os.listdir(path)), "BUILD_PENDING")

    @staticmethod
    def protected(value, pins):
        return value["id"] in pins or any(ref.partition("@")[2] in pins
                                         for ref in value["digests"])

    def audit(self):
        protected, _, _ = self.pins()
        protected |= self.docker.runtime_images()
        used = self.docker.used_images()
        entries = []
        for image_id in self.docker.image_ids():
            try:
                image = self.docker.image(image_id)
                state = "KEEP" if self.protected(image, protected) or image_id in used else "OBSOLETE"
                entries.append({**image, "state": state})
            except Failure as error:
                entries.append({"id": image_id, "state": "UNSAFE", "code": str(error)})
        return {"mode": "AUDIT", "images": entries,
                # Docker layers бывают общими: это логический размер, НЕ обещание freed bytes.
                "obsoleteLogicalBytes": sum(item.get("bytes", 0) for item in entries
                                            if item["state"] == "OBSOLETE")}

    def prune(self, targets):
        self.removed = []
        require(0 < len(targets) <= 32 and len(set(targets)) == len(targets) and
                all(DIGEST.fullmatch(value) for value in targets), "TARGETS_INVALID")
        pins, _, identity = self.pins()
        self.pending_build()
        pins |= self.docker.runtime_images()
        expected = []
        used = self.docker.used_images()
        for image_id in targets:
            image = self.docker.image(image_id)
            require(not self.protected(image, pins), "IMAGE_PROTECTED")
            require(image_id not in used, "IMAGE_IN_USE")
            expected.append(image)
        for image in expected:
            fresh_pins, _, fresh_identity = self.pins()
            require(fresh_identity == identity, "PINS_CHANGED")
            self.pending_build()
            fresh_pins |= self.docker.runtime_images()
            require(not self.protected(image, fresh_pins), "IMAGE_PROTECTED")
            require(self.docker.image(image["id"]) == image, "IMAGE_CHANGED")
            require(image["id"] not in self.docker.used_images(), "IMAGE_IN_USE")
            final_pins, _, final_identity = self.pins()
            final_pins |= self.docker.runtime_images()
            require(final_identity == identity and not self.protected(image, final_pins), "PINS_CHANGED")
            self.docker.remove(image["id"])
            self.removed.append(image["id"])
        return {"mode": "PRUNE", "removed": self.removed}


def main(argv=None):
    parser = argparse.ArgumentParser()
    parser.add_argument("mode", choices=("audit", "prune"), nargs="?", default="audit")
    parser.add_argument("--target", action="append", default=[])
    args = parser.parse_args(argv)
    images = Images()
    try:
        require(args.mode == "prune" or not args.target, "AUDIT_TARGET_FORBIDDEN")
        print(json.dumps(images.audit() if args.mode == "audit" else images.prune(args.target), sort_keys=True))
        return 0
    except (Failure, UnicodeError):
        print(json.dumps({"state": "FAILED", "code": "CLEANUP_ABORTED", "removed": images.removed}))
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
