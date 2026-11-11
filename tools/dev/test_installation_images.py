"""Image plan сохраняет чужие aliases и workload refs, а неизвестный component закрыто отклоняется."""

import unittest

import installation_images as images


class ImagePlanTests(unittest.TestCase):
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
