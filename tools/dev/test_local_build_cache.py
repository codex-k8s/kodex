"""Герметичные проверки: Docker/cleanup не запускаются."""

import contextlib
import importlib.util
import io
import json
import sys
from pathlib import Path
import unittest
from unittest.mock import patch


SPEC = importlib.util.spec_from_file_location("local_build_cache", Path(__file__).with_name("local-build-cache.py"))
CACHE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(CACHE)
TARGET = "a" * 24
OTHER = "b" * 24
CANARY = "PRIVATE_DESCRIPTION_CANARY"


def record(identifier=TARGET, **changes):
    value = {"ID": identifier, "Reclaimable": True, "Shared": False, "Mutable": False,
             "Size": "2.292GB", "CreatedAt": "4 days ago", "LastUsedAt": "3 days ago",
             "Description": CANARY}
    value.update(changes)
    return value


def wire(*values):
    return b"\n".join(json.dumps(value).encode() for value in values)


class BuildCacheTests(unittest.TestCase):
    def fingerprint(self, value=None):
        value = value or record()
        return CACHE.records("default", wire(value))[value["ID"]]["fingerprint"]

    def run_prune(self, responses, targets=(TARGET,), fingerprints=None):
        if fingerprints is None:
            fingerprints = [self.fingerprint(record(target)) for target in targets]
        with patch.object(CACHE, "docker", side_effect=responses) as mocked, \
                patch.object(CACHE, "free_bytes", side_effect=[100, 120]):
            result = CACHE.execute("prune", "default", targets, fingerprints)
        return result, mocked

    def test_audit_is_readonly_and_redacts_description(self):
        with patch.object(CACHE, "docker", return_value=wire(record(), record(OTHER, Shared=True))) as mocked:
            result = CACHE.execute("audit", "default")
        self.assertEqual(result["excludedCount"], 1)
        self.assertEqual(len(result["records"]), 1)
        self.assertNotIn(CANARY, json.dumps(result))
        self.assertEqual(mocked.call_args.args, ("default", "du"))

    def test_age_boundary_and_builder_fingerprint(self):
        for value, expected in (("2 days ago", True), ("25 hours ago", True),
                                ("24 hours ago", False), ("1 day ago", False),
                                ("23 hours ago", False), ("About a day ago", False)):
            self.assertEqual(CACHE.old_enough(value), expected)
        self.assertNotEqual(self.fingerprint(), CACHE.records("kodex-local-dev", wire(record()))[TARGET]["fingerprint"])

    def test_main_external_failure_is_closed(self):
        for failure in (OSError(CANARY), CACHE.Failure("DOCKER_COMMAND_FAILED")):
            output = io.StringIO()
            with patch.object(CACHE, "docker", side_effect=failure), contextlib.redirect_stdout(output):
                self.assertEqual(CACHE.main(["audit", "--builder", "default"]), 1)
            self.assertNotIn(CANARY, output.getvalue())

    def test_exact_target_positive_and_other_record_preserved(self):
        source = wire(record(), record(OTHER))
        result, mocked = self.run_prune([source, source, b"ignored provider output", wire(record(OTHER))])
        self.assertEqual(result["status"], "PASS")
        self.assertEqual(result["outcomes"], [{"id": TARGET, "effect": "ABSENT_AFTER", "sizeBefore": "2.292GB"}])
        self.assertEqual(mocked.call_args_list[2].args[:3], ("default", "prune", TARGET))
        self.assertEqual(result["spaceMeasurement"], "HOST_ROOT_CONCURRENT_NOT_ATTRIBUTED")

    def test_unsafe_or_recent_never_pruned(self):
        for changes in ({"Shared": True}, {"Reclaimable": False}, {"LastUsedAt": "23 hours ago"},
                        {"LastUsedAt": "1 day ago"}, {"LastUsedAt": "UNKNOWN"}):
            with self.subTest(changes=changes), patch.object(CACHE, "docker", return_value=wire(record(**changes))) as mocked:
                with self.assertRaises(CACHE.Failure):
                    CACHE.execute("prune", "default", [TARGET], [self.fingerprint()])
                self.assertEqual(mocked.call_count, 1)

    def test_target_binding_foreign_builder_and_injection(self):
        for builder, targets, hashes in (("foreign", [TARGET], [self.fingerprint()]),
                                         ("default", ["--all"], [self.fingerprint()]),
                                         ("default", [TARGET, TARGET], [self.fingerprint()] * 2),
                                         ("default", [TARGET], []),
                                         ("default", [TARGET] * 129, [self.fingerprint()] * 129)):
            with self.subTest(builder=builder), patch.object(CACHE, "docker") as mocked:
                with self.assertRaises(CACHE.Failure):
                    CACHE.execute("prune", builder, targets, hashes)
                mocked.assert_not_called()
        with patch.object(CACHE, "docker", return_value=wire(record(OTHER))):
            with self.assertRaises(CACHE.Failure):
                CACHE.execute("prune", "default", [TARGET], [self.fingerprint()])

    def test_before_effect_fingerprint_and_age_rechecked(self):
        for changes in ({"Size": "3GB"}, {"Description": "changed"}, {"CreatedAt": "5 days ago"},
                        {"Mutable": True}, {"Reclaimable": False}, {"LastUsedAt": "15 hours ago"}):
            with self.subTest(changes=changes):
                result, mocked = self.run_prune([wire(record()), wire(record(**changes))])
                self.assertEqual(result["status"], "PARTIAL")
                self.assertEqual(result["outcomes"][0]["effect"], "NOT_ATTEMPTED")
                self.assertEqual(mocked.call_count, 2)

    def test_timeout_readback_no_retry_and_remaining_not_attempted(self):
        source = wire(record(), record(OTHER))
        result, mocked = self.run_prune([source, source, CACHE.Failure("DOCKER_COMMAND_TIMEOUT"), source],
                                       [TARGET, OTHER])
        self.assertEqual(result["status"], "PARTIAL")
        self.assertEqual(result["outcomes"][0]["effect"], "PRESENT_AFTER")
        self.assertEqual(result["outcomes"][1]["effect"], "NOT_ATTEMPTED")
        self.assertEqual(sum(call.args[1] == "prune" for call in mocked.call_args_list), 1)

    def test_partial_after_first_success(self):
        source = wire(record(), record(OTHER))
        rest = wire(record(OTHER))
        result, _ = self.run_prune([source, source, b"", rest, rest,
                                   CACHE.Failure("DOCKER_COMMAND_FAILED"), CACHE.Failure("DOCKER_COMMAND_FAILED")],
                                  [TARGET, OTHER])
        self.assertEqual([item["effect"] for item in result["outcomes"]], ["ABSENT_AFTER", "UNKNOWN"])
        self.assertEqual(result["status"], "PARTIAL")

    def test_malformed_unknown_duplicate_and_output_limits(self):
        invalid = [b"not json " + CANARY.encode(), b"{}", wire(record(), record()),
                   wire(record(Size=CANARY)), wire(record(Shared="false")),
                   b'{"ID":"a","ID":"b"}', b"x" * (CACHE.MAX_OUTPUT + 1)]
        for raw in invalid:
            with self.subTest(length=len(raw)), self.assertRaises(CACHE.Failure):
                CACHE.records("default", raw)

    def test_docker_commands_closed_filters_environment_and_bounds(self):
        # Остановка на Popen позволяет проверить argv без Docker.
        for mode in ("du", "prune"):
            with patch.object(CACHE.subprocess, "Popen", side_effect=OSError(CANARY)) as mocked:
                with self.assertRaisesRegex(CACHE.Failure, "^DOCKER_COMMAND_FAILED$"):
                    CACHE.docker("default", mode, TARGET if mode == "prune" else None)
            command = mocked.call_args.args[0]
            self.assertEqual(command[:5], ["/usr/bin/docker", "--host", "unix:///var/run/docker.sock", "buildx", mode])
            self.assertNotIn("--all", command)
            self.assertNotIn("parents", " ".join(command))
            if mode == "prune":
                self.assertEqual(command[7:], ["--force", "--filter", "id=" + TARGET,
                                              "--filter", "until=24h", "--filter", "private"])
            else:
                self.assertEqual(command[7:], ["--format", "{{json .}}"])
            self.assertEqual(set(mocked.call_args.kwargs["env"]), {"PATH", "HOME", "LC_ALL", "LANG"})
            self.assertEqual(mocked.call_args.kwargs["stderr"], CACHE.subprocess.DEVNULL)
        with patch.object(CACHE.subprocess, "Popen") as mocked:
            with self.assertRaisesRegex(CACHE.Failure, "BUDGET_EXHAUSTED"):
                CACHE.docker("default", "du", deadline=1)
            mocked.assert_not_called()

    def test_actual_pipe_output_limit_and_timeout_without_docker(self):
        original = CACHE.subprocess.Popen
        for source, limit, budget, expected in (
                ("print('x' * 100)", 64, 2, "DOCKER_OUTPUT_LIMIT"),
                ("import time; time.sleep(2)", 1024, 0.05, "DOCKER_COMMAND_TIMEOUT")):
            def fixture(command, **options):
                return original([sys.executable, "-c", source], **options)
            with self.subTest(expected=expected), patch.object(CACHE.subprocess, "Popen", side_effect=fixture), \
                    patch.object(CACHE, "MAX_OUTPUT", limit), patch.object(CACHE, "COMMAND_SECONDS", budget):
                with self.assertRaisesRegex(CACHE.Failure, "^" + expected + "$"):
                    CACHE.docker("default", "du")

    def test_cli_invalid_arguments_and_provider_error_are_redacted(self):
        for arguments in (["prune", "--builder", CANARY], ["prune", "--builder", "default", "--targetid", CANARY]):
            output = io.StringIO()
            with contextlib.redirect_stdout(output), patch.object(CACHE, "docker") as mocked:
                try:
                    code = CACHE.main(arguments)
                except SystemExit as failure:
                    code = failure.code
            self.assertNotEqual(code, 0)
            self.assertNotIn(CANARY, output.getvalue())
            mocked.assert_not_called()


if __name__ == "__main__":
    unittest.main()
