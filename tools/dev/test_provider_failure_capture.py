"""Синтетические boundary/privacy tests; live kubectl не запускается."""
import copy
import importlib.util
import io
import json
from pathlib import Path
import re
import subprocess
import sys
import time
import unittest
from unittest.mock import patch


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


CAPTURE = load('provider_failure', 'provider-failure-capture.py')
FIXTURE = load('provider_ack_fixture', 'test_provider_input_ack_capture.py')
SENTINEL = 'PRIVATE_SECRET_BODY_SENTINEL'


def fixture():
    proof, columns, options = FIXTURE.fixture()
    options.timeout_seconds, options.pod_uid = 30, None
    return proof, columns, options


def request(**fields):
    values = dict(stage='TERMINAL_WAIT', category='PROVIDER', detail='STREAM_CLOSED', code=0,
                  notification='NONE', account='NONE', notification_error='NONE')
    values.update(fields)
    return ('2026/10/07 07:49:00 ' + CAPTURE.REQUEST_PREFIX + '{stage}; class: {category}; '
            'detail: {detail}; rpc_code: {code}; notification: {notification}; '
            'account_read: {account}; notification_error: {notification_error}\n').format(**values).encode()


class FakeClock:
    def __init__(self):
        self.value = 0

    def now(self):
        return self.value

    def sleep(self, seconds):
        self.value += seconds


class CaptureTests(unittest.TestCase):
    def exercise(self, proof=None, columns=None, options=None, after=None, log=None, diagnostic=None):
        baseline, baseline_columns, baseline_options = fixture()
        proof = baseline if proof is None else proof
        columns = baseline_columns if columns is None else columns
        options = baseline_options if options is None else options
        calls, followed, closed = [], [], []
        gets = 0

        def read(args):
            nonlocal gets
            calls.append(args)
            if args[0] == 'get':
                gets += 1
                if gets >= 3 and after is not None:
                    if isinstance(after, Exception):
                        raise after
                    return after
                return columns
            self.assertEqual(args[0], 'logs')
            return (json.dumps({'event': CAPTURE.ACK.EVENT, 'proof': proof,
                               'private': SENTINEL}).encode() if log is None else log)

        def follow(pod):
            followed.append(pod['uid'])
            try:
                yield json.dumps({'event': CAPTURE.ACK.EVENT, 'proof': proof,
                                  'private': SENTINEL}).encode() + b'\n'
                yield (SENTINEL + '\n').encode()
                yield request() if diagnostic is None else diagnostic
                self.fail('capture did not close follow after exact diagnostic')
            finally:
                closed.append(True)

        clock = FakeClock()
        result = CAPTURE.capture(options, read, follow, now=clock.now, sleep=clock.sleep)
        return result, calls, followed, closed

    def test_exact_capture_rejoin_and_privacy(self):
        result, calls, followed, closed = self.exercise()
        self.assertEqual(result['status'], 'CAPTURED')
        self.assertEqual(result['rejoin'], 'VERIFIED')
        self.assertEqual(result['diagnostic']['stage'], 'TERMINAL_WAIT')
        self.assertEqual(result['diagnostic']['class'], 'PROVIDER')
        self.assertNotIn(SENTINEL, json.dumps(result))
        self.assertEqual([call[0] for call in calls], ['get', 'logs', 'get', 'get'])
        self.assertEqual(len(followed), 1)
        self.assertEqual(closed, [True])

    def test_system_and_project_use_same_source_scope_contract(self):
        for scope in ('SYSTEM', 'PROJECT'):
            with self.subTest(scope=scope):
                proof, columns, options = fixture()
                proof['assistant_scope'] = options.assistant_scope = scope
                if scope == 'SYSTEM':
                    del proof['project_ref']
                result, _, _, _ = self.exercise(proof, columns, options)
                self.assertEqual(result['assistant_scope'], scope)

    def test_pod_cleanup_does_not_forge_rejoin(self):
        result, _, _, _ = self.exercise(after=b'')
        self.assertEqual(result['rejoin'], 'POD_CLEANED_AFTER_CAPTURE')

    def test_transport_unavailable_after_capture_is_explicit(self):
        result, _, _, _ = self.exercise(after=CAPTURE.Failure('KUBECTL_READ_FAILED'))
        self.assertEqual(result['rejoin'], 'READ_UNAVAILABLE_AFTER_CAPTURE')

    def test_changed_uid_after_capture_rejects(self):
        _, columns, _ = fixture()
        changed = columns.replace(b'12345678-1234-1234-1234-123456789abc',
                                  b'12345678-1234-1234-1234-123456789abd')
        with self.assertRaisesRegex(CAPTURE.Failure, '^POD_CHANGED_AFTER_CAPTURE$'):
            self.exercise(after=changed)

    def test_rejoin_pending_replacement_not_falsely_reported_cleaned(self):
        _, columns, _ = fixture()
        changed = columns.replace(b'|Running|', b'|Pending|').replace(
            b'12345678-1234-1234-1234-123456789abc', b'12345678-1234-1234-1234-123456789abd')
        with self.assertRaisesRegex(CAPTURE.Failure, '^POD_CHANGED_AFTER_CAPTURE$'):
            self.exercise(after=changed)

    def test_corrupt_pin_after_capture_not_transport_exception(self):
        _, columns, _ = fixture()
        with self.assertRaises(CAPTURE.Failure):
            self.exercise(after=columns.replace(b'a' * 64, b'c' * 64, 1))

    def test_foreign_tuple_never_reads_logs_and_timeout_bounded(self):
        proof, columns, options = fixture()
        for field in ('session_ref', 'turn_ref', 'attempt'):
            with self.subTest(field=field):
                candidate = copy.copy(options)
                setattr(candidate, field, 'foreign_reference' if field != 'attempt' else 2)
                clock, calls = FakeClock(), []

                def read(args):
                    calls.append(args)
                    self.assertEqual(args[0], 'get')
                    return columns

                with self.assertRaisesRegex(CAPTURE.Failure, '^EXACT_ACK_NOT_OBSERVED$'):
                    CAPTURE.capture(candidate, read, lambda pod: self.fail('foreign follow'),
                                    now=clock.now, sleep=clock.sleep)
                self.assertEqual(clock.value, 30)
                self.assertEqual(len(calls), 60)

    def test_wrong_image_rejected_before_log_read(self):
        _, columns, options = fixture()
        options.image_manifest = 'sha256:' + 'c' * 64
        calls = []

        def read(args):
            calls.append(args[0])
            return columns

        with self.assertRaisesRegex(CAPTURE.Failure, '^POD_IMAGE_MISMATCH$'):
            CAPTURE.capture(options, read, lambda pod: self.fail('follow'))
        self.assertEqual(calls, ['get'])

    def test_uid_pins_and_duplicate_pods_fail_before_logs(self):
        _, columns, options = fixture()
        options.pod_uid = '12345678-1234-1234-1234-123456789abd'
        with self.assertRaisesRegex(CAPTURE.Failure, '^POD_UID_MISMATCH$'):
            CAPTURE.selected_pod(lambda _: columns, options)
        options.pod_uid = None
        with self.assertRaisesRegex(CAPTURE.Failure, '^MULTIPLE_MATCHING_PODS$'):
            CAPTURE.selected_pod(lambda _: columns * 2, options)
        with self.assertRaises(CAPTURE.Failure):
            CAPTURE.selected_pod(lambda _: columns.replace(b'a' * 64, b'x' * 64, 1), options)

    def test_malformed_foreign_namespace_and_restart_denied(self):
        _, columns, options = fixture()
        for data in (b'bad\n', columns.replace(b'kodex-runtime', b'foreign-runtime'),
                     columns.replace(b'|0', b'|1')):
            with self.subTest(data=data[:8]), self.assertRaises(CAPTURE.Failure):
                CAPTURE.selected_pod(lambda _: data, options)

    def test_ack_mismatched_run_cannot_start_follow(self):
        proof, columns, options = fixture()
        proof['run_ref'] = 'run_foreign01'
        with self.assertRaisesRegex(CAPTURE.Failure, '^EXACT_ACK_NOT_OBSERVED$'):
            self.exercise(proof, columns, options)

    def test_ack_wrong_scope_or_pin_fails_closed(self):
        for field, value in (('assistant_scope', 'PROJECT'), ('runtime_revision_digest', 'c' * 64),
                             ('session_ref', 'ses_foreign01'), ('attempt', 2)):
            proof, columns, options = fixture()
            proof[field] = value
            with self.subTest(field=field), self.assertRaises(CAPTURE.Failure):
                self.exercise(proof, columns, options)

    def test_same_uid_rebound_before_follow_rejected(self):
        proof, columns, options = fixture()
        count = 0

        def read(args):
            nonlocal count
            if args[0] == 'logs':
                return json.dumps({'event': CAPTURE.ACK.EVENT, 'proof': proof}).encode()
            count += 1
            return columns if count == 1 else columns.replace(b'a' * 64, b'c' * 64, 1)

        with self.assertRaises(CAPTURE.Failure):
            CAPTURE.capture(options, read, lambda pod: self.fail('changed follow'))

    def test_known_request_fields_and_account_classification(self):
        parsed = CAPTURE.parse_line(request(stage='ACCOUNT_READ', detail='RPC_ERROR', code=-32603,
                                            account='DISCOVERY_UNAUTHORIZED'))
        self.assertEqual(parsed['rpc_code'], -32603)
        self.assertEqual(parsed['account_read'], 'DISCOVERY_UNAUTHORIZED')
        for stage in CAPTURE.STAGES:
            self.assertEqual(CAPTURE.parse_line(request(stage=stage))['stage'], stage)
        for category in CAPTURE.CLASSES:
            self.assertEqual(CAPTURE.parse_line(request(category=category))['class'], category)

    def test_closed_notification_and_terminal_codes(self):
        for method in CAPTURE.NOTIFICATIONS - {'NONE'}:
            parsed = CAPTURE.parse_line(request(detail='NOTIFICATION_INVALID', notification=method,
                                                notification_error='ENVELOPE'))
            self.assertEqual(parsed['notification'], method)
        for code in CAPTURE.TERMINAL_CODES:
            result = CAPTURE.parse_line(('2026/10/07 07:49:00 ' + CAPTURE.TERMINAL_PREFIX + code).encode())
            self.assertEqual(result, {'kind': 'TERMINAL_FAILURE', 'failure_code': code})

    def test_enum_sets_match_exact_repo_producers(self):
        source = Path(__file__).resolve().parents[2] / 'services/jobs/agent-runner/internal/codex'
        process = (source / 'process.go').read_text()
        stages = set(re.findall(r'providerStage\w+\s+providerExecutionStage = "([A-Z_]+)"', process))
        self.assertEqual(CAPTURE.STAGES, stages)
        parser = (source / 'parser.go').read_text()
        reasons = set(re.findall(r'tokenUsage\w+\s+tokenUsageFailureReason = "([A-Z_]+)"', parser))
        self.assertEqual(CAPTURE.TOKEN_USAGE_ERRORS, reasons)
        usage_guard = process.split('var usageFailure *tokenUsageFailure', 1)[1].split(
            'return &appServerCallFailure', 1)[0]
        self.assertEqual(CAPTURE.TOKEN_USAGE_METHODS,
                         set(re.findall(r'method == "([A-Za-z/._]+)"', usage_guard)))
        methods = parser.split('var serverNotificationMethods = stringSet(', 1)[1].split(')', 1)[0]
        broker = (source / 'broker.go').read_text()
        additional = broker.split('func safeNotificationMethod(', 1)[1].split('default:', 1)[0]
        notifications = set(re.findall(r'"([A-Za-z/._]+)"', methods + additional))
        self.assertEqual(CAPTURE.NOTIFICATIONS, notifications | {'NONE', 'UNKNOWN'})
        terminal = parser.split('func parseCodexErrorInfo(', 1)[1].split('func (state *protocolState) terminalResult', 1)[0]
        codes = set(re.findall(r'return "([a-z_]+)", true', terminal))
        self.assertEqual(CAPTURE.TERMINAL_CODES, codes | {
            'provider_error_info_invalid', 'provider_interrupted', 'RUNTIME_ARTIFACT_INVALID'})
        account = (source / 'account_read_failure.go').read_text().split('func safeAccountReadFailure(', 1)[1]
        self.assertEqual(CAPTURE.ACCOUNT_READ, set(re.findall(r'"([A-Z_]+)"', account)))

    def test_closed_token_usage_reasons_bind_exact_method_and_preserve_privacy(self):
        for reason in CAPTURE.TOKEN_USAGE_ERRORS | {'TOKEN_USAGE'}:
            for method in CAPTURE.TOKEN_USAGE_METHODS:
                with self.subTest(reason=reason, method=method):
                    parsed = CAPTURE.parse_line(request(detail='NOTIFICATION_INVALID',
                        notification=method, notification_error=reason))
                    self.assertEqual(parsed['notification_error'], reason)
                    self.assertEqual(parsed['notification'], method)
                    self.assertNotIn(SENTINEL, json.dumps(parsed))
                    self.assertEqual(set(parsed), {'kind', 'stage', 'class', 'detail', 'rpc_code',
                                                  'notification', 'account_read', 'notification_error'})
            if reason != 'TOKEN_USAGE':
                for method in CAPTURE.NOTIFICATIONS - CAPTURE.TOKEN_USAGE_METHODS:
                    with self.subTest(reason=reason, foreign_method=method), self.assertRaisesRegex(
                            CAPTURE.Failure, '^PROVIDER_DIAGNOSTIC_INVALID$'):
                        CAPTURE.parse_line(request(detail='NOTIFICATION_INVALID',
                            notification=method, notification_error=reason))
        for method in CAPTURE.TOKEN_USAGE_METHODS:
            with self.subTest(method=method), self.assertRaisesRegex(
                    CAPTURE.Failure, '^PROVIDER_DIAGNOSTIC_INVALID$') as caught:
                CAPTURE.parse_line(request(detail='NOTIFICATION_INVALID',
                    notification=method, notification_error='TOKEN_USAGE_'+SENTINEL))
            self.assertNotIn(SENTINEL, str(caught.exception))

    def test_new_usage_receipt_diagnostics_pass_exact_capture_and_remain_private(self):
        for reason in ('TOKEN_USAGE_RECEIPT_CONFLICT', 'TOKEN_USAGE_RECEIPT_LIMIT', 'TOKEN_USAGE_OVERFLOW'):
            for method in ('thread/tokenUsage/updated', 'rawResponse/completed'):
                with self.subTest(reason=reason, method=method):
                    diagnostic = request(detail='NOTIFICATION_INVALID', notification=method,
                                         notification_error=reason)
                    result, _, followed, closed = self.exercise(diagnostic=diagnostic)
                    self.assertEqual(result['diagnostic']['notification_error'], reason)
                    self.assertEqual(result['diagnostic']['notification'], method)
                    self.assertEqual(result['rejoin'], 'VERIFIED')
                    self.assertEqual(len(followed), 1)
                    self.assertEqual(closed, [True])
                    self.assertNotIn(SENTINEL, json.dumps(result))
                    self.assertEqual(CAPTURE.diagnostic_from_chunks(iter([diagnostic])), result['diagnostic'])
            for method in ('rawResponse/other', SENTINEL):
                with self.subTest(reason=reason, method=method):
                    with self.assertRaisesRegex(CAPTURE.Failure, '^PROVIDER_DIAGNOSTIC_INVALID$'):
                        CAPTURE.parse_line(request(detail='NOTIFICATION_INVALID',
                            notification=method, notification_error=reason))

    def test_unknown_or_malformed_diagnostic_never_reflects_sentinel(self):
        for field in ('stage', 'category', 'detail', 'notification', 'account', 'notification_error', 'code'):
            with self.subTest(field=field), self.assertRaisesRegex(
                    CAPTURE.Failure, '^PROVIDER_DIAGNOSTIC_INVALID$') as caught:
                CAPTURE.parse_line(request(**{field: SENTINEL}))
            self.assertNotIn(SENTINEL, str(caught.exception))
        with self.assertRaisesRegex(CAPTURE.Failure, '^PROVIDER_DIAGNOSTIC_INVALID$'):
            CAPTURE.parse_line((CAPTURE.TERMINAL_PREFIX + SENTINEL).encode())

    def test_inconsistent_diagnostic_fields_and_integer_bound(self):
        for fields in ({'code': 5}, {'code': 1 << 63, 'detail': 'RPC_ERROR'},
                       {'notification': 'error'}, {'notification_error': 'ITEM'},
                       {'account': 'DISCOVERY_FAILED'}, {'detail': 'NOTIFICATION_INVALID'}):
            with self.subTest(fields=fields), self.assertRaises(CAPTURE.Failure):
                CAPTURE.parse_line(request(**fields))

    def test_unrelated_raw_lines_and_invalid_utf8_are_not_output(self):
        for raw in (SENTINEL.encode(), b'\xff' + SENTINEL.encode(),
                    json.dumps({'input': SENTINEL, 'failure': 'provider_error'}).encode()):
            self.assertIsNone(CAPTURE.parse_line(raw))

    def test_chunked_diagnostic_with_large_ack_and_private_lines(self):
        raw = (b'{"proof":"' + b'x' * 20000 + b'"}\n' + SENTINEL.encode() + b'\n' + request())
        result = CAPTURE.diagnostic_from_chunks(iter(raw[index:index + 17] for index in range(0, len(raw), 17)))
        self.assertEqual(result['class'], 'PROVIDER')
        self.assertNotIn(SENTINEL, json.dumps(result))

    def test_oversized_known_diagnostic_rejected(self):
        with self.assertRaisesRegex(CAPTURE.Failure, '^PROVIDER_DIAGNOSTIC_INVALID$'):
            CAPTURE.diagnostic_from_chunks(iter([CAPTURE.REQUEST_PREFIX.encode() + b'x' * 5000 + b'\n']))

    def test_no_diagnostic_is_not_pass(self):
        with self.assertRaisesRegex(CAPTURE.Failure, '^FOLLOW_STREAM_ENDED$'):
            CAPTURE.diagnostic_from_chunks(iter([SENTINEL.encode() + b'\n']))

    def test_only_elapsed_deadline_reports_no_failure_before_deadline(self):
        clock = FakeClock()
        with self.assertRaisesRegex(CAPTURE.Failure, '^FOLLOW_STREAM_ENDED$'):
            CAPTURE.diagnostic_from_chunks(iter([]), deadline=30, now=clock.now)
        clock.value = 30
        with self.assertRaisesRegex(CAPTURE.Failure, '^FAILURE_NOT_OBSERVED_BEFORE_DEADLINE$'):
            CAPTURE.diagnostic_from_chunks(iter([]), deadline=30, now=clock.now)

    def test_no_follow_ack_at_early_eof_is_explicit(self):
        proof, _, options = fixture()
        expected = CAPTURE.ACK.project_ack({'event': CAPTURE.ACK.EVENT, 'proof': proof}, options.run_ref)
        with self.assertRaisesRegex(CAPTURE.Failure, '^FOLLOW_ACK_NOT_OBSERVED$'):
            CAPTURE.diagnostic_from_chunks(iter([SENTINEL.encode()]), expected, options.run_ref)

    def exercise_early_eof(self, after=None, rejoined_proof=None, later_diagnostic=False,
                           transport_error=False, deadline_on_eof=False):
        proof, columns, options = fixture()
        clock, follows, closed, calls = FakeClock(), [], [], []
        gets, logs = 0, 0

        def read(args):
            nonlocal gets, logs
            calls.append(args[0])
            if args[0] == 'get':
                gets += 1
                if gets >= 3 and isinstance(after, Exception):
                    raise after
                return after if gets >= 3 and after is not None else columns
            logs += 1
            value = rejoined_proof if logs > 1 and rejoined_proof is not None else proof
            return json.dumps({'event': CAPTURE.ACK.EVENT, 'proof': value}).encode()

        def follow(pod):
            follows.append(pod['uid'])
            try:
                yield json.dumps({'event': CAPTURE.ACK.EVENT, 'proof': proof}).encode() + b'\n'
                if transport_error:
                    raise CAPTURE.Failure('KUBECTL_FOLLOW_FAILED')
                if deadline_on_eof:
                    clock.value = 30
                if later_diagnostic and len(follows) == 2:
                    yield request()
            finally:
                closed.append(True)

        try:
            result = CAPTURE.capture(options, read, follow, now=clock.now, sleep=clock.sleep)
        except CAPTURE.Failure as error:
            result = {'status': 'NOT_CAPTURED', 'code': str(error)}
        self.assertEqual(len(closed), len(follows))
        self.assertNotIn(SENTINEL, json.dumps(result))
        return result, follows, calls, clock.value

    def test_clean_eof_rejoins_same_uid_exact_ack_for_later_failure(self):
        result, follows, _, elapsed = self.exercise_early_eof(later_diagnostic=True)
        self.assertEqual(result['status'], 'CAPTURED')
        self.assertEqual(len(follows), 2)
        self.assertEqual(len(set(follows)), 1)
        self.assertEqual(elapsed, 0.5)

    def test_clean_eof_rejoin_budget_is_bounded_and_not_provider_pass(self):
        result, follows, _, elapsed = self.exercise_early_eof()
        self.assertEqual(result, {'status': 'NOT_CAPTURED', 'code': 'FOLLOW_STREAM_ENDED'})
        self.assertEqual(len(follows), 1 + CAPTURE.MAX_FOLLOW_REJOINS)
        self.assertEqual(elapsed, 0.5 * CAPTURE.MAX_FOLLOW_REJOINS)

    def test_clean_eof_missing_terminal_and_pending_are_explicit(self):
        _, columns, _ = fixture()
        for after, expected in ((b'', 'FOLLOW_POD_CLEANED'),
                                (columns.replace(b'|Running|', b'|Succeeded|'), 'FOLLOW_POD_TERMINAL'),
                                (columns.replace(b'|Running|', b'|Failed|'), 'FOLLOW_POD_TERMINAL'),
                                (columns.replace(b'|Running|', b'|Pending|'), 'FOLLOW_POD_NOT_RUNNING')):
            with self.subTest(expected=expected):
                result, follows, _, _ = self.exercise_early_eof(after=after)
                self.assertEqual(result['code'], expected)
                self.assertEqual(len(follows), 1)

    def test_clean_eof_replacement_and_changed_binding_fail_closed(self):
        proof, columns, _ = fixture()
        changed = columns.replace(b'12345678-1234-1234-1234-123456789abc',
                                  b'12345678-1234-1234-1234-123456789abd')
        result, follows, _, _ = self.exercise_early_eof(after=changed)
        self.assertEqual(result['code'], 'POD_CHANGED_BEFORE_FOLLOW')
        self.assertEqual(len(follows), 1)
        result, follows, _, _ = self.exercise_early_eof(after=columns.replace(b'a' * 64, b'c' * 64, 1))
        self.assertEqual(result['status'], 'NOT_CAPTURED')
        self.assertEqual(len(follows), 1)
        result, follows, _, _ = self.exercise_early_eof(rejoined_proof=dict(proof, lease_generation=2))
        self.assertEqual(result['code'], 'FOLLOW_ACK_BINDING_MISMATCH')
        self.assertEqual(len(follows), 1)
        result, follows, _, _ = self.exercise_early_eof(rejoined_proof=dict(proof, run_ref='run_foreign01'))
        self.assertEqual(result['code'], 'FOLLOW_ACK_NOT_OBSERVED')
        self.assertEqual(len(follows), 1)

    def test_follow_transport_failure_never_rejoins(self):
        result, follows, calls, _ = self.exercise_early_eof(transport_error=True)
        self.assertEqual(result['code'], 'KUBECTL_FOLLOW_FAILED')
        self.assertEqual(len(follows), 1)
        self.assertEqual(calls, ['get', 'logs', 'get'])

    def test_clean_eof_rejoin_read_failure_is_not_swallowed(self):
        result, follows, calls, _ = self.exercise_early_eof(after=CAPTURE.Failure('KUBECTL_READ_FAILED'))
        self.assertEqual(result['code'], 'KUBECTL_READ_FAILED')
        self.assertEqual(len(follows), 1)
        self.assertEqual(calls, ['get', 'logs', 'get', 'get'])

    def test_follow_deadline_never_rejoins(self):
        result, follows, calls, elapsed = self.exercise_early_eof(deadline_on_eof=True)
        self.assertEqual(result['code'], 'FAILURE_NOT_OBSERVED_BEFORE_DEADLINE')
        self.assertEqual(len(follows), 1)
        self.assertEqual(calls, ['get', 'logs', 'get'])
        self.assertEqual(elapsed, 30)

    def test_follow_requires_same_exact_ack_not_only_pre_read_metadata(self):
        proof, _, options = fixture()
        expected = CAPTURE.ACK.project_ack({'event': CAPTURE.ACK.EVENT, 'proof': proof}, options.run_ref)
        with self.assertRaisesRegex(CAPTURE.Failure, '^FOLLOW_ACK_NOT_OBSERVED$'):
            CAPTURE.diagnostic_from_chunks(iter([request()]), expected, options.run_ref)
        foreign = dict(proof, run_ref='run_foreign01')
        foreign_line = json.dumps({'event': CAPTURE.ACK.EVENT, 'proof': foreign}).encode() + b'\n'
        with self.assertRaisesRegex(CAPTURE.Failure, '^FOLLOW_ACK_NOT_OBSERVED$'):
            CAPTURE.diagnostic_from_chunks(iter([foreign_line, request()]), expected, options.run_ref)
        changed = dict(proof, lease_generation=2)
        changed_line = json.dumps({'event': CAPTURE.ACK.EVENT, 'proof': changed}).encode() + b'\n'
        with self.assertRaisesRegex(CAPTURE.Failure, '^FOLLOW_ACK_BINDING_MISMATCH$'):
            CAPTURE.diagnostic_from_chunks(iter([changed_line, request()]), expected, options.run_ref)

    def test_large_follow_ack_preserves_only_binding_no_payload(self):
        proof, _, options = fixture()
        expected = CAPTURE.ACK.project_ack({'event': CAPTURE.ACK.EVENT, 'proof': proof}, options.run_ref)
        raw = json.dumps({'event': CAPTURE.ACK.EVENT, 'proof': proof,
                          'private': SENTINEL * 500}).encode() + b'\n' + request()
        result = CAPTURE.diagnostic_from_chunks(iter(raw[index:index + 101] for index in range(0, len(raw), 101)),
                                                expected, options.run_ref)
        self.assertEqual(result['stage'], 'TERMINAL_WAIT')
        self.assertNotIn(SENTINEL, json.dumps(result))

    def test_options_bounds_and_private_cli_errors(self):
        for field, value in (('timeout_seconds', 29), ('timeout_seconds', 3601),
                             ('timeout_seconds', True), ('timeout_seconds', 3600.0),
                             ('attempt', 0), ('attempt', True), ('run_ref', SENTINEL + '/'),
                             ('image_manifest', SENTINEL), ('assistant_scope', SENTINEL)):
            _, _, options = fixture()
            setattr(options, field, value)
            with self.subTest(field=field), self.assertRaises(CAPTURE.Failure):
                CAPTURE.validate_options(options)
        output = io.StringIO()
        with patch('sys.stdout', output):
            status = CAPTURE.main(['--unknown-' + SENTINEL])
        self.assertEqual(status, 1)
        self.assertEqual(json.loads(output.getvalue())['code'], 'ARGUMENTS_INVALID')
        self.assertNotIn(SENTINEL, output.getvalue())

    def test_timeout_boundaries_accept_up_to_one_hour(self):
        for seconds in (30, 120, 240, 3600):
            _, _, options = fixture()
            options.timeout_seconds = seconds
            with self.subTest(seconds=seconds):
                CAPTURE.validate_options(options)

    def test_one_hour_capture_observes_late_failure_and_closes_follow(self):
        proof, columns, options = fixture()
        options.timeout_seconds = 3600
        clock, closed = FakeClock(), []

        def read(args):
            if args[0] == 'get':
                return columns
            return json.dumps({'event': CAPTURE.ACK.EVENT, 'proof': proof}).encode()

        def follow(pod):
            try:
                yield json.dumps({'event': CAPTURE.ACK.EVENT, 'proof': proof}).encode() + b'\n'
                clock.sleep(3599)
                yield request()
                self.fail('capture did not close long follow after exact diagnostic')
            finally:
                closed.append(True)

        result = CAPTURE.capture(options, read, follow, now=clock.now, sleep=clock.sleep)
        self.assertEqual(result['status'], 'CAPTURED')
        self.assertEqual(result['rejoin'], 'VERIFIED')
        self.assertEqual(clock.value, 3599)
        self.assertEqual(closed, [True])
        self.assertNotIn(SENTINEL, json.dumps(result))

    def test_cli_keeps_default_and_rejects_over_one_hour_before_io(self):
        _, _, options = fixture()
        arguments = ['--run-ref', options.run_ref, '--session-ref', options.session_ref,
                     '--turn-ref', options.turn_ref, '--attempt', str(options.attempt),
                     '--image-manifest', options.image_manifest]
        for extra, expected in (([], 120), (['--timeout-seconds', '3600'], 3600)):
            with self.subTest(seconds=expected), patch('sys.stdout', io.StringIO()), patch.object(
                    CAPTURE, 'capture', return_value={'status': 'CAPTURED'}) as captured:
                self.assertEqual(CAPTURE.main(arguments + extra), 0)
                self.assertEqual(captured.call_args.args[0].timeout_seconds, expected)
        output = io.StringIO()
        with patch('sys.stdout', output), patch.object(CAPTURE, 'Kubectl') as kubectl, patch.object(
                CAPTURE.tempfile, 'TemporaryDirectory') as temporary:
            self.assertEqual(CAPTURE.main(arguments + ['--timeout-seconds', '3601']), 1)
        kubectl.assert_not_called()
        temporary.assert_not_called()
        self.assertEqual(json.loads(output.getvalue()), {'status': 'NOT_CAPTURED', 'code': 'TIMEOUT_INVALID'})

    def test_cli_follow_failures_emit_only_closed_code_and_no_private_exception(self):
        _, _, options = fixture()
        arguments = ['--run-ref', options.run_ref, '--session-ref', options.session_ref,
                     '--turn-ref', options.turn_ref, '--attempt', str(options.attempt),
                     '--image-manifest', options.image_manifest]
        for code in ('KUBECTL_FOLLOW_FAILED', 'KUBECTL_FOLLOW_EXIT_TIMEOUT', 'FOLLOW_STREAM_ENDED',
                     'FOLLOW_POD_CLEANED', 'FOLLOW_POD_TERMINAL', 'FOLLOW_POD_NOT_RUNNING',
                     'FAILURE_NOT_OBSERVED_BEFORE_DEADLINE'):
            output = io.StringIO()
            with self.subTest(code=code), patch('sys.stdout', output), patch.object(
                    CAPTURE, 'capture', side_effect=CAPTURE.Failure(code)):
                self.assertEqual(CAPTURE.main(arguments), 1)
            self.assertEqual(json.loads(output.getvalue()), {'status': 'NOT_CAPTURED', 'code': code})
            self.assertNotIn(SENTINEL, output.getvalue())
        output = io.StringIO()
        with patch('sys.stdout', output), patch.object(CAPTURE, 'capture', side_effect=OSError(SENTINEL)):
            self.assertEqual(CAPTURE.main(arguments), 1)
        self.assertEqual(json.loads(output.getvalue()), {'status': 'NOT_CAPTURED', 'code': 'CAPTURE_IO_FAILED'})
        self.assertNotIn(SENTINEL, output.getvalue())

    def test_kubectl_exact_identity_private_cache_and_no_inherited_env(self):
        with patch.object(CAPTURE.subprocess, 'Popen') as popen:
            CAPTURE.Kubectl('/home/s/.cache/owned-private', time.monotonic() + 30).start(['get', 'pods'])
        args, kwargs = popen.call_args
        self.assertIn('--kubeconfig=/home/s/.kube/config', args[0])
        self.assertIn('--context=k3d-kodex', args[0])
        self.assertIn('--cache-dir=/home/s/.cache/owned-private', args[0])
        self.assertEqual(set(kwargs['env']), {'HOME', 'PATH', 'KUBECONFIG'})
        self.assertEqual(kwargs['env']['HOME'], '/home/s')
        self.assertEqual(kwargs['stderr'], subprocess.DEVNULL)
        self.assertEqual(kwargs['stdin'], subprocess.DEVNULL)

    def test_follow_timeout_uses_bounded_watch_budget_not_five_second_read_budget(self):
        for seconds in (30, 3600):
            with self.subTest(seconds=seconds), patch.object(CAPTURE.subprocess, 'Popen') as popen, patch.object(
                    CAPTURE.time, 'monotonic', return_value=0):
                CAPTURE.Kubectl('/home/s/.cache/owned-private', seconds).start(['logs', '--follow'])
                timeouts = [arg for arg in popen.call_args.args[0] if arg.startswith('--request-timeout=')]
                self.assertEqual(timeouts, ['--request-timeout=' + str(seconds + 1) + 's'])
                CAPTURE.Kubectl('/home/s/.cache/owned-private', seconds).start(['get', 'pods'])
                self.assertIn('--request-timeout=5s', popen.call_args.args[0])

    def test_cancel_is_closed_error_for_join_finally(self):
        with self.assertRaisesRegex(CAPTURE.Failure, '^CAPTURE_CANCELLED$'):
            CAPTURE.cancel_capture(15, None)

    def test_actual_local_synthetic_pipe_timeout_and_join(self):
        process = subprocess.Popen([sys.executable, '-c', 'import time;time.sleep(30)'],
                                   stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                                   stdin=subprocess.DEVNULL)
        started = time.monotonic()
        try:
            self.assertEqual(list(CAPTURE.chunks(process, started + 0.05)), [])
        finally:
            CAPTURE.stop_process(process)
        self.assertIsNotNone(process.returncode)
        self.assertTrue(process.stdout.closed)
        self.assertLess(time.monotonic() - started, 3)

    def test_actual_local_synthetic_pipe_limit_and_stderr_privacy(self):
        code = 'import sys;sys.stderr.write("' + SENTINEL + '");sys.stdout.write("x"*10000)'
        process = subprocess.Popen([sys.executable, '-c', code], stdout=subprocess.PIPE,
                                   stderr=subprocess.DEVNULL, stdin=subprocess.DEVNULL)
        try:
            with self.assertRaisesRegex(CAPTURE.Failure, '^PROVIDER_LOG_LIMIT$'):
                list(CAPTURE.chunks(process, time.monotonic() + 2, maximum=5000))
        finally:
            CAPTURE.stop_process(process)
        self.assertIsNotNone(process.returncode)

    def actual_follow(self, code, seconds=2):
        process = subprocess.Popen([sys.executable, '-c', code], stdout=subprocess.PIPE,
                                   stderr=subprocess.DEVNULL, stdin=subprocess.DEVNULL)
        client = CAPTURE.Kubectl('/unused', time.monotonic() + seconds)
        with patch.object(client, 'start', return_value=process):
            try:
                result = CAPTURE.diagnostic_from_chunks(client.follow({'name': 'synthetic'}),
                                                        deadline=client.deadline)
            except CAPTURE.Failure as error:
                result = {'status': 'NOT_CAPTURED', 'code': str(error)}
        self.assertIsNotNone(process.returncode)
        self.assertTrue(process.stdout.closed)
        self.assertNotIn(SENTINEL, json.dumps(result))
        return result, process

    def test_actual_follow_nonzero_eof_is_transport_error_not_deadline(self):
        result, process = self.actual_follow('import sys;sys.stderr.write("' + SENTINEL + '");sys.exit(7)')
        self.assertEqual(result['code'], 'KUBECTL_FOLLOW_FAILED')
        self.assertEqual(process.returncode, 7)

    def test_actual_follow_zero_eof_is_stream_end_not_deadline(self):
        result, process = self.actual_follow('import sys;sys.stdout.write("' + SENTINEL + '\\n")')
        self.assertEqual(result['code'], 'FOLLOW_STREAM_ENDED')
        self.assertEqual(process.returncode, 0)

    def test_actual_follow_partial_safe_line_is_consumed_on_clean_eof(self):
        code = 'import sys;sys.stdout.buffer.write(' + repr(request().rstrip(b'\n')) + ')'
        result, process = self.actual_follow(code)
        self.assertEqual(result['kind'], 'REQUEST_FAILURE')
        self.assertEqual(process.returncode, 0)

    def test_actual_follow_deadline_terminates_and_joins_owned_child(self):
        started = time.monotonic()
        result, process = self.actual_follow('import time;time.sleep(30)', seconds=0.05)
        self.assertEqual(result['code'], 'FAILURE_NOT_OBSERVED_BEFORE_DEADLINE')
        self.assertLess(process.returncode, 0)
        self.assertLess(time.monotonic() - started, 1)

    def test_actual_follow_eof_while_process_alive_is_exit_timeout_and_joined(self):
        result, process = self.actual_follow('import os,time;os.close(1);time.sleep(30)')
        self.assertEqual(result['code'], 'KUBECTL_FOLLOW_EXIT_TIMEOUT')
        self.assertLess(process.returncode, 0)

    def test_actual_follow_early_diagnostic_closes_stream_and_joins_owned_child(self):
        code = ('import sys,time;sys.stdout.buffer.write(' + repr(request()) +
                ');sys.stdout.flush();time.sleep(30)')
        process = subprocess.Popen([sys.executable, '-c', code], stdout=subprocess.PIPE,
                                   stderr=subprocess.DEVNULL, stdin=subprocess.DEVNULL)
        client = CAPTURE.Kubectl('/unused', time.monotonic() + 2)
        with patch.object(client, 'start', return_value=process):
            stream = client.follow({'name': 'synthetic'})
            try:
                self.assertEqual(CAPTURE.diagnostic_from_chunks(stream)['kind'], 'REQUEST_FAILURE')
            finally:
                stream.close()
        self.assertIsNotNone(process.returncode)
        self.assertTrue(process.stdout.closed)

    def test_follow_cancel_closes_stream_and_joins_owned_child(self):
        process = subprocess.Popen([sys.executable, '-c', 'import time;time.sleep(30)'],
                                   stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                                   stdin=subprocess.DEVNULL)
        client = CAPTURE.Kubectl('/unused', time.monotonic() + 2)

        def cancelled_chunks(*args):
            yield SENTINEL.encode() + b'\n'
            raise CAPTURE.Failure('CAPTURE_CANCELLED')

        with patch.object(client, 'start', return_value=process), patch.object(
                CAPTURE, 'chunks', cancelled_chunks):
            with self.assertRaisesRegex(CAPTURE.Failure, '^CAPTURE_CANCELLED$'):
                list(client.follow({'name': 'synthetic'}))
        self.assertIsNotNone(process.returncode)
        self.assertTrue(process.stdout.closed)

    def test_follow_observed_nonzero_exit_is_retained_at_deadline(self):
        from unittest.mock import Mock
        process = Mock(stdout=io.BytesIO())
        process.poll.return_value = 7
        process.wait.return_value = 7
        client = CAPTURE.Kubectl('/unused', 0)

        def ended_chunks(*args):
            yield b''
            return 'EOF'

        with patch.object(client, 'start', return_value=process), patch.object(
                CAPTURE, 'chunks', ended_chunks):
            with self.assertRaisesRegex(CAPTURE.Failure, '^KUBECTL_FOLLOW_FAILED$'):
                list(client.follow({'name': 'synthetic'}))
        self.assertTrue(process.stdout.closed)

    def test_stop_process_escalates_to_kill_and_always_closes_stdout(self):
        from unittest.mock import Mock
        process = Mock(stdout=io.BytesIO())
        process.poll.return_value = None
        process.wait.side_effect = [subprocess.TimeoutExpired('synthetic', 1), 0]
        CAPTURE.stop_process(process)
        process.terminate.assert_called_once_with()
        process.kill.assert_called_once_with()
        self.assertEqual([call.kwargs['timeout'] for call in process.wait.call_args_list], [1, 2])
        self.assertTrue(process.stdout.closed)
        process = Mock(stdout=io.BytesIO())
        process.poll.return_value = 0
        process.wait.side_effect = subprocess.TimeoutExpired('synthetic', 2)
        with self.assertRaises(subprocess.TimeoutExpired):
            CAPTURE.stop_process(process)
        self.assertTrue(process.stdout.closed)


if __name__ == '__main__':
    unittest.main()
