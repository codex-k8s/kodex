"""Пустой bootstrap создаётся один раз, опубликованные почтовые credentials не заменяются."""
import copy
import json
from types import SimpleNamespace
import unittest
from unittest.mock import patch
import email_projection as projection
from deployment_profile import Rejected


def fixture():
    return [dict(kind='Deployment', metadata=dict(name='email-bridge')),
            dict(apiVersion='v1', kind='Secret', metadata=dict(name=projection.NAME, namespace='kodex-system', labels={'app.kubernetes.io/managed-by': 'control-plane'}), type='Opaque', stringData={'mailboxes.json': json.dumps(projection.EMPTY)})]


def published():
    value = copy.deepcopy(fixture()[1]);value.pop('stringData');value['data'] = {'mailboxes.json': 'c3ludGhldGlj'};value['metadata']['uid'] = 'synthetic-uid'
    return value


class EmailBootstrap(unittest.TestCase):
    def test_existing_publication_is_checked_without_write_or_payload_replacement(self):
        response = SimpleNamespace(returncode=0, stdout=json.dumps(published()).encode())
        with patch.object(projection.subprocess, 'run', return_value=response) as run:
            projection.reconcile(fixture(), 'synthetic-context')
        self.assertEqual(run.call_count, 1)
        self.assertIn('get', run.call_args.args[0])

    def test_missing_seed_create_and_lost_response_reconcile_without_second_write(self):
        for code in [0, 1]:
            responses = [SimpleNamespace(returncode=0, stdout=b''), SimpleNamespace(returncode=code, stdout=b''), SimpleNamespace(returncode=0, stdout=json.dumps(published()).encode())]
            with patch.object(projection.subprocess, 'run', side_effect=responses) as run:
                projection.reconcile(fixture(), 'synthetic-context')
            writes = [call for call in run.call_args_list if 'create' in call.args[0]]
            self.assertEqual(len(writes), 1)
            self.assertEqual(json.loads(writes[0].kwargs['input']), fixture()[1])
            self.assertNotIn('apply', writes[0].args[0])

    def test_preflight_is_server_dry_run_only_and_unowned_secret_rejects(self):
        responses = [SimpleNamespace(returncode=0, stdout=b''), SimpleNamespace(returncode=0, stdout=b'')]
        with patch.object(projection.subprocess, 'run', side_effect=responses) as run:
            projection.reconcile(fixture(), 'synthetic-context', True)
        self.assertEqual(run.call_count, 2)
        self.assertIn('--dry-run=server', run.call_args.args[0])
        value = published();value['metadata']['labels'] = {}
        with patch.object(projection.subprocess, 'run', return_value=SimpleNamespace(returncode=0, stdout=json.dumps(value).encode())) as run, self.assertRaises(Rejected):
            projection.reconcile(fixture(), 'synthetic-context')
        self.assertEqual(run.call_count, 1)

    def test_seed_requires_exact_empty_bootstrap_and_never_real_mailboxes(self):
        self.assertEqual(projection.seed(fixture()), fixture()[1])
        for field, value in [('mailboxes', [dict(id='synthetic')]), ('revision', 2), ('source', 'other')]:
            resources = fixture();body = copy.deepcopy(projection.EMPTY);body[field] = value
            resources[1]['stringData']['mailboxes.json'] = json.dumps(body)
            with self.assertRaises(Rejected): projection.seed(resources)


if __name__ == '__main__': unittest.main()
