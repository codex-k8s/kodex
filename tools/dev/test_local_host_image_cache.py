"""Регрессии адресной очистки host images без подключения к Docker."""

import copy
import importlib.util
from pathlib import Path
import sys
import unittest


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
        self.assertEqual(calls, [["image", "rm", OLD]])


if __name__ == "__main__":
    unittest.main()
