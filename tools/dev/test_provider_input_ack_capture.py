"""Герметичные negative/privacy tests; live kubectl никогда не вызывается."""
import argparse
import copy
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import sys
import unittest
from unittest.mock import patch

SOURCE = Path(__file__).with_name('provider-input-ack-capture.py')
SPEC = importlib.util.spec_from_file_location('capture', SOURCE)
CAPTURE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(CAPTURE)


def fixture():
    proof = {field: field + '_fixture' for field in CAPTURE.REF_FIELDS}
    proof.update({field: 'a' * 64 for field in CAPTURE.HASH_FIELDS})
    proof.update({field: 1 for field in CAPTURE.NUMBER_FIELDS})
    proof.update(image_reference='registry.fixture/roles@sha256:' + 'b' * 64,
                 image_manifest_digest='sha256:' + 'b' * 64,
                 assistant_scope='NONE', model='gpt-6.1-sol', reasoning_effort='medium', reasoning_mode='SUPPORTED',
                 task_in_prompt=True, instructions_file_comparison='EQUAL',
                 instructions_file_sha256='a' * 64, inbox_prompt_comparison='EQUAL',
                 inbox_prompt_sha256='a' * 64, tools=[{}] * 38, grants=[], capabilities=[])
    annotations = ['a' * 64] * 5 + [CAPTURE.sha(proof['session_ref'])[:16],
                                   CAPTURE.sha(proof['turn_ref'])[:16], '1', proof['lease_ref']]
    parts = ['kodex-runtime', 'runtime-turn-fixture',
             '12345678-1234-1234-1234-123456789abc', 'Running', 'true', 'turn'] + annotations
    parts += [proof['image_reference'], proof['image_reference'], '0'] * 2
    columns = ('|'.join(parts) + '\n').encode()
    options = argparse.Namespace(run_ref=proof['run_ref'], session_ref=proof['session_ref'],
                                 turn_ref=proof['turn_ref'], attempt=1, pod_name=None,
                                 task_sha256='a' * 64, image_manifest='sha256:' + 'b' * 64,
                                 binary_sha256='c' * 64, timeout_seconds=1, assistant_scope='NONE')
    return proof, columns, options


class CaptureTest(unittest.TestCase):
    def test_system_project_context_requires_explicit_exact_pin(self):
        proof, _, _ = fixture()
        proof['assistant_scope'] = 'SYSTEM'
        record = {'event': CAPTURE.EVENT, 'proof': proof}
        result = CAPTURE.project_ack(record, proof['run_ref'], 'SYSTEM', proof['project_ref'])
        self.assertEqual(result['project_ref'], proof['project_ref'])
        with self.assertRaisesRegex(CAPTURE.Failure, '^ACK_PROJECT_SCOPE_INVALID$'):
            CAPTURE.project_ack(record, proof['run_ref'], 'SYSTEM')
        for actual in ('project_foreign', '', None):
            candidate = dict(proof, project_ref=actual)
            with self.subTest(actual=actual), self.assertRaisesRegex(
                    CAPTURE.Failure, '^EXPECTED_PROJECT_PIN_MISMATCH$'):
                CAPTURE.project_ack({'event': CAPTURE.EVENT, 'proof': candidate},
                                    proof['run_ref'], 'SYSTEM', proof['project_ref'])

    def test_expected_project_pin_invalid_and_never_inferred(self):
        proof, _, _ = fixture()
        proof['assistant_scope'] = 'SYSTEM'
        for expected in ('', 'short', 'PRIVATE project', 'x' * 129, 123, True):
            with self.subTest(expected=expected), self.assertRaisesRegex(
                    CAPTURE.Failure, '^EXPECTED_PROJECT_REFERENCE_INVALID$'):
                CAPTURE.project_ack({'event': CAPTURE.EVENT, 'proof': proof},
                                    proof['run_ref'], 'SYSTEM', expected)
        self.assertIsNone(CAPTURE.project_ack({'event': CAPTURE.EVENT, 'proof': proof},
                                             'run_foreign01', 'SYSTEM', proof['project_ref']))
        with self.assertRaisesRegex(CAPTURE.Failure, '^ACK_SCOPE_INVALID$'):
            CAPTURE.project_ack({'event': CAPTURE.EVENT, 'proof': proof},
                                proof['run_ref'], 'PROJECT', proof['project_ref'])

    def test_none_and_project_optional_pin_adds_only_exact_restriction(self):
        proof, _, _ = fixture()
        for scope in ('NONE', 'PROJECT'):
            proof['assistant_scope'] = scope
            record = {'event': CAPTURE.EVENT, 'proof': proof}
            with self.subTest(scope=scope):
                self.assertEqual(CAPTURE.project_ack(record, proof['run_ref'], scope),
                                 CAPTURE.project_ack(record, proof['run_ref'], scope, proof['project_ref']))
                with self.assertRaisesRegex(CAPTURE.Failure, '^EXPECTED_PROJECT_PIN_MISMATCH$'):
                    CAPTURE.project_ack(record, proof['run_ref'], scope, 'project_foreign')

    def test_system_project_capture_retains_privacy_checkpoint_binary_and_rejoin(self):
        proof, columns, options = fixture()
        proof['assistant_scope'], options.assistant_scope = 'SYSTEM', 'SYSTEM'
        options.expected_project_ref = proof['project_ref']
        proof.update(input={'secret': 'PRIVATE_INPUT'}, tools=[{'command': 'PRIVATE_COMMAND'}])
        logs = json.dumps({'event': CAPTURE.EVENT, 'proof': proof, 'body': 'PRIVATE_BODY'}).encode()
        calls, checkpoints = [], []

        def read(args):
            calls.append(args)
            if args[0] == 'get':
                return columns
            if args[0] == 'logs':
                return logs
            return ('c' * 64 + '  ' + CAPTURE.BINARY + '\n').encode()

        result = CAPTURE.capture(options, read=read, on_ack=checkpoints.append)
        self.assertEqual(result['status'], 'CAPTURED')
        self.assertEqual(result['proof']['project_ref'], options.expected_project_ref)
        self.assertEqual(result['proof']['assistant_scope'], 'SYSTEM')
        self.assertEqual(result['binary']['expected_comparison'], 'EQUAL')
        self.assertEqual([call[0] for call in calls], ['get', 'logs', 'exec', 'get'])
        self.assertEqual(checkpoints[0]['proof'], result['proof'])
        self.assertEqual(checkpoints[0]['status'], 'ACK_CAPTURED_POD_REJOIN_PENDING')
        self.assertNotIn('PRIVATE', json.dumps([result, checkpoints]))
        options.expected_project_ref = 'project_foreign'
        calls.clear()
        checkpoints.clear()
        with self.assertRaisesRegex(CAPTURE.Failure, '^EXPECTED_PROJECT_PIN_MISMATCH$'):
            CAPTURE.capture(options, read=read, on_ack=checkpoints.append)
        self.assertEqual([call[0] for call in calls], ['get', 'logs'])
        self.assertEqual(checkpoints, [])

    def test_invalid_project_capture_pin_fails_before_any_read(self):
        _, _, options = fixture()
        options.expected_project_ref = 'PRIVATE invalid'
        with self.assertRaisesRegex(CAPTURE.Failure, '^EXPECTED_PROJECT_REFERENCE_INVALID$'):
            CAPTURE.capture(options, read=lambda args: self.fail('invalid pin reached kubectl'))

    def test_system_project_pin_keeps_all_remaining_capture_guards(self):
        for changed in ('task', 'image', 'revision', 'tuple', 'false_equal', 'binary', 'rejoin'):
            proof, columns, options = fixture()
            proof['assistant_scope'], options.assistant_scope = 'SYSTEM', 'SYSTEM'
            options.expected_project_ref = proof['project_ref']
            if changed == 'task':
                options.task_sha256 = 'd' * 64
            elif changed == 'image':
                proof['image_reference'] = 'registry.fixture/foreign@sha256:' + 'b' * 64
            elif changed == 'revision':
                proof['runtime_revision_digest'] = 'd' * 64
            elif changed == 'tuple':
                proof['turn_ref'] = 'turn_foreign01'
            elif changed == 'false_equal':
                proof['inbox_prompt_sha256'] = 'd' * 64
            reads = 0

            def read(args):
                nonlocal reads
                if args[0] == 'get':
                    reads += 1
                    if changed == 'rejoin' and reads == 2:
                        return columns.replace(b'123456789abc', b'123456789abd')
                    return columns
                if args[0] == 'logs':
                    return json.dumps({'event': CAPTURE.EVENT, 'proof': proof}).encode()
                digest = 'd' if changed == 'binary' else 'c'
                return (digest * 64 + '  ' + CAPTURE.BINARY + '\n').encode()

            with self.subTest(changed=changed), self.assertRaises(CAPTURE.Failure):
                CAPTURE.capture(options, read=read)

    def test_cli_passes_exact_project_pin_without_changing_default(self):
        for scope, expected in (('SYSTEM', None), ('SYSTEM', 'project_fixture'),
                                ('NONE', 'project_fixture'), ('PROJECT', 'project_fixture')):
            args = ['capture', '--run-ref', 'run_fixture', '--assistant-scope', scope]
            if expected is not None:
                args += ['--expected-project-ref', expected]
            with self.subTest(scope=scope, expected=expected), patch.object(sys, 'argv', args), \
                    patch.object(CAPTURE, 'capture', return_value={'status': 'CAPTURED'}) as captured, \
                    patch('sys.stdout', io.StringIO()):
                CAPTURE.main()
                self.assertEqual(captured.call_args.args[0].expected_project_ref, expected)
                self.assertEqual(captured.call_args.args[0].assistant_scope, scope)

    def test_none_default_and_explicit_project_require_current_source_scope(self):
        proof, _, _ = fixture()
        record = {'event': CAPTURE.EVENT, 'proof': proof}
        self.assertEqual(CAPTURE.project_ack(record, proof['run_ref'])['assistant_scope'], 'NONE')
        proof['assistant_scope'] = 'PROJECT'
        self.assertEqual(CAPTURE.project_ack(record, proof['run_ref'], 'PROJECT')['project_ref'], proof['project_ref'])
        with self.assertRaises(CAPTURE.Failure):
            CAPTURE.project_ack(record, proof['run_ref'])

    def test_explicit_system_without_project_retains_exact_pod_and_binary_binding(self):
        proof, columns, options = fixture()
        proof['assistant_scope'], options.assistant_scope = 'SYSTEM', 'SYSTEM'
        del proof['project_ref']
        logs = json.dumps({'event': CAPTURE.EVENT, 'proof': proof}).encode()

        def read(args):
            if args[0] == 'get':
                return columns
            if args[0] == 'logs':
                return logs
            return ('c' * 64 + '  ' + CAPTURE.BINARY + '\n').encode()

        result = CAPTURE.capture(options, read=read)
        self.assertEqual(result['status'], 'CAPTURED')
        self.assertEqual(result['proof']['assistant_scope'], 'SYSTEM')
        self.assertNotIn('project_ref', result['proof'])
        self.assertEqual(result['proof']['run_ref'], options.run_ref)
        self.assertEqual(result['binary']['expected_comparison'], 'EQUAL')
        self.assertEqual(result['pod']['uid'], '12345678-1234-1234-1234-123456789abc')

    def test_scope_missing_project_and_cross_scope_fail_closed(self):
        proof, _, _ = fixture()
        for selected, scope, project in (
                ('NONE', 'NONE', None), ('NONE', 'NONE', ''),
                ('NONE', 'PROJECT', 'project_fixture'), ('PROJECT', 'NONE', 'project_fixture'),
                ('NONE', 'SYSTEM', None), ('SYSTEM', 'NONE', None),
                ('PROJECT', 'PROJECT', None), ('PROJECT', 'PROJECT', ''),
                ('PROJECT', 'SYSTEM', None), ('SYSTEM', 'PROJECT', None),
                ('SYSTEM', 'SYSTEM', 'project_foreign'), ('SYSTEM', 'SYSTEM', None),
                ('SYSTEM', 'UNKNOWN', None), ('UNKNOWN', 'SYSTEM', None),
                ('SYSTEM', None, None)):
            candidate = dict(proof)
            if scope is None:
                del candidate['assistant_scope']
            else:
                candidate['assistant_scope'] = scope
            if project is None:
                candidate.pop('project_ref')
            else:
                candidate['project_ref'] = project
            record = {'event': CAPTURE.EVENT, 'proof': candidate}
            with self.subTest(selected=selected, scope=scope, project=project):
                if selected == scope == 'SYSTEM' and project is None:
                    self.assertEqual(CAPTURE.project_ack(record, proof['run_ref'], selected)['assistant_scope'], 'SYSTEM')
                else:
                    with self.assertRaises(CAPTURE.Failure):
                        CAPTURE.project_ack(record, proof['run_ref'], selected)

    def test_system_does_not_relax_other_pins_or_requested_run(self):
        proof, _, _ = fixture()
        proof['assistant_scope'] = 'SYSTEM'
        del proof['project_ref']
        for field, bad in [('lease_ref', ''), ('image_recipe_ref', ''), ('runtime_revision_digest', ''), ('attempt', 0)]:
            candidate = dict(proof)
            candidate[field] = bad
            with self.subTest(field=field), self.assertRaises(CAPTURE.Failure):
                CAPTURE.ack_from_logs(json.dumps({'event': CAPTURE.EVENT, 'proof': candidate}).encode(), proof['run_ref'], 'SYSTEM')
        self.assertIsNone(CAPTURE.project_ack({'event': CAPTURE.EVENT, 'proof': proof}, 'run_foreign01', 'SYSTEM'))
        with self.assertRaises(CAPTURE.Failure):
            CAPTURE.project_ack({'event': CAPTURE.EVENT, 'proof': proof}, proof['run_ref'])

    def test_closed_projection_privacy_and_exact_equal(self):
        proof, columns, options = fixture()
        record = {'event': CAPTURE.EVENT, 'proof': proof, 'message': 'PRIVATE_COOKIE_SENTINEL'}
        proof.update(input={'password': 'PRIVATE_INPUT_SENTINEL'},
                     provider_credential='PRIVATE_CREDENTIAL_SENTINEL',
                     tools=[{'command': 'PRIVATE_COMMAND_SENTINEL'}])
        calls = []

        def read(args):
            calls.append(args)
            if args[0] == 'get':
                return columns
            if args[0] == 'logs':
                return (b'PRIVATE_RAW_LOG_SENTINEL\n' + json.dumps(record).encode() + b'\n' +
                        b'{"event":"OTHER_EVENT","input":"PRIVATE_OTHER_SENTINEL"}\n')
            if args[0] == 'exec':
                return ('c' * 64 + '  ' + CAPTURE.BINARY + '\n').encode()
            self.fail('unexpected command')

        result = CAPTURE.capture(options, read=read)
        serialized = json.dumps(result)
        self.assertNotIn('PRIVATE_', serialized)
        self.assertEqual(result['checks']['task_expected_comparison'], 'EQUAL')
        self.assertEqual(result['checks']['provider_inbox_comparison'], 'EQUAL')
        self.assertEqual(result['binary']['expected_comparison'], 'EQUAL')
        self.assertEqual(result['proof']['tools_count'], 1)
        self.assertEqual(result['pod']['uid'], '12345678-1234-1234-1234-123456789abc')
        self.assertEqual(calls[-1][0], 'get')
        self.assertEqual(next(call for call in calls if call[0] == 'exec')[-3:],
                         ['--', 'sha256sum', CAPTURE.BINARY])
        self.assertNotIn('.env', CAPTURE.POD_JSONPATH)
        self.assertNotIn('.volumes', CAPTURE.POD_JSONPATH)

    def test_unknown_events_not_decoded_and_duplicate_keys_rejected(self):
        proof, _, _ = fixture()
        self.assertIsNone(CAPTURE.ack_from_logs(b'{PRIVATE_MALFORMED_OTHER}\n', proof['run_ref']))
        raw = json.dumps({'event': CAPTURE.EVENT, 'proof': proof})[:-1] + ',"event":"OTHER"}'
        with self.assertRaises(CAPTURE.Failure):
            CAPTURE.ack_from_logs(raw.encode(), proof['run_ref'])

    def test_false_equal_and_malformed_metadata_rejected(self):
        proof, columns, _ = fixture()
        for field, bad in [('inbox_prompt_sha256', 'd' * 64), ('run_ref', 'PRIVATE bad'),
                           ('attempt', True), ('model', 'PRIVATE password URL'),
                           ('image_reference', 'https://user:password@host/')]:
            candidate = copy.deepcopy(proof)
            candidate[field] = bad
            with self.subTest(field=field), self.assertRaises(CAPTURE.Failure):
                CAPTURE.project_ack({'event': CAPTURE.EVENT, 'proof': candidate}, candidate['run_ref'])
        pod = CAPTURE.pods_from_columns(columns)[0]
        for field in ('revision-digest', 'session-hash', 'attempt', 'lease-ref'):
            candidate = copy.deepcopy(pod)
            candidate['annotations'][field] = 'OTHER'
            with self.subTest(field=field), self.assertRaises(CAPTURE.Failure):
                CAPTURE.bind_pod(candidate, proof)

    def test_different_and_unavailable_not_promoted_to_equal(self):
        proof, _, _ = fixture()
        for comparison in ('DIFFERENT', 'UNAVAILABLE'):
            proof['inbox_prompt_comparison'] = comparison
            proof['inbox_prompt_sha256'] = 'd' * 64 if comparison == 'DIFFERENT' else ''
            result = CAPTURE.project_ack({'event': CAPTURE.EVENT, 'proof': proof}, proof['run_ref'])
            self.assertEqual(result['inbox_prompt_comparison'], comparison)

    def test_multiple_turns_and_bounded_log_rejected(self):
        proof, _, _ = fixture()
        other = dict(proof, turn_ref='turn_other_fixture')
        raw = b'\n'.join(json.dumps({'event': CAPTURE.EVENT, 'proof': item}).encode()
                         for item in (proof, other))
        with self.assertRaises(CAPTURE.Failure):
            CAPTURE.ack_from_logs(raw, proof['run_ref'])
        with self.assertRaises(CAPTURE.Failure):
            CAPTURE.ack_from_logs(b'a' * ((512 << 10) + 1), proof['run_ref'])

    def test_uid_replacement_or_cleanup_and_expected_task_fail_closed(self):
        proof, columns, options = fixture()
        logs = json.dumps({'event': CAPTURE.EVENT, 'proof': proof}).encode()
        calls = 0

        def read(args):
            nonlocal calls
            if args[0] == 'get':
                calls += 1
                return columns if calls == 1 else columns.replace(b'123456789abc', b'123456789abd')
            if args[0] == 'logs':
                return logs
            return ('c' * 64 + '  ' + CAPTURE.BINARY).encode()

        checkpoints = []
        with self.assertRaises(CAPTURE.Failure):
            CAPTURE.capture(options, read=read, on_ack=checkpoints.append)
        self.assertEqual(len(checkpoints), 1)
        self.assertEqual(checkpoints[0]['status'], 'ACK_CAPTURED_POD_REJOIN_PENDING')
        self.assertNotIn('binary', checkpoints[0])
        calls = 0
        options.task_sha256 = 'd' * 64
        with self.assertRaises(CAPTURE.Failure):
            CAPTURE.capture(options, read=read)

    def test_scrubbed_kubectl_and_private_error(self):
        result = subprocess.CompletedProcess([], 1, b'PRIVATE_STDOUT', b'PRIVATE_TOKEN')
        with patch.object(CAPTURE.subprocess, 'run', return_value=result) as run:
            with self.assertRaisesRegex(CAPTURE.Failure, '^KUBECTL_READ_FAILED$'):
                CAPTURE.kube(['get', 'pods'])
            args, kwargs = run.call_args
            self.assertEqual(args[0][:5], ['kubectl', '--context', 'k3d-kodex', '--request-timeout=5s', 'get'])
            self.assertEqual(set(kwargs['env']), {'PATH', 'KUBECONFIG'})
            self.assertEqual(kwargs['env']['KUBECONFIG'], '/home/s/.kube/config')

    def test_invalid_cli_never_echoes_argument_values(self):
        for args in (['--run-ref', 'PRIVATE PASSWORD'],
                     ['--run-ref', 'run_fixture', '--unknown', 'PRIVATE_TOKEN'],
                     ['--run-ref', 'run_fixture', '--assistant-scope', 'PRIVATE_SCOPE'],
                     ['--run-ref', 'run_fixture', '--expected-project-ref', 'PRIVATE project'],
                     ['--run-ref', 'run_fixture', '--expected-project-ref', ''],
                     ['--run-ref', 'run_fixture', '--expected-project-ref', 'short'],
                     ['--run-ref', 'run_fixture', '--expected-project-ref', 'x' * 129],
                     ['--run-ref', 'run_fixture', '--expected-project-ref']):
            result = subprocess.run([sys.executable, str(SOURCE), *args],
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False)
            self.assertEqual(result.returncode, 1)
            self.assertNotIn(b'PRIVATE', result.stdout + result.stderr)
            self.assertIn(b'PROVIDER_ACK_CAPTURE_FAILED', result.stderr)


if __name__ == '__main__':
    unittest.main()
