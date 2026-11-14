#!/usr/bin/env python3
"""Закрепляет полный schema044 catalog и воспроизводимо генерирует read-only proofs."""
import argparse
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
RELEASE = ROOT / 'tools/release'
MANIFEST = RELEASE / 'github-repository-scope-schema-manifest.json'
parser = argparse.ArgumentParser()
parser.add_argument('--capture-directory', type=Path)
parser.add_argument('--check', action='store_true')
args = parser.parse_args()
if args.capture_directory:
    if args.check:
        parser.error('--capture-directory cannot be used with --check')
    manifest = {'version': 1, 'predecessorVersion': 20261113000100, 'migrationVersion': 20261114120000,
                'catalogScope': 'ALL_CONTROL_PLANE_TABLES_FUNCTIONS_TRIGGERS_AUTHORITY_AND_PROJECT_PURGE_GRAPH', 'snapshots': {}}
    for mode in ['normal', 'backup']:
        for phase in ['before', 'after']:
            manifest['snapshots'][mode+'-'+phase] = json.loads((args.capture_directory / (mode+'-'+phase+'.json')).read_text())
    for phase in ['before', 'after']:
        if manifest['snapshots']['normal-'+phase]['restoreCatalogDigest'] != manifest['snapshots']['backup-'+phase]['restoreCatalogDigest']:
            parser.error('normal and backup normalized catalogs differ')
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
# Разные источники схемы не угадываются: имена state tables ниже сверяются native тестом.
outputs = {}
outputs[RELEASE/'github-repository-scope-catalog.gen.mjs'] = '// Generated: generate-github-repository-scope-proof.py; exact043 and044 full catalog.\nexport const githubRepositoryScopeCatalog = Object.freeze('+canonical({mode: {phase: schema(mode,phase) for phase in ['before','after']} for mode in ['normal','backup']})+');\n'
for mode in ['normal','backup']:
    prefix='github-repository-scope'+('-backup' if mode=='backup' else '')
    for phase,kind in [('before','precondition'),('after','postcondition')]:
        profile='GITHUB_REPOSITORY_SCOPE_'+('BACKUP_' if mode=='backup' else '')+('PREDECESSOR_' if phase=='before' else '')+'V1'
        fields="'profile', '"+profile+"', 'database',current_database(),'readOnly',current_setting('transaction_read_only'),'gooseVersion',(SELECT max(version_id) FROM public.goose_db_version WHERE is_applied),'schemaCatalog',("+query+")"
        if phase=='before':
            fields += ",'githubRepositoryScopeAbsent',NOT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='control_plane.integration_personal_credentials'::regclass AND attname='github_discovery' AND NOT attisdropped),"+activity
        guard="DO $catalog_guard$ DECLARE actual jsonb;BEGIN SELECT ("+query+") INTO actual;IF actual IS DISTINCT FROM $expected$"+canonical(schema(mode,phase))+"$expected$::jsonb THEN RAISE EXCEPTION 'GitHub repository scope schema catalog changed';END IF;END $catalog_guard$;\n"
        outputs[RELEASE/(prefix+'-migration-'+kind+'.sql')] = "-- Generated: generate-github-repository-scope-proof.py; полный закрытый schema catalog.\nBEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;\nSET LOCAL statement_timeout='15s';SET LOCAL lock_timeout='500ms';SET LOCAL search_path=pg_catalog;\n"+guard+"SELECT jsonb_build_object("+fields+")::text;\nROLLBACK;\n"
# Миграция отвергает дрейф любого predecessor объекта до первого ALTER.
p=ROOT/'services/internal/control-plane/cmd/cli/migrations/20261114120000_github_repository_scope.sql'
s=p.read_text();begin='-- BEGIN GENERATED GITHUB SCOPE PREDECESSOR\n';end='-- END GENERATED GITHUB SCOPE PREDECESSOR\n'
body=begin+'-- +goose StatementBegin\nDO $closed_predecessor$\nDECLARE actual jsonb;\nBEGIN\nSELECT ('+query+") INTO actual;\nIF actual IS DISTINCT FROM $normal$"+canonical(schema('normal','before'))+"$normal$::jsonb AND actual IS DISTINCT FROM $backup$"+canonical(schema('backup','before'))+"$backup$::jsonb THEN RAISE EXCEPTION 'GitHub scope exact predecessor catalog changed';END IF;\nEND $closed_predecessor$;\n-- +goose StatementEnd\n"+end
if begin in s:
    a=s.index(begin);b=s.index(end,a)+len(end);s=s[:a]+body+s[b:]
else:
    needle='SET ROLE control_plane_owner;\n';s=s.replace(needle,needle+body,1)
outputs[p]=s
# Restore допускает только normal/backup SELECT. Дрейф column/function/schema ACL не нормализуется.
normal=manifest['snapshots']['normal-after'];backup=manifest['snapshots']['backup-after']
restoreSQL="""-- Generated: generate-github-repository-scope-proof.py; exact044 normal/backup restore.
SET LOCAL search_path=pg_catalog;SET LOCAL ROLE control_plane_owner;
SET LOCAL lock_timeout='8s';SET LOCAL statement_timeout='30s';
SELECT pg_advisory_xact_lock(hashtextextended('kodex.workspace.backup-acl-maintenance.v1',0));
DO $restore044$
DECLARE actual jsonb;
BEGIN
 IF session_user<>'control_plane_migrator' OR current_database()<>'control_plane' OR (SELECT max(version_id) FROM public.goose_db_version WHERE is_applied)<>20261114120000 THEN RAISE EXCEPTION 'GitHub backup restore boundary changed';END IF;
 IF EXISTS(SELECT 1 FROM control_plane.runs WHERE state NOT IN ('SUCCEEDED','FAILED','CANCELLED'))
 OR EXISTS(SELECT 1 FROM control_plane.run_nodes WHERE state IN ('QUEUED','RUNNING','WAITING'))
 OR EXISTS(SELECT 1 FROM control_plane.runtime_leases WHERE state='CLAIMED')
 OR EXISTS(SELECT 1 FROM control_plane.session_archive_tasks WHERE state IN('READY','CLAIMED'))
 OR EXISTS(SELECT 1 FROM control_plane.session_workspace_storage)
 OR EXISTS(SELECT 1 FROM pg_class WHERE relnamespace='public'::regnamespace AND relkind IN('r','p','v','m','f') AND oid<>'public.goose_db_version'::regclass)
 OR EXISTS(SELECT 1 FROM pg_class c CROSS JOIN LATERAL aclexplode(coalesce(c.relacl,acldefault('r',c.relowner))) a WHERE (c.relnamespace='control_plane'::regnamespace OR c.oid='public.goose_db_version'::regclass) AND a.grantee='kodex_backup_reader'::regrole AND (a.privilege_type<>'SELECT' OR a.is_grantable OR a.grantor<>'control_plane_owner'::regrole)) THEN RAISE EXCEPTION 'GitHub backup restore is not idle or safe';END IF;
 SELECT ("""+restore+""") INTO actual;
 IF actual->>'catalogDigest' IS DISTINCT FROM '"""+normal['restoreCatalogDigest']+"""' OR (actual->'tableDefaultACL' IS DISTINCT FROM $normal$"""+canonical(normal['tableDefaultACL'])+"""$normal$::jsonb AND actual->'tableDefaultACL' IS DISTINCT FROM $backup$"""+canonical(backup['tableDefaultACL'])+"""$backup$::jsonb) THEN RAISE EXCEPTION 'GitHub backup restore catalog changed';END IF;
 GRANT SELECT ON ALL TABLES IN SCHEMA control_plane,public TO kodex_backup_reader;
 ALTER DEFAULT PRIVILEGES FOR ROLE control_plane_owner IN SCHEMA control_plane GRANT SELECT ON TABLES TO kodex_backup_reader;
 SELECT ("""+query+""") INTO actual;
 IF actual IS DISTINCT FROM $expected$"""+canonical(schema('backup','after'))+"""$expected$::jsonb THEN RAISE EXCEPTION 'GitHub backup restore final catalog changed';END IF;
END $restore044$;
RESET ROLE;
"""
outputs[ROOT/'services/internal/control-plane/cmd/cli/workspace_backup_sql/restore_20261114120000.sql']=restoreSQL
failed=[]
for path,content in outputs.items():
    if args.check:
        if not path.exists() or path.read_text()!=content: failed.append(str(path.relative_to(ROOT)))
    else:
        path.write_text(content)
if failed:
    raise SystemExit('GitHub scope generated proof differs: '+', '.join(failed))
