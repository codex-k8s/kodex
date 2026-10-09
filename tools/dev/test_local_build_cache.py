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
    def fingerprint(self, value=None, minimum_age_hours=24):
        value = value or record()
        return CACHE.records("default", wire(value), minimum_age_hours)[value["ID"]]["fingerprint"]

    def run_prune(self, responses, targets=(TARGET,), fingerprints=None, minimum_age_hours=24):
        if fingerprints is None:
            fingerprints = [self.fingerprint(record(target), minimum_age_hours) for target in targets]
        with patch.object(CACHE, "docker", side_effect=responses) as mocked, \
                patch.object(CACHE, "free_bytes", side_effect=[100, 120]):
            result = CACHE.execute("prune", "default", targets, fingerprints,
                                   minimum_age_hours=minimum_age_hours)
        return result, mocked

    def test_explicit_profiles_and_conservative_boundaries(self):
        for profile, value, expected in ((4, "5 hours ago", True), (4, "4 hours ago", False),
                                         (4, "1 day ago", True), (4, "8 hours ago", True),
                                         (4, "UNKNOWN", False), (24, "8 hours ago", False),
                                         (24, "25 hours ago", True), (24, "24 hours ago", False),
                                         (24, "2 days ago", True), (24, "1 day ago", False)):
            with self.subTest(profile=profile, value=value):
                self.assertEqual(CACHE.old_enough(value, profile), expected)
        for profile, count in ((4, 1), (24, 0)):
            with patch.object(CACHE, "docker", return_value=wire(record(LastUsedAt="8 hours ago"))):
                result = CACHE.execute("audit", "default", minimum_age_hours=profile)
            self.assertEqual(result["minimumAgeHours"], profile)
            self.assertEqual(len(result["records"]), count)

    def test_cross_profile_fingerprint_rejected_before_effect(self):
        for before, after in ((4, 24), (24, 4)):
            with patch.object(CACHE, "docker", return_value=wire(record())) as mocked:
                with self.assertRaisesRegex(CACHE.Failure, "TARGET_CHANGED"):
                    CACHE.execute("prune", "default", [TARGET], [self.fingerprint(minimum_age_hours=before)],
                                  minimum_age_hours=after)
                self.assertEqual(mocked.call_count, 1)

    def test_profile_prune_uses_exact_until_and_reports_profile(self):
        for profile in (4, 24):
            source = wire(record())
            result, mocked = self.run_prune([source, source, b"", b""], minimum_age_hours=profile)
            self.assertEqual(result["status"], "PASS")
            self.assertEqual(result["minimumAgeHours"], profile)
            self.assertTrue(all(call.kwargs["minimum_age_hours"] == profile for call in mocked.call_args_list))
            with patch.object(CACHE.subprocess, "Popen", side_effect=OSError(CANARY)) as mocked:
                with self.assertRaises(CACHE.Failure):
                    CACHE.docker("default", "prune", [TARGET], minimum_age_hours=profile)
            self.assertEqual(mocked.call_args.args[0][-4:],
                             ["--filter", f"until={profile}h", "--filter", 'private=""'])

    def test_invalid_profile_no_docker_and_cli_default(self):
        for value in (0, 3, 5, 48, "4", True, None):
            with self.subTest(value=value), patch.object(CACHE, "docker") as mocked:
                with self.assertRaisesRegex(CACHE.Failure, "AGE_PROFILE_REJECTED"):
                    CACHE.execute("audit", "default", minimum_age_hours=value)
                mocked.assert_not_called()
        for arguments, expected in (([], 24), (["--minimum-age-hours", "4"], 4)):
            with patch.object(CACHE, "execute", return_value={"status": "PASS"}) as mocked, \
                    contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(CACHE.main(["audit", "--builder", "default", *arguments]), 0)
            self.assertEqual(mocked.call_args.kwargs["minimum_age_hours"], expected)
        for value in ("5", "0", CANARY):
            output = io.StringIO()
            with patch.object(CACHE, "docker") as mocked, contextlib.redirect_stdout(output):
                with self.assertRaises(SystemExit):
                    CACHE.main(["audit", "--builder", "default", "--minimum-age-hours", value])
            self.assertNotIn(CANARY, output.getvalue())
            mocked.assert_not_called()

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

    def test_referenced_parent_is_not_pruned_before_leaf(self):
        source = wire(record(), record(OTHER, Parents=[TARGET]))
        parsed = CACHE.records("default", source)
        self.assertFalse(parsed[TARGET]["eligible"])
        self.assertTrue(parsed[OTHER]["eligible"])
        self.assertTrue(CACHE.records("default", wire(record()))[TARGET]["eligible"])
        with patch.object(CACHE, "docker", return_value=source) as mocked:
            with self.assertRaisesRegex(CACHE.Failure, "TARGET_NOT_ELIGIBLE"):
                CACHE.execute("prune", "default", [TARGET], [self.fingerprint()])
            self.assertEqual(mocked.call_count, 1)

    def test_invalid_parent_reference_is_closed(self):
        for parents in ("wrong", False, ["--all"], [42]):
            with self.subTest(parents=parents), self.assertRaises(CACHE.Failure):
                CACHE.records("default", wire(record(Parents=parents)))

    def test_internal_frontend_unknown_types_excluded(self):
        for cache_type in ("internal", "frontend", "unrecognized"):
            self.assertFalse(CACHE.records("default", wire(record(Type=cache_type)))[TARGET]["eligible"])

    def test_exact_target_positive_and_other_record_preserved(self):
        source = wire(record(), record(OTHER))
        result, mocked = self.run_prune([source, source, b"ignored provider output", wire(record(OTHER))])
        self.assertEqual(result["status"], "PASS")
        self.assertEqual(result["outcomes"], [{"id": TARGET, "effect": "ABSENT_AFTER", "sizeBefore": "2.292GB"}])
        self.assertEqual(mocked.call_args_list[2].args[:3], ("default", "prune", (TARGET,)))
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

    def test_timeout_batch_readback_no_retry(self):
        source = wire(record(), record(OTHER))
        result, mocked = self.run_prune([source, source, CACHE.Failure("DOCKER_COMMAND_TIMEOUT"), source],
                                       [TARGET, OTHER])
        self.assertEqual(result["status"], "PARTIAL")
        self.assertEqual(result["outcomes"][0]["effect"], "PRESENT_AFTER")
        self.assertEqual(result["outcomes"][1]["effect"], "PRESENT_AFTER")
        self.assertEqual(sum(call.args[1] == "prune" for call in mocked.call_args_list), 1)

    def test_batch_partial_survivor_and_unknown_readback(self):
        source = wire(record(), record(OTHER))
        rest = wire(record(OTHER))
        result, mocked = self.run_prune([source, source, b"", rest],
                                  [TARGET, OTHER])
        self.assertEqual([item["effect"] for item in result["outcomes"]], ["ABSENT_AFTER", "PRESENT_AFTER"])
        self.assertEqual(result["status"], "PARTIAL")
        self.assertEqual(mocked.call_count, 4)

    def test_batch_all_128_selected_ids_one_effect(self):
        targets = tuple(f"{index:024d}" for index in range(128))
        source = wire(*(record(target) for target in targets), record(OTHER))
        result, mocked = self.run_prune([source, source, b"", wire(record(OTHER))], targets)
        self.assertEqual(result["status"], "PASS")
        self.assertEqual(len(result["outcomes"]), 128)
        self.assertTrue(all(item["effect"] == "ABSENT_AFTER" for item in result["outcomes"]))
        self.assertEqual(mocked.call_count, 4)
        self.assertEqual(mocked.call_args_list[2].args[:3], ("default", "prune", targets))

    def test_batch_changed_last_member_or_new_parent_ref_prevents_all_effects(self):
        source = wire(record(), record(OTHER))
        for fresh in (wire(record(), record(OTHER, Description="changed")),
                      wire(record(), record(OTHER), record("c" * 24, Parents=[TARGET])),
                      CACHE.Failure("DOCKER_COMMAND_FAILED")):
            with self.subTest(fresh_type=type(fresh).__name__):
                result, mocked = self.run_prune([source, fresh], [TARGET, OTHER])
                self.assertEqual(result["status"], "PARTIAL")
                self.assertEqual([item["effect"] for item in result["outcomes"]],
                                 ["NOT_ATTEMPTED", "NOT_ATTEMPTED"])
                self.assertEqual(mocked.call_count, 2)

    def test_batch_timeout_keeps_individual_partial_readback(self):
        source = wire(record(), record(OTHER))
        result, mocked = self.run_prune([source, source, CACHE.Failure("DOCKER_COMMAND_TIMEOUT"),
                                        wire(record(OTHER))], [TARGET, OTHER])
        self.assertEqual([item["effect"] for item in result["outcomes"]],
                         ["ABSENT_AFTER", "PRESENT_AFTER"])
        self.assertTrue(all(item["error"] == "DOCKER_COMMAND_TIMEOUT" for item in result["outcomes"]))
        self.assertEqual(result["status"], "PARTIAL")
        self.assertEqual(mocked.call_count, 4)

    def test_batch_regex_matches_only_exact_selected_ids(self):
        for targets in ((TARGET,), (TARGET, OTHER)):
            with patch.object(CACHE.subprocess, "Popen", side_effect=OSError(CANARY)) as mocked:
                with self.assertRaises(CACHE.Failure):
                    CACHE.docker("default", "prune", targets)
            command = mocked.call_args.args[0]
            selector = command[command.index("--filter") + 1]
            self.assertEqual(selector, "id=^(" + "|".join(targets) + ")$")
            compiled = CACHE.re.compile(selector[3:])
            self.assertTrue(all(compiled.fullmatch(target) for target in targets))
            self.assertFalse(compiled.fullmatch("c" * 24))
            self.assertFalse(compiled.fullmatch("prefix" + TARGET))
            self.assertFalse(compiled.fullmatch(TARGET + "suffix"))
            self.assertNotIn("--all", command)
            self.assertEqual(command[-4:], ["--filter", "until=24h", "--filter", 'private=""'])
        for targets in ((), [TARGET, TARGET], [TARGET, "a.*"], tuple(f"{i:024d}" for i in range(129))):
            with patch.object(CACHE.subprocess, "Popen") as mocked:
                with self.assertRaises(CACHE.Failure):
                    CACHE.docker("default", "prune", targets)
                mocked.assert_not_called()

    def test_batch_readback_unavailable_is_unknown_for_every_target(self):
        source = wire(record(), record(OTHER))
        result, mocked = self.run_prune([source, source, CACHE.Failure("DOCKER_COMMAND_TIMEOUT"),
                                        CACHE.Failure("DOCKER_COMMAND_FAILED")], [TARGET, OTHER])
        self.assertEqual([item["effect"] for item in result["outcomes"]], ["UNKNOWN", "UNKNOWN"])
        self.assertEqual(result["status"], "PARTIAL")
        self.assertEqual(mocked.call_count, 4)

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
                self.assertEqual(command[7:], ["--force", "--filter", "id=^(" + TARGET + ")$",
                                              "--filter", "until=24h", "--filter", 'private=""'])
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
