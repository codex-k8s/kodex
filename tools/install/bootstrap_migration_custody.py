"""Связывает настоящие bootstrap effects с read-only свидетельством мигрированной установки.

Результат сохраняет происхождение наблюдений, но не является bootstrap completion,
разрешением apply или основанием повторять исторические Jobs. Архивная декларация,
текущая декларация ConfigMap и фактические schema heads остаются разными полями.
"""
import argparse
import copy
from datetime import datetime, timezone
import json
from pathlib import Path
import re
import subprocess

import yaml
import bootstrap_completion as completion
import bootstrap_upgrade as upgrade
from deployment_profile import require, Rejected


SCHEMA = 'kodex.bootstrap-migration-custody/v2'
WITNESS_SCHEMA = 'kodex.component-migration-adoption-readback/v2'
HEADS = {'control-plane', 'internal-rpc-authority', 'email-bridge'}
WITNESS_FIELDS = {'schema', 'observedAt', 'context', 'clusterUID', 'namespaceUID',
                  'installationUID', 'installationSHA256', 'declaredSourceSHA',
                  'declaredReleaseLockSHA256', 'declaredSchemaHeads', 'migrationSourceSHA',
                  'schemaHeads', 'archiveTerminalSHA256', 'archiveExecutionSHA256',
                  'archiveJobUID', 'archivePodUID', 'authorityBaselineSHA256', 'authorityIdentitySHA256',
                  'schemaCatalogSHA256', 'storageSHA256', 'dossierSHA256'}
_PRODUCED = object()


def canonical_digest(value):
    """Повторяет Node fingerprint для строковых данных installation ConfigMap."""
    return completion.digest(json.dumps(value, sort_keys=True, separators=(',', ':'),
                                        ensure_ascii=False, allow_nan=False).encode())


def schema_heads(value):
    """Не принимает bool, неполный набор сервисов и неизвестные имена владельцев."""
    require(isinstance(value, dict) and set(value) == HEADS and
            all(type(head) is int and head > 0 for head in value.values()),
            'MIGRATION_CUSTODY_SCHEMA_HEADS_REJECTED')
    return copy.deepcopy(value)


def installation_resource(resources):
    """Извлекает единственную принадлежащую установщику декларацию в точном namespace."""
    rows = [row for row in resources if row.get('kind') == 'ConfigMap' and
            row.get('metadata', {}).get('name') == 'kodex-installation-profile']
    require(len(rows) == 1, 'MIGRATION_CUSTODY_INSTALLATION_REQUIRED')
    row = rows[0]
    metadata = row.get('metadata', {})
    labels = metadata.get('labels', {})
    require(row.get('apiVersion') == 'v1' and metadata.get('namespace') == 'kodex-system' and
            labels.get('app.kubernetes.io/part-of') == 'kodex' and
            labels.get('app.kubernetes.io/managed-by') == 'kodex-install' and
            not metadata.get('deletionTimestamp'), 'MIGRATION_CUSTODY_INSTALLATION_OWNERSHIP_REQUIRED')
    declaration(row.get('data'))
    return row


def declaration(data):
    """Проверяет декларативные pins без объявления их текущим каталогом PostgreSQL."""
    require(isinstance(data, dict) and all(isinstance(k, str) and isinstance(v, str) for k, v in data.items()) and
            completion.hex_value(data.get('source.sha'), 40) and
            completion.hex_value(data.get('release-lock.sha256')),
            'MIGRATION_CUSTODY_DECLARATION_REJECTED')
    heads = schema_heads(json.loads(data.get('schema-heads.json', ''), object_pairs_hook=completion.unique_object))
    return {'source_sha': data['source.sha'], 'release_lock_sha256': data['release-lock.sha256'],
            'schema_heads': heads}


def live_installation(state, archived):
    """Читает текущий UID и полный data hash, сохраняя самостоятельность старой декларации."""
    value = state.get(archived)
    require(isinstance(value, dict), 'MIGRATION_CUSTODY_LIVE_INSTALLATION_REQUIRED')
    installation_resource([value])
    metadata = value['metadata']
    require(completion.uuid_value(metadata.get('uid')) and
            isinstance(metadata.get('resourceVersion'), str) and
            re.fullmatch(r'[0-9]+', metadata['resourceVersion']),
            'MIGRATION_CUSTODY_INSTALLATION_IDENTITY_REQUIRED')
    return {'uid': metadata['uid'], 'resource_version': metadata['resourceVersion'],
            'data_sha256': canonical_digest(value['data']), 'declaration': declaration(value['data'])}


class _VerifiedWitness:
    """Внутренний результат точного producer port; JSON-файл не заменяет вызов producer."""

    def __init__(self, raw, marker):
        require(marker is _PRODUCED, 'MIGRATION_CUSTODY_PRODUCER_REQUIRED')
        self._raw = bytes(raw)
        self._marker = marker

    def value(self):
        require(self._marker is _PRODUCED, 'MIGRATION_CUSTODY_PRODUCER_REQUIRED')
        return json.loads(self._raw, object_pairs_hook=completion.unique_object)


class MigrationWitnessProducer:
    """Вызывает закреплённый repo-owned reader; executor подменяется только локальной оснасткой.

    Путь программы и список аргументов закрыты. Не принимаются готовый stdout,
    пользовательский JS-модуль, shell command или сериализованный owner proof.
    """

    def __init__(self, dossier_path, dossier_sha256, *, executor=None):
        self.dossier_path = Path(dossier_path)
        self.dossier_sha256 = dossier_sha256
        self.executor = subprocess.run if executor is None else executor

    def read(self, state):
        """Запускает только чтение и проверяет ограниченный, свежий, закрытый ответ."""
        return self._read(state, [], WITNESS_SCHEMA, WITNESS_FIELDS)

    def read_continuity(self, state, schema, fields):
        """Читает положительную сохранность архивных эффектов во время rollover serving specs."""
        return self._read(state, ['--continuity'], schema, fields)

    def _read(self, state, modes, schema, fields):
        """Проводит оба закрытых вызова через один transport, pin и проверку свежести."""
        require(self.dossier_path.is_absolute() and completion.hex_value(self.dossier_sha256),
                'MIGRATION_CUSTODY_DOSSIER_PIN_REQUIRED')
        require(completion.digest(completion.private_bytes(self.dossier_path)) == self.dossier_sha256,
                'MIGRATION_CUSTODY_DOSSIER_CHANGED')
        kubeconfig = getattr(state, 'kubeconfig', None)
        require(kubeconfig is not None and Path(kubeconfig).is_absolute() and
                not any(c in str(kubeconfig) for c in ('\0', '\r', '\n')) and
                isinstance(state.context, str) and re.fullmatch(r'[A-Za-z0-9_.:@/-]{1,160}', state.context),
                'MIGRATION_CUSTODY_EXPLICIT_TARGET_REQUIRED')
        program = Path(__file__).resolve().parents[1] / 'release' / 'component-migration-adoption.mjs'
        try:
            result = self.executor(['node', str(program), *modes, state.context, str(kubeconfig),
                                    str(self.dossier_path), self.dossier_sha256],
                                   capture_output=True, timeout=110, check=False)
        except (OSError, subprocess.SubprocessError):
            raise Rejected('MIGRATION_CUSTODY_PRODUCER_REJECTED') from None
        require(result.returncode == 0 and isinstance(result.stdout, bytes) and
                0 < len(result.stdout) <= 65536, 'MIGRATION_CUSTODY_PRODUCER_REJECTED')
        value = json.loads(result.stdout, object_pairs_hook=completion.unique_object)
        require(isinstance(value, dict) and set(value) == fields and value['schema'] == schema and
                value['context'] == state.context and value['dossierSHA256'] == self.dossier_sha256,
                'MIGRATION_CUSTODY_WITNESS_REJECTED')
        for name in fields:
            if name.endswith('SHA256') or name.endswith('SHA'):
                require(completion.hex_value(value[name], 40 if name.endswith('SHA') else 64),
                        'MIGRATION_CUSTODY_WITNESS_PIN_REJECTED')
            elif name.endswith('UID'):
                require(completion.uuid_value(value[name]), 'MIGRATION_CUSTODY_WITNESS_IDENTITY_REJECTED')
            elif name.endswith('ResourceVersion'):
                require(isinstance(value[name], str) and re.fullmatch(r'[0-9]+', value[name]),
                        'MIGRATION_CUSTODY_WITNESS_VERSION_REJECTED')
        schema_heads(value['declaredSchemaHeads'])
        schema_heads(value['schemaHeads'])
        observed = value['observedAt']
        require(isinstance(observed, str) and
                re.fullmatch(r'\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,6})?Z', observed),
                'MIGRATION_CUSTODY_WITNESS_TIME_REJECTED')
        age = (datetime.now(timezone.utc) - datetime.fromisoformat(observed.replace('Z', '+00:00'))).total_seconds()
        require(-5 <= age <= 30, 'MIGRATION_CUSTODY_WITNESS_STALE')
        require(completion.digest(completion.private_bytes(self.dossier_path)) == self.dossier_sha256,
                'MIGRATION_CUSTODY_DOSSIER_CHANGED')
        return _VerifiedWitness(result.stdout, _PRODUCED)


def load_original(previous_receipt_path, previous_receipt_sha256, previous_render_path, previous_render_sha256):
    """Закрывает все исторические pins до discovery или другого обращения к кластеру."""
    previous = completion.load(previous_receipt_path, previous_receipt_sha256)
    require(previous['origin'] == 'LIVE_CAPTURE', 'MIGRATION_CUSTODY_ORIGINAL_LIVE_CAPTURE_REQUIRED')
    require(completion.hex_value(previous_render_sha256), 'MIGRATION_CUSTODY_RENDER_PIN_REQUIRED')
    raw = completion.private_bytes(previous_render_path, 8 << 20)
    require(completion.digest(raw) == previous_render_sha256, 'MIGRATION_CUSTODY_ARCHIVED_RENDER_CHANGED')
    resources = upgrade.decode_render(raw)
    archived = installation_resource(resources)
    completion.desired_jobs(resources)
    return previous, resources, archived


def capture(state, *, previous_receipt_path, previous_receipt_sha256,
            previous_render_path, previous_render_sha256, witness_producer):
    """Сохраняет пять подлинных effects и независимую live сверку без выдачи допуска apply.

    Jobs сверяются только с архивным render. Его старые schema heads не передаются
    в verify_schemas: фактические heads доказывает отдельный JS reader. TTL
    допускается ровно по прежним semantics verify_jobs и исходному LIVE_CAPTURE.
    """
    require(type(witness_producer) is MigrationWitnessProducer, 'MIGRATION_CUSTODY_PRODUCER_REQUIRED')
    previous, resources, archived = load_original(previous_receipt_path, previous_receipt_sha256,
                                                previous_render_path, previous_render_sha256)
    completion.verify_jobs(state, resources, previous)
    before = live_installation(state, archived)
    produced = witness_producer.read(state)
    require(type(produced) is _VerifiedWitness, 'MIGRATION_CUSTODY_PRODUCER_REQUIRED')
    witness = produced.value()
    require(witness['clusterUID'] == previous['cluster_uid'] and witness['namespaceUID'] == previous['namespace_uid'] and
            witness['installationUID'] == before['uid'] and witness['installationSHA256'] == before['data_sha256'] and
            witness['declaredSourceSHA'] == before['declaration']['source_sha'] and
            witness['declaredReleaseLockSHA256'] == before['declaration']['release_lock_sha256'] and
            witness['declaredSchemaHeads'] == before['declaration']['schema_heads'],
            'MIGRATION_CUSTODY_LIVE_BINDING_MISMATCH')
    completion.verify_jobs(state, resources, previous)
    require(live_installation(state, archived) == before, 'MIGRATION_CUSTODY_INSTALLATION_CHANGED')
    return {'schema': SCHEMA, 'origin': 'VERIFIED_MIGRATED_INSTALLATION_READ_ONLY',
            'created_at': datetime.now(timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ'),
            'context': state.context, 'cluster_uid': previous['cluster_uid'], 'namespace_uid': previous['namespace_uid'],
            'installation_uid': before['uid'], 'installation_sha256': before['data_sha256'],
            'previous_receipt_sha256': previous_receipt_sha256, 'previous_render_sha256': previous_render_sha256,
            'archived_declaration': declaration(archived['data']), 'live_declaration': before['declaration'],
            'migration_source_sha': witness['migrationSourceSHA'], 'actual_schema_heads': copy.deepcopy(witness['schemaHeads']),
            'jobs': copy.deepcopy(previous['jobs']), 'migration_witness': witness,
            'migration_witness_sha256': completion.digest(produced._raw),
            'effects_replayed': False, 'apply_granted': False}


def verify_observation(prior, fresh):
    """Сверяет lineage и устойчивые pins после нового авторитетного чтения."""
    def stable(value):
        witness = {key: pin for key, pin in value['migration_witness'].items()
                   if key not in ('observedAt', 'authorityBaselineSHA256')}
        result = {key: pin for key, pin in value.items()
                  if key not in ('created_at', 'migration_witness_sha256')}
        result['migration_witness'] = witness
        return result

    require(stable(prior) == stable(fresh), 'MIGRATION_CUSTODY_OBSERVATION_CHANGED')


def main():
    """Публичный capture читает только bootstrap identities и отдельное свидетельство миграции."""
    from portable_state import State
    parser = argparse.ArgumentParser(description='Capture read-only custody of authentic bootstrap effects after migration.')
    parser.add_argument('--context', required=True)
    parser.add_argument('--kubeconfig', type=Path, required=True)
    for flag in ('previous-render', 'previous-receipt', 'dossier'):
        parser.add_argument('--' + flag, type=Path, required=True)
        parser.add_argument('--' + flag + '-sha256', required=True)
    parser.add_argument('--expected-custody', type=Path)
    parser.add_argument('--expected-custody-sha256')
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    try:
        # Discovery ограничивается проверенным архивом; другие старые API и schema не требуются.
        _, resources, archived = load_original(args.previous_receipt, args.previous_receipt_sha256,
                                              args.previous_render, args.previous_render_sha256)
        require(re.fullmatch(r'[A-Za-z0-9_.:@/-]{1,160}', args.context) and args.dossier.is_absolute() and
                completion.hex_value(args.dossier_sha256), 'MIGRATION_CUSTODY_EXPLICIT_TARGET_REQUIRED')
        require(completion.digest(completion.private_bytes(args.dossier)) == args.dossier_sha256,
                'MIGRATION_CUSTODY_DOSSIER_CHANGED')
        require((args.expected_custody is None) == (args.expected_custody_sha256 is None),
                'MIGRATION_CUSTODY_OBSERVATION_PIN_REQUIRED')
        prior = None
        if args.expected_custody is not None:
            prior = completion.pinned_json(args.expected_custody, args.expected_custody_sha256)
            require(isinstance(prior, dict) and prior.get('schema') == SCHEMA and
                    prior.get('previous_receipt_sha256') == args.previous_receipt_sha256 and
                    prior.get('previous_render_sha256') == args.previous_render_sha256 and
                    prior.get('context') == args.context and
                    prior['migration_witness']['dossierSHA256'] == args.dossier_sha256 and
                    prior.get('effects_replayed') is False and prior.get('apply_granted') is False,
                    'MIGRATION_CUSTODY_OBSERVATION_PIN_MISMATCH')
        state = State(args.context, args.kubeconfig)
        state.discover([archived, *completion.desired_jobs(resources).values()])
        result = capture(state, previous_receipt_path=args.previous_receipt,
                         previous_receipt_sha256=args.previous_receipt_sha256,
                         previous_render_path=args.previous_render,
                         previous_render_sha256=args.previous_render_sha256,
                         witness_producer=MigrationWitnessProducer(args.dossier, args.dossier_sha256))
        if prior is not None:
            verify_observation(prior, result)
        raw = (json.dumps(result, sort_keys=True, ensure_ascii=False) + '\n').encode()
        upgrade.write_new(args.output, raw)
        require(completion.private_bytes(args.output) == raw, 'MIGRATION_CUSTODY_OUTPUT_READBACK_REJECTED')
        print(json.dumps({'status': 'MIGRATION_CUSTODY_RECORDED_READ_ONLY', 'sha256': completion.digest(raw),
                          'jobs': len(completion.JOBS), 'effects_replayed': False, 'apply_granted': False}))
    except (Rejected, ValueError, TypeError, KeyError, OSError, subprocess.SubprocessError, yaml.YAMLError):
        raise SystemExit('Bootstrap migration custody rejected; no bootstrap effects executed') from None


if __name__ == '__main__':
    main()
