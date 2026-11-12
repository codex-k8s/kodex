"""Image plan сохраняет чужие aliases и workload refs, а неизвестный component закрыто отклоняется."""

import unittest

import installation_images as images


class ImagePlanTests(unittest.TestCase):
    def test_promoted_policy_scope_and_bare_alias_require_local_target_witness(self):
        digest = "sha256:" + "a" * 64
        witness = {"name": images.PREFIX + "control-plane:fixture", "digest": digest}
        promoted = {"name": "example.invalid/kodex/control-plane@" + digest, "digest": digest}
        bare = {"name": "sha256:" + "b" * 64, "digest": digest}
        self.assertEqual(images.candidates([witness, promoted, bare], {"control-plane"}, set())[0], [])
        selected, preserved = images.candidates([witness, promoted, bare], {"control-plane"}, set(), {"example.invalid"})
        self.assertEqual(preserved, [])
        self.assertEqual(selected[-1], witness)
        self.assertTrue(all(entry["witness"] == witness["name"] for entry in selected[:-1]))
        self.assertEqual(images.candidates([bare], {"control-plane"}, set(), {"example.invalid"})[0], [])

    def test_shared_target_and_live_reference_are_preserved(self):
        digest = "sha256:" + "a" * 64
        own = {"name": images.PREFIX + "control-plane:fixture", "digest": digest}
        foreign = {"name": "example.invalid/foreign:fixture", "digest": digest}
        selected, kept = images.candidates([own, foreign], {"control-plane"}, set())
        self.assertEqual(selected, [])
        self.assertEqual(kept, [own, foreign])
        self.assertEqual(images.candidates([own], {"control-plane"}, {own["name"]})[0], [])
        self.assertEqual(images.candidates([own], {"control-plane"}, set())[0], [own])

    def test_unknown_component_duplicate_or_malformed_target_is_rejected(self):
        own = {"name": images.PREFIX + "unknown:fixture", "digest": "sha256:" + "a" * 64}
        with self.assertRaises(ValueError):
            images.candidates([own], {"control-plane"}, set())
        own["name"] = images.PREFIX + "control-plane:fixture"
        with self.assertRaises(ValueError):
            images.candidates([own, own], {"control-plane"}, set())
        own["digest"] = "mutable"
        with self.assertRaises(ValueError):
            images.candidates([own], {"control-plane"}, set())


if __name__ == "__main__":
    unittest.main()
