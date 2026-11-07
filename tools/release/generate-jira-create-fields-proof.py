#!/usr/bin/env python3
"""Выводит exact035 из frozen034: меняется только закрытая ветвь старого Jira package."""
import argparse
import copy
import hashlib
import json
from pathlib import Path

PINS = {'tools/release/personal-credentials-migration-precondition.sql': '71e92e9eecca30397da1c0d0009fa09c3d54e8b15d760ee66716847d71484ed6', 'tools/release/personal-credentials-migration-postcondition.sql': '5f9cd776ba965a6d1f542702e163d52fd1d5139f3665594beb38c9d2bbd6e868', 'tools/release/personal-credentials-backup-migration-postcondition.sql': '92244cfbec9e49ac30895e72b58859f6bb582d0b1f4d01bf6653203eccd891c7', 'services/internal/control-plane/cmd/cli/workspace_backup_sql/restore_20261106000100.sql': '374b97518e986abf42d51ee0bc0f7fabd781fd49f7d6ca8ec0ac270b4eebc18d', 'tools/release/personal-credentials-schema-manifest.json': '71352dc71d33bd00f3fd3e4dc36fc98cde8f1da0b03034313fef73baa87acca5'}
SIGNATURE = 'control_plane.integration_grant_admission(uuid,uuid,uuid,text,text,text,text,text,text,text[])'
OLD_PACKAGE = '838878a5a984dafaa8d1c46563e3e660fc3b074515e25376e53ae0cae204fec8'
NEW_PACKAGE = 'fc35495f83ec21d3698c1e7cfa37eec1075ad35dd30b9dd7f9f680acf989cb31'
BEFORE = "                ELSE (\n                    SELECT revision.content::jsonb #> '{spec,capabilities}'"
AFTER = ("                WHEN connection.definition_key='jira-server-dc'\n"
         "                  AND (definition.definition_version,definition.digest,connection.definition_version,connection.definition_digest)\n"
         "                    = ('1.1.0','" + NEW_PACKAGE + "','1.0.0','" + OLD_PACKAGE + "')\n"
         "                THEN definition.capabilities\n" + BEFORE)


def canonical(value):
    """Сериализует публичный schema catalog детерминированно, без runtime данных."""
    return json.dumps(value,sort_keys=True,separators=(',',':'),ensure_ascii=False)


def source(definition):
    """Извлекает точный dollar-quoted body только из закреплённого pg_get_functiondef."""
    marker='AS $function$'
    assert definition.count(marker)==1 and definition.endswith('$function$\n')
    return definition.split(marker,1)[1].removesuffix('$function$\n')


def outputs():
    """Проверяет frozen bytes и строит migration, normal/backup/restore и precondition без ослабления034."""
    root=Path(__file__).resolve().parents[2]
    for name,digest in PINS.items():
        assert hashlib.sha256((root/name).read_bytes()).hexdigest()==digest,'frozen034 changed'
    manifest=json.loads((root/'tools/release/personal-credentials-schema-manifest.json').read_text())
    previous=manifest['catalog'];catalog=copy.deepcopy(previous)
    old=catalog['functions'][SIGNATURE];old_md5=old['sourceMD5']
    assert hashlib.md5(source(old['definition']).encode()).hexdigest()==old_md5
    assert old['definition'].count(BEFORE)==1 and AFTER not in old['definition']
    old['definition']=old['definition'].replace(BEFORE,AFTER)
    old['sourceMD5']=hashlib.md5(source(old['definition']).encode()).hexdigest()
    new_md5=old['sourceMD5']
    assert old_md5!=new_md5
    result={}
    migration="""-- Generated: generate-jira-create-fields-proof.py; exact034 function-only forward change.
-- +goose Up
SET ROLE control_plane_owner;
-- +goose StatementBegin
DO $jira_compatibility$
DECLARE
 v_signature regprocedure := '"""+SIGNATURE+"""'::regprocedure;
 v_definition text;
 v_before text := $jira_before$"""+BEFORE+"""$jira_before$;
 v_after text := $jira_after$"""+AFTER+"""$jira_after$;
BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_proc WHERE oid=v_signature AND md5(prosrc)='"""+old_md5+"""'
   AND pg_get_userbyid(proowner)='control_plane_owner' AND NOT prosecdef)
 THEN RAISE EXCEPTION 'Jira create fields admission predecessor changed'; END IF;
 v_definition := pg_get_functiondef(v_signature);
 IF (length(v_definition)-length(replace(v_definition,v_before,'')))/length(v_before)<>1
 THEN RAISE EXCEPTION 'Jira create fields admission patch is not unique'; END IF;
 EXECUTE replace(v_definition,v_before,v_after);
END $jira_compatibility$;
-- +goose StatementEnd
RESET ROLE;
"""
    migration_path='services/internal/control-plane/cmd/cli/migrations/20261107000100_jira_create_fields_compatibility.sql'
    result[migration_path]=migration
    digests={}
    for backup in (False,True):
        selected=copy.deepcopy(catalog)
        if backup:
            for table in selected['tables'].values():
                table['acl'].append(['kodex_backup_reader','control_plane_owner','SELECT',False])
                table['acl'].sort(key=lambda row:(row[0]=='PUBLIC',row[0],row[2]))
        prefix='backup-' if backup else ''
        path='tools/release/personal-credentials-'+prefix+'migration-postcondition.sql'
        raw=(root/path).read_text()
        parts=raw.split('$expected_personal$')
        assert len(parts)==3
        observed=json.loads(parts[1]);expected=copy.deepcopy(selected)
        # Единственная нормализация ожидаемой (не фактической) схемы — новая функция.
        old_expected=copy.deepcopy(selected);old_expected['functions'][SIGNATURE]=previous['functions'][SIGNATURE]
        assert observed==old_expected
        digest=hashlib.sha256(canonical(selected).encode()).hexdigest();digests['backup' if backup else 'normal']=digest
        raw=parts[0]+'$expected_personal$'+canonical(selected)+'$expected_personal$'+parts[2]
        assert raw.count(old_md5)==1
        raw=raw.replace(old_md5,new_md5)
        old_digest=hashlib.sha256(canonical(observed).encode()).hexdigest()
        assert raw.count(old_digest)==1
        raw=raw.replace(old_digest,digest)
        profile='PERSONAL_INTEGRATION_CREDENTIALS_'+('BACKUP_' if backup else '')+'V1'
        assert raw.count("'"+profile+"'")==1
        raw=raw.replace("'"+profile+"'","'JIRA_CREATE_FIELDS_"+('BACKUP_' if backup else '')+"V1'")
        result['tools/release/jira-create-fields-'+prefix+'migration-postcondition.sql']='-- Generated: generate-jira-create-fields-proof.py; exact035 full catalog.\n'+raw
        # Предусловие использует НЕизменённый actual034 proof и отдельные idle counters.
        pre=(root/path).read_text()
        assert pre.count("'"+profile+"'")==1
        pre=pre.replace("'"+profile+"'","'JIRA_CREATE_FIELDS_"+('BACKUP_' if backup else '')+"PREDECESSOR_V1'")
        end=')::text FROM predecessor_proof CROSS JOIN evidence_proof;'
        assert pre.count(end)==1
        frozen_idle=(root/'tools/release/personal-credentials-migration-precondition.sql').read_text()
        idle="'oldClaimSessions',"+frozen_idle.split("'oldClaimSessions',",1)[1].split(" 'personalCredentialsAbsent',",1)[0]
        assert 'activeRuntimeRuns' in idle and idle.rstrip().endswith(',')
        counters=" || jsonb_build_object(\n"+idle+"""
 'activeIntegrationInvocations',(SELECT count(*) FROM control_plane.integration_invocations WHERE state NOT IN ('SUCCEEDED','FAILED','CANCELLED')),
 'activeIntegrationConnectionTests',(SELECT count(*) FROM control_plane.integration_connection_tests WHERE state NOT IN ('SUCCEEDED','FAILED','CANCELLED')),
 'activePersonalCredentialTests',(SELECT count(*) FROM control_plane.integration_personal_credential_tests WHERE state IN ('DUE','CLAIMED','PROBING')))
"""
        pre=pre.replace(end,counters+end)
        result['tools/release/jira-create-fields-'+prefix+'migration-precondition.sql']='-- Generated: generate-jira-create-fields-proof.py; exact034 idle predecessor.\n'+pre
    restore=(root/'services/internal/control-plane/cmd/cli/workspace_backup_sql/restore_20261106000100.sql').read_text()
    parts=restore.split('$expected_personal$');assert len(parts)==3 and json.loads(parts[1])==previous
    restore=parts[0]+'$expected_personal$'+canonical(catalog)+'$expected_personal$'+parts[2]
    assert restore.count('20261106000100::bigint')==1
    restore=restore.replace('20261106000100::bigint','20261107000100::bigint')
    result['services/internal/control-plane/cmd/cli/workspace_backup_sql/restore_20261107000100.sql']='-- Generated: generate-jira-create-fields-proof.py; exact035 restore only.\n'+restore
    constants={'normal':digests['normal'],'backup':digests['backup'],'oldAdmissionMD5':old_md5,'newAdmissionMD5':new_md5,'oldDefinitionDigest':OLD_PACKAGE,'newDefinitionDigest':NEW_PACKAGE,'migrationSHA256':hashlib.sha256(migration.encode()).hexdigest()}
    result['tools/release/jira-create-fields-catalog.gen.mjs']='// Generated: generate-jira-create-fields-proof.py; exact035.\nexport const jiraCreateFieldsCatalog = Object.freeze('+canonical(constants)+');\n'
    return result


def main():
    """--check сверяет generated bytes; обычный запуск обновляет только перечисленные035 outputs."""
    parser=argparse.ArgumentParser();parser.add_argument('--check',action='store_true');args=parser.parse_args()
    root=Path(__file__).resolve().parents[2]
    for name,raw in outputs().items():
        path=root/name
        if args.check:assert path.read_text()==raw,'generated035 output differs: '+name
        else:path.write_text(raw)


if __name__=='__main__':main()
