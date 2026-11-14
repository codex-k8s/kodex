#!/usr/bin/env python3
"""Закрепляет полный schema044 catalog и воспроизводимо генерирует read-only proofs."""
import argparse
import json
import hashlib
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
RELEASE = ROOT / 'tools/release'
MANIFEST = RELEASE / 'github-repository-scope-schema-manifest.json'
parser = argparse.ArgumentParser()
parser.add_argument('--capture-directory', type=Path)
parser.add_argument('--check', action='store_true')
INITIALIZER = ROOT / 'deploy/k8s/base/platform-state/postgresql/reconcile-runtime-credentials.sh'
initializer_source = INITIALIZER.read_text()
begin_initializer = '-- BEGIN CANONICAL CONTROL PLANE BACKUP ACL\n'
end_initializer = '-- END CANONICAL CONTROL PLANE BACKUP ACL'
if initializer_source.count(begin_initializer) != 1 or initializer_source.count(end_initializer) != 1:
    parser.error('canonical backup initializer markers changed')
initializer = initializer_source.split(begin_initializer)[1].split(end_initializer)[0]
initializer_sha256 = hashlib.sha256(initializer.encode()).hexdigest()
args = parser.parse_args()
if args.capture_directory:
    if args.check:
        parser.error('--capture-directory cannot be used with --check')
    manifest = {'version': 1, 'predecessorVersion': 20261113000100, 'migrationVersion': 20261114120000,
                'catalogScope': 'ALL_CONTROL_PLANE_TABLES_FUNCTIONS_TRIGGERS_AUTHORITY_AND_PROJECT_PURGE_GRAPH', 'snapshots': {}}
    for mode in ['normal', 'backup', 'table-only-backup']:
        for phase in ['before', 'after']:
            manifest['snapshots'][mode+'-'+phase] = json.loads((args.capture_directory / (mode+'-'+phase+'.json')).read_text())
    for snapshot in manifest['snapshots'].values():
        if snapshot.pop('backupInitializerSHA256', None) != initializer_sha256:
            parser.error('capture backup initializer source changed')
    manifest['backupInitializer'] = {'path': str(INITIALIZER.relative_to(ROOT)), 'sqlSHA256': initializer_sha256}
    MANIFEST.write_text(json.dumps(manifest, ensure_ascii=False, sort_keys=True, indent=2)+'\n')
manifest = json.loads(MANIFEST.read_text())
query = (RELEASE / 'github-repository-scope-catalog.sql').read_text().strip().rstrip(';')
restore = (RELEASE / 'github-repository-scope-restore-catalog.sql').read_text().strip().rstrip(';')
def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'))
def schema(mode, phase):
    return {key: value for key, value in manifest['snapshots'][mode+'-'+phase].items() if key != 'restoreCatalogDigest'}
activity = (RELEASE/'workspace-unbound-stop-migration-precondition.sql').read_text()
activity = activity[activity.index(" 'activeRuntimeLeases'"):activity.index(")::text;ROLLBACK;")]
activity += ",\n 'activeIntegrationInvocations',(SELECT count(*) FROM control_plane.integration_invocations WHERE state NOT IN ('SUCCEEDED','FAILED','CANCELLED','REJECTED','EXPIRED')),\n 'activeIntegrationConnectionTests',(SELECT count(*) FROM control_plane.integration_connection_tests WHERE state IN ('DUE','CLAIMED')),\n 'activePersonalCredentialTests',(SELECT count(*) FROM control_plane.integration_personal_credential_tests WHERE state IN ('DUE','CLAIMED','PROBING')),\n 'activeRepositoryBundles',(SELECT count(*) FROM control_plane.repository_bundle_publications WHERE state IN ('WAITING_APPROVAL','QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME')),\n 'activeConfigurationSources',(SELECT count(*) FROM control_plane.managed_configuration_source_work WHERE state IN ('QUEUED','CLAIMED')),\n 'activeConfigurationWritebacks',(SELECT count(*) FROM control_plane.managed_configuration_writebacks WHERE state IN ('WAITING_APPROVAL','QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME'))"
activity += ",\n 'activeBackupSessions',(SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND usesysid IN (WITH RECURSIVE backup_roles(oid) AS (SELECT oid FROM pg_roles WHERE rolname='kodex_backup_reader' UNION SELECT m.member FROM pg_auth_members m JOIN backup_roles r ON m.roleid=r.oid) SELECT oid FROM backup_roles))"
# Разные источники схемы не угадываются: имена state tables ниже сверяются native тестом.
outputs = {}
outputs[RELEASE/'github-repository-scope-catalog.gen.mjs'] = '// Generated: generate-github-repository-scope-proof.py; exact043 and044 full catalog.\nexport const githubRepositoryScopeCatalog = Object.freeze('+canonical({mode: {phase: schema(mode,phase) for phase in ['before','after']} for mode in ['normal','backup','table-only-backup']})+');\n'
for mode in ['normal','backup']:
    prefix='github-repository-scope'+('-backup' if mode=='backup' else '')
    for phase,kind in [('before','precondition'),('after','postcondition')]:
        profile='GITHUB_REPOSITORY_SCOPE_'+('BACKUP_' if mode=='backup' else '')+('PREDECESSOR_' if phase=='before' else '')+'V1'
        fields="'profile', '"+profile+"', 'database',current_database(),'readOnly',current_setting('transaction_read_only'),'gooseVersion',(SELECT max(version_id) FROM public.goose_db_version WHERE is_applied),'schemaCatalog',("+query+")"
        if mode == 'backup':
            fields += ", 'backupQualification',CASE WHEN EXISTS(SELECT 1 FROM pg_namespace n CROSS JOIN LATERAL aclexplode(n.nspacl) a WHERE n.oid='control_plane'::regnamespace AND a.grantee='kodex_backup_reader'::regrole AND a.privilege_type='USAGE') THEN 'CANONICAL_BACKUP' ELSE 'TABLE_ONLY_BACKUP' END"
        else:
            fields += ", 'backupQualification','NONE'"
        if phase=='before':
            fields += ",'githubRepositoryScopeAbsent',NOT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='control_plane.integration_personal_credentials'::regclass AND attname='github_discovery' AND NOT attisdropped),"+activity
        allowed = [schema(mode,phase)] + ([schema('table-only-backup',phase)] if mode == 'backup' else [])
        mismatch = ' AND '.join('actual IS DISTINCT FROM $expected'+str(i)+'$'+canonical(v)+'$expected'+str(i)+'$::jsonb' for i,v in enumerate(allowed))
        guard="DO $catalog_guard$ DECLARE actual jsonb;BEGIN SELECT ("+query+") INTO actual;IF "+mismatch+" THEN RAISE EXCEPTION 'GitHub repository scope schema catalog changed';END IF;END $catalog_guard$;\n"
        outputs[RELEASE/(prefix+'-migration-'+kind+'.sql')] = "-- Generated: generate-github-repository-scope-proof.py; полный закрытый schema catalog.\nBEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;\nSET LOCAL statement_timeout='15s';SET LOCAL lock_timeout='500ms';SET LOCAL search_path=pg_catalog;\n"+guard+"SELECT jsonb_build_object("+fields+")::text;\nROLLBACK;\n"
# Historical044 остаётся byte-identical; новый maintenance path не переписывает её DDL.
if manifest.get('backupInitializer') != {'path': str(INITIALIZER.relative_to(ROOT)), 'sqlSHA256': initializer_sha256}:
    parser.error('manifest backup initializer source changed; recapture both profiles')
# Restore допускает только normal/backup SELECT. Дрейф column/function/schema ACL не нормализуется.
normal=manifest['snapshots']['normal-after'];backup=manifest['snapshots']['backup-after']
for phase, version in [('before',20261113000100),('after',20261114120000)]:
 normal=manifest['snapshots']['normal-'+phase];backup=manifest['snapshots']['backup-'+phase]
 table_only=manifest['snapshots']['table-only-backup-'+phase]
 restoreSQL="""-- Generated: generate-github-repository-scope-proof.py; exact044 normal/backup restore.
 SET LOCAL search_path=pg_catalog;SET LOCAL ROLE control_plane_owner;
 SET LOCAL lock_timeout='8s';SET LOCAL statement_timeout='30s';
 SELECT pg_advisory_xact_lock(hashtextextended('kodex.workspace.backup-acl-maintenance.v1',0));
 DO $restore044$
 DECLARE actual jsonb;
 BEGIN
  IF session_user<>'control_plane_migrator' OR current_database()<>'control_plane' OR (SELECT max(version_id) FROM public.goose_db_version WHERE is_applied)<>"""+str(version)+""" THEN RAISE EXCEPTION 'GitHub backup restore boundary changed';END IF;
  IF EXISTS(SELECT 1 FROM control_plane.runs WHERE state NOT IN ('SUCCEEDED','FAILED','CANCELLED'))
  OR EXISTS(SELECT 1 FROM control_plane.run_nodes WHERE state IN ('QUEUED','RUNNING','WAITING'))
  OR EXISTS(SELECT 1 FROM control_plane.runtime_leases WHERE state='CLAIMED')
  OR EXISTS(SELECT 1 FROM control_plane.session_archive_tasks WHERE state IN('READY','CLAIMED'))
  OR EXISTS(SELECT 1 FROM control_plane.session_workspace_storage)
  OR EXISTS(SELECT 1 FROM pg_class WHERE relnamespace='public'::regnamespace AND relkind IN('r','p','v','m','f') AND oid<>'public.goose_db_version'::regclass)
  OR EXISTS(SELECT 1 FROM pg_class c CROSS JOIN LATERAL aclexplode(coalesce(c.relacl,acldefault(CASE WHEN c.relkind='S' THEN 'S'::"char" ELSE 'r'::"char" END,c.relowner))) a WHERE (c.relnamespace='control_plane'::regnamespace OR c.oid='public.goose_db_version'::regclass) AND a.grantee='kodex_backup_reader'::regrole AND ((a.privilege_type<>'SELECT' AND NOT(c.relkind='S' AND a.privilege_type='USAGE')) OR a.is_grantable OR a.grantor<>'control_plane_owner'::regrole)) THEN RAISE EXCEPTION 'GitHub backup restore is not idle or safe';END IF;
  SELECT ("""+restore+""") INTO actual;
  IF (actual->>'catalogDigest' IS DISTINCT FROM '"""+normal['restoreCatalogDigest']+"""' AND actual->>'catalogDigest' IS DISTINCT FROM '"""+backup['restoreCatalogDigest']+"""' AND actual->>'catalogDigest' IS DISTINCT FROM '"""+table_only['restoreCatalogDigest']+"""') OR (actual->'tableDefaultACL' IS DISTINCT FROM $normal$"""+canonical(normal['tableDefaultACL'])+"""$normal$::jsonb AND actual->'tableDefaultACL' IS DISTINCT FROM $backup$"""+canonical(backup['tableDefaultACL'])+"""$backup$::jsonb) THEN RAISE EXCEPTION 'GitHub backup restore catalog changed';END IF;
  """+initializer+"""
  SELECT ("""+query+""") INTO actual;
  IF actual IS DISTINCT FROM $expected$"""+canonical(schema('backup',phase))+"""$expected$::jsonb THEN RAISE EXCEPTION 'GitHub backup restore final catalog changed';END IF;
 END $restore044$;
 RESET ROLE;
 """
 outputs[ROOT/('services/internal/control-plane/cmd/cli/workspace_backup_sql/restore_'+str(version)+'.sql')]=restoreSQL.strip()+'\n'
# Только exact043 canonical/table-only допускаются к временной normalization.
normalization = """-- Generated: exact043 backup maintenance; frozen044 не меняется.
SET LOCAL search_path=pg_catalog;SET LOCAL ROLE control_plane_owner;
SET LOCAL lock_timeout='8s';SET LOCAL statement_timeout='30s';
SELECT pg_advisory_xact_lock(hashtextextended('kodex.workspace.backup-acl-maintenance.v1',0));
DO $normalize043$
DECLARE actual jsonb; activity jsonb;
BEGIN
 IF session_user<>'control_plane_migrator' OR current_database()<>'control_plane' OR (SELECT max(version_id) FROM public.goose_db_version WHERE is_applied)<>20261113000100 THEN RAISE EXCEPTION 'GitHub backup normalize boundary changed'; END IF;
 SELECT ("""+query+""") INTO actual;
 IF actual IS DISTINCT FROM $canonical$"""+canonical(schema('backup','before'))+"""$canonical$::jsonb AND actual IS DISTINCT FROM $table_only$"""+canonical(schema('table-only-backup','before'))+"""$table_only$::jsonb THEN RAISE EXCEPTION 'GitHub backup normalize catalog changed';END IF;
 SELECT jsonb_build_object("""+activity+""") INTO activity;
 IF EXISTS(SELECT 1 FROM jsonb_each_text(activity) WHERE value::bigint<>0) THEN RAISE EXCEPTION 'GitHub backup normalize owner is not idle';END IF;
 REVOKE USAGE ON SCHEMA control_plane FROM kodex_backup_reader;
 REVOKE USAGE,SELECT ON ALL SEQUENCES IN SCHEMA control_plane FROM kodex_backup_reader;
 ALTER DEFAULT PRIVILEGES FOR ROLE control_plane_owner IN SCHEMA control_plane REVOKE USAGE,SELECT ON SEQUENCES FROM kodex_backup_reader;
 SELECT ("""+query+""") INTO actual;
 IF actual IS DISTINCT FROM $table_only$"""+canonical(schema('table-only-backup','before'))+"""$table_only$::jsonb THEN RAISE EXCEPTION 'GitHub backup normalize final catalog changed';END IF;
END $normalize043$;
RESET ROLE;
"""
outputs[ROOT/'services/internal/control-plane/cmd/cli/workspace_backup_sql/normalize_20261113000100.sql']=normalization
failed=[]
for path,content in outputs.items():
    if args.check:
        if not path.exists() or path.read_text()!=content: failed.append(str(path.relative_to(ROOT)))
    else:
        path.write_text(content)
if failed:
    raise SystemExit('GitHub scope generated proof differs: '+', '.join(failed))
