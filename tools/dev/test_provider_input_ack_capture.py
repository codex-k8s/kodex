"""Герметичные negative/privacy tests; live kubectl никогда не вызывается."""
import argparse
import copy
import importlib.util
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
                 model='gpt-6.1-sol', reasoning_effort='medium', reasoning_mode='SUPPORTED',
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
                                 binary_sha256='c' * 64, timeout_seconds=1)
    return proof, columns, options


class CaptureTest(unittest.TestCase):
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
                     ['--run-ref', 'run_fixture', '--unknown', 'PRIVATE_TOKEN']):
            result = subprocess.run([sys.executable, str(SOURCE), *args],
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False)
            self.assertEqual(result.returncode, 1)
            self.assertNotIn(b'PRIVATE', result.stdout + result.stderr)
            self.assertIn(b'PROVIDER_ACK_CAPTURE_FAILED', result.stderr)


if __name__ == '__main__':
    unittest.main()
