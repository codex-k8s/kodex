#!/usr/bin/env python3
"""Проверка закрытой JSON Schema результата; доступ и происхождение проверяет CP."""

import copy
from decimal import Decimal
import json
from pathlib import Path
import unittest

from jsonschema import Draft202012Validator


ROOT = Path(__file__).resolve().parents[2]
SCHEMA = json.loads(
    (ROOT / "contracts/runtime/workflow-result/v1/schema.json").read_text()
)


class WorkflowResultContractTest(unittest.TestCase):
    """Различимые источники и неизменяемые pins без caller-assigned полномочий."""

    @classmethod
    def setUpClass(cls):
        Draft202012Validator.check_schema(SCHEMA)
        cls.validator = Draft202012Validator(SCHEMA)

    def validate_result(self, result, expected):
        payload = {"schema": "kodex.workflow-result.v1", "results": [result]}
        self.assertEqual(self.validator.is_valid(payload), expected)

    def test_reachable_sources_and_external_evidence(self):
        for result in (
            {"key": "analysis", "source": {"kind": "CURRENT_COMPLETION"}},
            {
                "key": "bundle",
                "source": {
                    "kind": "CAPTURED_ARTIFACT",
                    "artifactRef": "art_12345678",
                    "revision": 3,
                    "digest": "sha256:" + "a" * 64,
                },
            },
            {"key": "pr", "receiptRef": "erc_12345678"},
            {"key": "review", "headSha": "a" * 40, "verdict": "PASS"},
            {"key": "qa", "headSha": "b" * 64, "verdict": "CHANGES_REQUIRED"},
        ):
            with self.subTest(key=result["key"]):
                self.validate_result(result, True)

    def test_current_selector_cannot_assign_pins_or_clean(self):
        for field, value in (
            ("artifactRef", "art_12345678"),
            ("revision", 1),
            ("digest", "sha256:" + "a" * 64),
            ("scanState", "CLEAN"),
            ("nodeRef", "node_12345678"),
            ("fileName", "analysis.md"),
        ):
            with self.subTest(field=field):
                self.validate_result(
                    {"key": "analysis", "source": {"kind": "CURRENT_COMPLETION", field: value}},
                    False,
                )

    def test_captured_requires_every_exact_pin(self):
        source = {
            "kind": "CAPTURED_ARTIFACT",
            "artifactRef": "art_12345678",
            "revision": 3,
            "digest": "sha256:" + "a" * 64,
        }
        for key in source:
            for mode in ("absent", "null"):
                with self.subTest(key=key, mode=mode):
                    invalid = copy.deepcopy(source)
                    if mode == "absent":
                        del invalid[key]
                    else:
                        invalid[key] = None
                    self.validate_result({"key": "bundle", "source": invalid}, False)
        for key, value in (
            ("revision", 0),
            ("revision", -1),
            ("revision", 1.5),
            ("revision", 9007199254740992),
            ("revision", "3"),
            ("revision", True),
            ("artifactRef", "run_12345678"),
            ("artifactRef", "https://example.test/artifact"),
            ("digest", "a" * 64),
            ("digest", "sha256:" + "A" * 64),
            ("digest", "sha256:abcd"),
        ):
            with self.subTest(key=key, value=value):
                self.validate_result({"key": "bundle", "source": {**source, key: value}}, False)

    def test_result_forms_are_exclusive(self):
        for result in (
            {"key": "analysis"},
            {"key": "analysis", "source": None},
            {"key": "analysis", "source": {"kind": "LATEST"}},
            {"key": "analysis", "source": {"Kind": "CURRENT_COMPLETION"}},
            {"key": "analysis", "source": {"kind": "CURRENT_COMPLETION"}, "receiptRef": ""},
            {"key": "pr", "receiptRef": "erc_12345678", "source": {"kind": "CURRENT_COMPLETION"}},
            {"key": "pr", "receiptRef": "erc_12345678", "headSha": "a" * 40},
            {"key": "review", "headSha": "main", "verdict": "PASS"},
            {"key": "review", "headSha": "a" * 40, "verdict": "APPROVED"},
            {"key": "review", "headSha": "a" * 40, "verdict": "PASS", "accepted": True},
        ):
            with self.subTest(result=result):
                self.validate_result(result, False)

    def test_closed_envelope_and_result_count(self):
        result = {"key": "analysis", "source": {"kind": "CURRENT_COMPLETION"}}
        for payload in (
            {"schema": "kodex.workflow-result.v1", "results": []},
            {"schema": "kodex.workflow-result.v1", "results": [result] * 5},
            {"Schema": "kodex.workflow-result.v1", "results": [result]},
            {"schema": "kodex.workflow-result.v1", "results": [result], "accepted": True},
            {"schema": "kodex.workflow-result.v2", "results": [result]},
        ):
            with self.subTest(payload=payload):
                self.assertFalse(self.validator.is_valid(payload))

    def test_shared_wire_corpus_with_exact_numbers(self):
        """Не округляем JSON float до проверки: 9007199254740990.5 остаётся дробью."""
        def exact_number(raw):
            value = Decimal(raw)
            if value.adjusted() <= 16 and value == value.to_integral_value():
                return int(value)
            return value

        cases = json.loads(
            (ROOT / "contracts/runtime/workflow-result/v1/declaration-cases.json").read_text()
        )
        for case in cases:
            with self.subTest(name=case["name"]):
                payload = json.loads(case["raw"], parse_float=exact_number)
                self.assertEqual(self.validator.is_valid(payload), case["valid"])


class WorkflowCaptureProgressContractTest(unittest.TestCase):
    """Отдельная закрытая фаза не принимает бизнес-результат и не назначает authority."""

    def test_shared_strict_wire_corpus(self):
        schema = json.loads(
            (ROOT / "contracts/runtime/workflow-result/v1/capture-progress.schema.json").read_text()
        )
        Draft202012Validator.check_schema(schema)
        validator = Draft202012Validator(schema)
        cases = json.loads(
            (ROOT / "contracts/runtime/workflow-result/v1/capture-progress-cases.json").read_text()
        )

        def unique_object(pairs):
            """Как Go decoder, отклоняем повторные JSON keys до проверки schema."""
            result = {}
            for key, value in pairs:
                if key in result:
                    raise ValueError("duplicate object member")
                result[key] = value
            return result

        for case in cases:
            with self.subTest(name=case["name"]):
                try:
                    payload = json.loads(case["raw"], object_pairs_hook=unique_object)
                except ValueError:
                    valid = False
                else:
                    valid = validator.is_valid(payload)
                    if valid:
                        self.assertFalse(Draft202012Validator(SCHEMA).is_valid(payload))
                self.assertEqual(valid, case["valid"])


if __name__ == "__main__":
    unittest.main()
