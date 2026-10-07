"""Регрессии адресной очистки host images без подключения к Docker."""

import copy
import contextlib
import importlib.util
import io
from pathlib import Path
import sys
import unittest
from unittest.mock import patch
from types import SimpleNamespace


SPEC = importlib.util.spec_from_file_location("local_host_image_cache", Path(__file__).with_name("local-host-image-cache.py"))
CACHE = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = CACHE
SPEC.loader.exec_module(CACHE)
OLD = "sha256:" + "a" * 64
CURRENT = "sha256:" + "b" * 64


def image(ref=OLD):
    return {"id": ref, "tags": ["kodex-local/image-admission-tools:" + "1" * 64],
            "digests": ["kodex-local/image-admission-tools@" + ref], "bytes": 100}


class Pins:
    calls = 0
    changed_after = None
    path = Path("/nonexistent")

    def pins(self):
        self.calls += 1
        identity = "changed" if self.changed_after and self.calls >= self.changed_after else "initial"
        return {CURRENT}, set(), identity


class Docker:
    def __init__(self):
        self.images = {OLD: image(), CURRENT: image(CURRENT)}
        self.used = set()
        self.removed = []
        self.image_calls = 0
        self.retag = False
        self.usage_calls = 0
        self.race_use = False

    def image_ids(self):
        return list(self.images)

    def used_images(self):
        self.usage_calls += 1
        return self.used | ({OLD} if self.race_use and self.usage_calls >= 2 else set())

    def image(self, ref):
        self.image_calls += 1
        result = copy.deepcopy(self.images[ref])
        if self.retag and self.image_calls >= 2:
            result["tags"].append("kodex-local/image-admission-tools:" + "2" * 64)
        return result

    def remove(self, ref):
        self.removed.append(ref)

    def runtime_images(self):
        return set()


class Tests(unittest.TestCase):
    def setUp(self):
        self.docker = Docker()
        self.pins = Pins()
        self.images = CACHE.Images(self.docker, self.pins)
        self.images.pending_build = lambda: None

    def test_audit_never_removes(self):
        result = self.images.audit()
        self.assertEqual([item["state"] for item in result["images"]], ["OBSOLETE", "KEEP"])
        self.assertEqual(result["obsoleteLogicalBytes"], 100)
        self.assertEqual(self.docker.removed, [])

    def test_removes_only_explicit_old_id(self):
        self.assertEqual(self.images.prune([OLD])["removed"], [OLD])
        self.assertEqual(self.docker.removed, [OLD])

    def test_current_and_restore_pin_is_protected(self):
        with self.assertRaisesRegex(CACHE.Failure, "IMAGE_PROTECTED"):
            self.images.prune([CURRENT])
        self.assertEqual(self.docker.removed, [])

    def test_stopped_container_still_protects_image(self):
        self.docker.used.add(OLD)
        with self.assertRaisesRegex(CACHE.Failure, "IMAGE_IN_USE"):
            self.images.prune([OLD])

    def test_late_container_aborts(self):
        self.docker.race_use = True
        with self.assertRaisesRegex(CACHE.Failure, "IMAGE_IN_USE"):
            self.images.prune([OLD])
        self.assertEqual(self.docker.removed, [])

    def test_pin_changed_before_remove_aborts(self):
        for call in (2, 3):
            with self.subTest(call=call):
                self.pins = Pins()
                self.pins.changed_after = call
                with self.assertRaisesRegex(CACHE.Failure, "PINS_CHANGED"):
                    self.images.pins = CACHE.Images(self.docker, self.pins).pins
                    self.images.prune([OLD])
        self.assertEqual(self.docker.removed, [])

    def test_retag_aborts(self):
        self.docker.retag = True
        with self.assertRaisesRegex(CACHE.Failure, "IMAGE_CHANGED"):
            self.images.prune([OLD])

    def test_invalid_or_duplicate_targets_aborts(self):
        for targets in ([], ["latest"], [OLD, OLD], ["--force"], ["sha256:" + "a" * 63]):
            with self.subTest(targets=targets):
                with self.assertRaises(CACHE.Failure):
                    self.images.prune(targets)
        self.assertEqual(self.docker.removed, [])

    def test_repository_alias_digest_protects_config_id(self):
        self.docker.images[OLD]["digests"] = ["kodex-local/image-admission-tools@" + CURRENT]
        with self.assertRaisesRegex(CACHE.Failure, "IMAGE_PROTECTED"):
            self.images.prune([OLD])

    def test_cri_or_live_pod_manifest_is_protected(self):
        self.docker.runtime_images = lambda: {OLD}
        with self.assertRaisesRegex(CACHE.Failure, "IMAGE_PROTECTED"):
            self.images.prune([OLD])

    def test_pending_build_aborts(self):
        def pending():
            raise CACHE.Failure("BUILD_PENDING")
        self.images.pending_build = pending
        with self.assertRaisesRegex(CACHE.Failure, "BUILD_PENDING"):
            self.images.prune([OLD])
        self.assertEqual(self.docker.removed, [])

    def test_inspect_rejects_foreign_tag_and_unbounded_size(self):
        docker = CACHE.Docker()
        for key, value in (("tags", ["radar/image:latest"]), ("tags", ["kodex-local/image-admission-tools:latest"]),
                           ("digests", ["foreign/image@" + OLD]), ("bytes", True), ("bytes", 17 << 30)):
            data = image()
            data[key] = value
            docker.run = lambda _arguments, data=data: CACHE.json.dumps(data)
            with self.subTest(key=key, value=value):
                with self.assertRaisesRegex(CACHE.Failure, "IMAGE_OUTSIDE_SCOPE"):
                    docker.image(OLD)

    def test_remove_has_no_force_or_volume_flags(self):
        docker = CACHE.Docker()
        calls = []
        docker.run = lambda args: calls.append(args)
        docker.remove(OLD)
        self.assertEqual(calls, [["image", "rm", "--no-prune", OLD]])

    def test_dependent_child_conflict_never_retries_with_force(self):
        with patch.object(CACHE.subprocess, "run", return_value=SimpleNamespace(
                returncode=1, stdout=b"", stderr=b"dependent child images PRIVATE_SENTINEL")) as run:
            with self.assertRaisesRegex(CACHE.Failure, "^DOCKER_REMOVE_CONFLICT$"):
                CACHE.Docker().remove(OLD)
        self.assertEqual(run.call_count, 1)
        self.assertEqual(run.call_args.args[0][-4:], ["image", "rm", "--no-prune", OLD])

    def test_cli_error_has_only_closed_code(self):
        for stderr, code in ((b"conflict: must be forced PRIVATE_SENTINEL", "DOCKER_REMOVE_CONFLICT"),
                             (b"image is referenced in multiple repositories PRIVATE_SENTINEL", "DOCKER_REMOVE_CONFLICT"),
                             (b"image is being used by PRIVATE_SENTINEL", "DOCKER_IMAGE_IN_USE"),
                             (b"No such image PRIVATE_SENTINEL", "DOCKER_IMAGE_NOT_FOUND"),
                             (b"PRIVATE_SENTINEL", "DOCKER_COMMAND_FAILED")):
            with self.subTest(code=code), patch.object(CACHE.subprocess, "run", return_value=SimpleNamespace(
                    returncode=1, stdout=b"", stderr=stderr)):
                with self.assertRaisesRegex(CACHE.Failure, "^" + code + "$"):
                    CACHE.Docker().remove(OLD)

    def test_timeout_distinct_from_rejected_delete(self):
        with patch.object(CACHE.subprocess, "run", side_effect=CACHE.subprocess.TimeoutExpired("PRIVATE_SENTINEL", 30)):
            with self.assertRaisesRegex(CACHE.Failure, "^DOCKER_TIMEOUT$"):
                CACHE.Docker().remove(OLD)

    def test_main_preserves_closed_reason_and_partial_receipt(self):
        self.docker.remove = lambda _ref: (_ for _ in ()).throw(CACHE.Failure("DOCKER_REMOVE_CONFLICT"))
        output = io.StringIO()
        with patch.object(CACHE, "Images", return_value=self.images), contextlib.redirect_stdout(output):
            self.assertEqual(CACHE.main(["prune", "--target", OLD]), 1)
        self.assertEqual(CACHE.json.loads(output.getvalue()),
                         {"state": "FAILED", "code": "DOCKER_REMOVE_CONFLICT", "removed": []})

    def test_main_never_prints_unknown_exception_text(self):
        self.docker.remove = lambda _ref: (_ for _ in ()).throw(CACHE.Failure("PRIVATE_SENTINEL"))
        output = io.StringIO()
        with patch.object(CACHE, "Images", return_value=self.images), contextlib.redirect_stdout(output):
            self.assertEqual(CACHE.main(["prune", "--target", OLD]), 1)
        self.assertNotIn("PRIVATE_SENTINEL", output.getvalue())
        self.assertEqual(CACHE.json.loads(output.getvalue())["code"], "CLEANUP_ABORTED")


if __name__ == "__main__":
    unittest.main()
