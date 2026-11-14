#!/usr/bin/env python3
"""Генерирует exact044→045 catalog proofs и canonical ACL-only restore."""
import argparse
import hashlib
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
RELEASE = ROOT / 'tools/release'
MANIFEST = RELEASE / 'sentry-observability-schema-manifest.json'
PREDECESSOR = RELEASE / 'github-repository-scope-schema-manifest.json'
INITIALIZER = ROOT / 'deploy/k8s/base/platform-state/postgresql/reconcile-runtime-credentials.sh'
MIGRATION = ROOT / 'services/internal/control-plane/cmd/cli/migrations/20261114130000_sentry_personal_credentials.sql'
MODES = ['normal', 'backup', 'table-only-backup']
parser = argparse.ArgumentParser()
parser.add_argument('--capture-directory', type=Path)
parser.add_argument('--prepare-capture', action='store_true')
parser.add_argument('--check', action='store_true')
args = parser.parse_args()
if sum([bool(args.capture_directory), args.prepare_capture, args.check]) > 1:
    parser.error('capture, prepare-capture and check modes are mutually exclusive')
source = INITIALIZER.read_text()
start = '-- BEGIN CANONICAL CONTROL PLANE BACKUP ACL\n'
end = '-- END CANONICAL CONTROL PLANE BACKUP ACL'
if source.count(start) != 1 or source.count(end) != 1:
    parser.error('canonical backup initializer markers changed')
initializer = source.split(start)[1].split(end)[0]
initializer_pin = {'path': str(INITIALIZER.relative_to(ROOT)), 'sqlSHA256': hashlib.sha256(initializer.encode()).hexdigest()}
predecessor = json.loads(PREDECESSOR.read_text())
if predecessor.get('migrationVersion') != 20261114120000 or predecessor.get('backupInitializer') != initializer_pin:
    parser.error('qualified canonical044 predecessor or initializer changed')
query = (RELEASE / 'sentry-observability-catalog.sql').read_text().strip().rstrip(';')
restore = (RELEASE / 'sentry-observability-restore-catalog.sql').read_text().strip().rstrip(';')
public_sequence = (RELEASE / 'github-repository-scope-public-sequence.sql').read_text().strip().rstrip(';')
workspace_idle = (RELEASE / 'github-repository-scope-workspace-idle.sql').read_text().strip().rstrip(';')
public_guard = 'IF (' + public_sequence + ") IS DISTINCT FROM true THEN RAISE EXCEPTION 'Sentry observability public sequence boundary changed';END IF;\n"
workspace_guard = 'IF (' + workspace_idle + ") IS DISTINCT FROM true THEN RAISE EXCEPTION 'Sentry observability workspace owner is not idle';END IF;\n"


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'))


def schema(value):
    return {key: item for key, item in value.items() if key != 'restoreCatalogDigest'}


def predecessor_migration():
    # Для первой capture берётся только квалифицированный upstream044 catalog.
    # Будущие045 after fingerprints не подставляются и не угадываются.
    allowed = [schema(predecessor['snapshots'][mode+'-after']) for mode in MODES]
    mismatch = ' AND '.join('actual IS DISTINCT FROM $expected'+str(i)+'$'+canonical(value)+'$expected'+str(i)+'$::jsonb' for i, value in enumerate(allowed))
    begin = '-- BEGIN GENERATED SENTRY OBSERVABILITY PREDECESSOR\n'
    finish = '-- END GENERATED SENTRY OBSERVABILITY PREDECESSOR\n'
    body = begin + '-- +goose StatementBegin\nDO $closed_predecessor$\nDECLARE actual jsonb;\nBEGIN\n' + public_guard + workspace_guard + 'SELECT (' + query + ') INTO actual;\nIF ' + mismatch + " THEN RAISE EXCEPTION 'Sentry observability exact predecessor catalog changed';END IF;\nEND $closed_predecessor$;\n-- +goose StatementEnd\n" + finish
    text = MIGRATION.read_text()
    if text.count(begin) != 1 or text.count(finish) != 1:
        parser.error('Sentry predecessor generation markers changed')
    first = text.index(begin)
    last = text.index(finish, first) + len(finish)
    return text[:first] + body + text[last:]


if args.prepare_capture:
    MIGRATION.write_text(predecessor_migration())
    raise SystemExit(0)
if args.capture_directory:
    manifest = {'version': 2, 'predecessorVersion': 20261114120000, 'migrationVersion': 20261114130000,
                'catalogScope': 'ALL_CONTROL_PLANE_TABLES_FUNCTIONS_TRIGGERS_AUTHORITY_AND_PROJECT_PURGE_GRAPH',
                'backupInitializer': initializer_pin, 'predecessorManifestSHA256': hashlib.sha256(PREDECESSOR.read_bytes()).hexdigest(), 'snapshots': {}}
    for mode in MODES:
        for phase in ['before', 'after']:
            value = json.loads((args.capture_directory / (mode+'-'+phase+'.json')).read_text())
            if value.pop('backupInitializerSHA256', None) != initializer_pin['sqlSHA256']:
                parser.error('capture backup initializer source changed')
            manifest['snapshots'][mode+'-'+phase] = value
        if manifest['snapshots'][mode+'-before'] != predecessor['snapshots'][mode+'-after']:
            parser.error('captured045 predecessor differs from qualified044 '+mode)
    MANIFEST.write_text(json.dumps(manifest, ensure_ascii=False, sort_keys=True, indent=2)+'\n')
manifest = json.loads(MANIFEST.read_text())
if manifest.get('backupInitializer') != initializer_pin or manifest.get('predecessorManifestSHA256') != hashlib.sha256(PREDECESSOR.read_bytes()).hexdigest():
    parser.error('Sentry canonical predecessor source changed; recapture all profiles')
for mode in MODES:
    if manifest['snapshots'][mode+'-before'] != predecessor['snapshots'][mode+'-after']:
        parser.error('Sentry predecessor snapshot changed')
activity = (RELEASE/'workspace-unbound-stop-migration-precondition.sql').read_text()
activity = activity[activity.index(" 'activeRuntimeLeases'"):activity.index(")::text;ROLLBACK;")]
activity += ",\n 'activeIntegrationInvocations',(SELECT count(*) FROM control_plane.integration_invocations WHERE state NOT IN ('SUCCEEDED','FAILED','CANCELLED','REJECTED','EXPIRED')),\n 'activeIntegrationConnectionTests',(SELECT count(*) FROM control_plane.integration_connection_tests WHERE state IN ('DUE','CLAIMED')),\n 'activePersonalCredentialTests',(SELECT count(*) FROM control_plane.integration_personal_credential_tests WHERE state IN ('DUE','CLAIMED','PROBING')),\n 'activeRepositoryBundles',(SELECT count(*) FROM control_plane.repository_bundle_publications WHERE state IN ('WAITING_APPROVAL','QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME')),\n 'activeConfigurationSources',(SELECT count(*) FROM control_plane.managed_configuration_source_work WHERE state IN ('QUEUED','CLAIMED')),\n 'activeConfigurationWritebacks',(SELECT count(*) FROM control_plane.managed_configuration_writebacks WHERE state IN ('WAITING_APPROVAL','QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME'))"
activity += ",\n 'activeBackupSessions',(SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND usesysid IN (WITH RECURSIVE backup_roles(oid) AS (SELECT oid FROM pg_roles WHERE rolname='kodex_backup_reader' UNION SELECT m.member FROM pg_auth_members m JOIN backup_roles r ON m.roleid=r.oid) SELECT oid FROM backup_roles))"
outputs = {MIGRATION: predecessor_migration()}
catalog = {mode: {phase: schema(manifest['snapshots'][mode+'-'+phase]) for phase in ['before', 'after']} for mode in MODES}
outputs[RELEASE/'sentry-observability-catalog.gen.mjs'] = '// Generated: generate-sentry-observability-proof.py; exact044 and045 full catalog.\nexport const sentryObservabilityCatalog = Object.freeze('+canonical(catalog)+');\n'
for phase, version in [('before', 20261114120000), ('after', 20261114130000)]:
    boundary = """-- Generated: exact canonical backup boundary; no ACL normalization.
SET LOCAL search_path=pg_catalog;SET LOCAL ROLE control_plane_owner;
SET LOCAL lock_timeout='8s';SET LOCAL statement_timeout='30s';
SELECT pg_advisory_xact_lock(hashtextextended('kodex.workspace.backup-acl-maintenance.v1',0));
DO $sentry_backup_boundary$
DECLARE actual jsonb; activity jsonb;
BEGIN
 """+public_guard+workspace_guard+"""
 IF session_user<>'control_plane_migrator' OR current_database()<>'control_plane' OR (SELECT max(version_id) FROM public.goose_db_version WHERE is_applied)<>"""+str(version)+""" THEN RAISE EXCEPTION 'Sentry observability backup maintenance boundary changed';END IF;
 SELECT ("""+query+""") INTO actual;
 IF actual IS DISTINCT FROM $expected$"""+canonical(catalog['backup'][phase])+"""$expected$::jsonb THEN RAISE EXCEPTION 'Sentry observability canonical backup catalog changed';END IF;
 SELECT jsonb_build_object("""+activity+""") INTO activity;
 IF EXISTS(SELECT 1 FROM jsonb_each_text(activity) WHERE value::bigint<>0) THEN RAISE EXCEPTION 'Sentry observability backup owner is not idle';END IF;
END $sentry_backup_boundary$;
RESET ROLE;
"""
    outputs[ROOT/('services/internal/control-plane/cmd/cli/workspace_backup_sql/check_sentry_'+str(version)+'_backup.sql')] = boundary
for mode in ['normal', 'backup']:
    prefix = 'sentry-observability'+('-backup' if mode == 'backup' else '')
    for phase, kind in [('before', 'precondition'), ('after', 'postcondition')]:
        profile = 'SENTRY_OBSERVABILITY_'+('BACKUP_' if mode == 'backup' else '')+('PREDECESSOR_' if phase == 'before' else '')+'V1'
        fields = "'profile', '"+profile+"', 'database',current_database(),'readOnly',current_setting('transaction_read_only'),'gooseVersion',(SELECT max(version_id) FROM public.goose_db_version WHERE is_applied),'schemaCatalog',("+query+")"
        if mode == 'backup':
            fields += ", 'backupQualification',CASE WHEN EXISTS(SELECT 1 FROM pg_namespace n CROSS JOIN LATERAL aclexplode(n.nspacl) a WHERE n.oid='control_plane'::regnamespace AND a.grantee='kodex_backup_reader'::regrole AND a.privilege_type='USAGE') THEN 'CANONICAL_BACKUP' ELSE 'TABLE_ONLY_BACKUP' END"
        else:
            fields += ", 'backupQualification','NONE'"
        fields += ", 'publicSequenceSafe',true"
        if phase == 'before':
            fields += ",'workspaceOwnerIdle',true,'sentryObservabilityAbsent',NOT EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='control_plane.integration_definitions'::regclass AND contype='c' AND position('SENTRY' IN pg_get_constraintdef(oid))>0),"+activity
        allowed = [catalog[mode][phase]] + ([catalog['table-only-backup'][phase]] if mode == 'backup' else [])
        mismatch = ' AND '.join('actual IS DISTINCT FROM $expected'+str(i)+'$'+canonical(value)+'$expected'+str(i)+'$::jsonb' for i, value in enumerate(allowed))
        guard = 'DO $catalog_guard$ DECLARE actual jsonb;BEGIN '+public_guard+(workspace_guard if phase == 'before' else '')+'SELECT ('+query+") INTO actual;IF "+mismatch+" THEN RAISE EXCEPTION 'Sentry observability schema catalog changed';END IF;END $catalog_guard$;\n"
        outputs[RELEASE/(prefix+'-migration-'+kind+'.sql')] = "-- Generated: generate-sentry-observability-proof.py; полный закрытый schema catalog.\nBEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;\nSET LOCAL statement_timeout='15s';SET LOCAL lock_timeout='500ms';SET LOCAL search_path=pg_catalog;\n"+guard+'SELECT jsonb_build_object('+fields+')::text;\nROLLBACK;\n'
normal = manifest['snapshots']['normal-after']
backup = manifest['snapshots']['backup-after']
table_only = manifest['snapshots']['table-only-backup-after']
restoreSQL = """-- Generated: generate-sentry-observability-proof.py; exact045 ACL-only restore.
SET LOCAL search_path=pg_catalog;SET LOCAL ROLE control_plane_owner;
SET LOCAL lock_timeout='8s';SET LOCAL statement_timeout='30s';
SELECT pg_advisory_xact_lock(hashtextextended('kodex.workspace.backup-acl-maintenance.v1',0));
DO $restore045$
DECLARE actual jsonb;
BEGIN
 """+public_guard+workspace_guard+"""
 IF session_user<>'control_plane_migrator' OR current_database()<>'control_plane' OR (SELECT max(version_id) FROM public.goose_db_version WHERE is_applied)<>20261114130000 THEN RAISE EXCEPTION 'Sentry observability backup restore boundary changed';END IF;
 IF EXISTS(SELECT 1 FROM pg_class WHERE relnamespace='public'::regnamespace AND relkind IN('r','p','v','m','f') AND oid<>'public.goose_db_version'::regclass)
 OR EXISTS(SELECT 1 FROM pg_class c CROSS JOIN LATERAL aclexplode(coalesce(c.relacl,acldefault(CASE WHEN c.relkind='S' THEN 'S'::"char" ELSE 'r'::"char" END,c.relowner))) a WHERE (c.relnamespace='control_plane'::regnamespace OR c.oid='public.goose_db_version'::regclass) AND a.grantee='kodex_backup_reader'::regrole AND ((a.privilege_type<>'SELECT' AND NOT(c.relkind='S' AND a.privilege_type='USAGE')) OR a.is_grantable OR a.grantor<>'control_plane_owner'::regrole)) THEN RAISE EXCEPTION 'Sentry observability backup restore is not idle or safe';END IF;
 SELECT ("""+restore+""") INTO actual;
 IF (actual->>'catalogDigest' IS DISTINCT FROM '"""+normal['restoreCatalogDigest']+"""' AND actual->>'catalogDigest' IS DISTINCT FROM '"""+backup['restoreCatalogDigest']+"""' AND actual->>'catalogDigest' IS DISTINCT FROM '"""+table_only['restoreCatalogDigest']+"""') OR (actual->'tableDefaultACL' IS DISTINCT FROM $normal$"""+canonical(normal['tableDefaultACL'])+"""$normal$::jsonb AND actual->'tableDefaultACL' IS DISTINCT FROM $backup$"""+canonical(backup['tableDefaultACL'])+"""$backup$::jsonb) THEN RAISE EXCEPTION 'Sentry observability backup restore catalog changed';END IF;
 """+initializer+"""
 SELECT ("""+query+""") INTO actual;
 IF actual IS DISTINCT FROM $expected$"""+canonical(catalog['backup']['after'])+"""$expected$::jsonb THEN RAISE EXCEPTION 'Sentry observability backup restore final catalog changed';END IF;
END $restore045$;
RESET ROLE;
"""
outputs[ROOT/'services/internal/control-plane/cmd/cli/workspace_backup_sql/restore_20261114130000.sql'] = restoreSQL
failed = []
for path, content in outputs.items():
    if args.check:
        if not path.exists() or path.read_text() != content:
            failed.append(str(path.relative_to(ROOT)))
    else:
        path.write_text(content)
if failed:
    raise SystemExit('Sentry observability generated proof differs: '+', '.join(failed))
