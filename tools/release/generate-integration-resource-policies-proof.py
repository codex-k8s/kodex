#!/usr/bin/env python3
"""Генерирует exact037 поверх frozen036; не расширяет старые release verifiers."""
import argparse
import copy
import hashlib
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
PREFIX = 'tools/release/integration-resource-policies-'
MIGRATION = 'services/internal/control-plane/cmd/cli/migrations/20261109000100_integration_resource_policies.sql'
END = ')::text FROM predecessor_proof CROSS JOIN evidence_proof;'


def canonical(value):
    """Закрепляет authored schema metadata без чтения business rows либо live configuration."""
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=False)


def backup_catalog(catalog):
    """Добавляет только SELECT approved backup reader; иные ACL не нормализуются."""
    selected = copy.deepcopy(catalog)
    for table in selected['tables'].values():
        assert not any(row[0] == 'kodex_backup_reader' for row in table['acl'])
        table['acl'].append(['kodex_backup_reader', 'control_plane_owner', 'SELECT', False])
        table['acl'].sort(key=lambda row: (row[0] == 'PUBLIC', row[0], row[2]))
    return selected


def schema_guard(reader, catalog):
    """Сверяет точный full catalog, включая ABI, triggers, ограничения, ACL и purge graph."""
    query = reader.rstrip()
    assert query.endswith(');')
    query = query[:-2] + ') INTO v_actual;'
    return ('DO $resource_policy_catalog$\nDECLARE v_actual jsonb;\nBEGIN\n' + query +
            '\nIF v_actual IS DISTINCT FROM $expected_resource_policy$' + canonical(catalog) +
            "$expected_resource_policy$::jsonb THEN\n RAISE EXCEPTION 'integration resource policy schema catalog changed';\nEND IF;\nEND $resource_policy_catalog$;\n")


def replace_catalog(source, guard):
    """Заменяет только прежний закрытый personal catalog на полный новый, сохраняя остальные guards."""
    pattern = r'DO \$personal_catalog\$.*?END \$personal_catalog\$;'
    assert len(re.findall(pattern, source, re.S)) == 1
    return re.sub(pattern, lambda _: guard.rstrip(), source, count=1, flags=re.S)


def validate_authored_delta(before, after):
    """Допускает только перечисленный delta037; прочие прежние таблицы, функции и purge graph неизменны."""
    assert set(before['tables']) == set(after['tables'])
    assert len(after['tables']) == 11 and len(after['functions']) == 24 and len(after['triggers']) == 21
    assert before['purgeGraph'] == after['purgeGraph'] and before['sequences'] == after['sequences']
    changed_tables = {'integration_invocations', 'integration_personal_invocation_credentials', 'integration_grants', 'integration_approval_scopes'}
    assert {name for name in before['tables'] if before['tables'][name] != after['tables'][name]} == changed_tables
    for name in changed_tables - {'integration_approval_scopes'}:
        old, new = copy.deepcopy(before['tables'][name]), copy.deepcopy(after['tables'][name])
        old.pop('constraints'); new.pop('constraints')
        assert old == new, 'unexpected table authority/column drift'
    changed_functions = {'control_plane.guard_personal_invocation_credential()',
        'control_plane.integration_grant_admission(uuid,uuid,uuid,text,text,text,text,text,text,text[])',
        'control_plane.protect_email_mailbox_gate()', 'control_plane.revoke_integration_approval_scopes()',
        'control_plane.guard_integration_approval_scope_update()'}
    assert {name for name in before['functions'] if before['functions'][name] != after['functions'].get(name)} == changed_functions
    assert set(after['functions']) - set(before['functions']) == {
        'control_plane.integration_policy_legacy_capabilities(text,text,text,text,text)',
        'control_plane.integration_pinned_capabilities(uuid)',
        'control_plane.integration_declared_policy_allowed(uuid,text,text,text,text,text,jsonb)',
        'control_plane.guard_integration_approval_scope_insert()'}
    assert set(after['triggers']) - set(before['triggers']) == {'integration_approval_scopes.integration_approval_scopes_guard_insert'}
    assert all(after['triggers'][name] == value for name, value in before['triggers'].items())
    assert all(table['owner'] == 'control_plane_owner' for table in after['tables'].values())


def replace_volume_catalog(source, manifest):
    """037 сохраняет custody authority с устойчивыми к pg_dump именами колонок и тремя закрытыми AND-эквивалентами."""
    reader = (ROOT/(PREFIX+'volume-catalog.sql')).read_text()
    assert hashlib.sha256(reader.encode()).hexdigest() == manifest['volumeCatalogReaderSHA256']
    pattern=r'DO \$volume_catalog\$.*?END \$volume_catalog\$;'
    old=re.search(pattern,source,re.S)
    assert old is not None and len(re.findall(pattern,source,re.S))==1
    acl="FROM pg_catalog.aclexplode(coalesce(r.relacl,pg_catalog.acldefault('r',r.relowner))) a)"
    if "backup_acl.grantee" in old[0]:
        assert reader.count(acl)==1
        reader=reader.replace(acl,"FROM (SELECT * FROM pg_catalog.aclexplode(coalesce(r.relacl,pg_catalog.acldefault('r',r.relowner))) backup_acl WHERE pg_catalog.pg_get_userbyid(backup_acl.grantee) IS DISTINCT FROM 'kodex_backup_reader') a)",1)
    expected=json.loads(re.search(r"IF v_actual IS DISTINCT FROM '([^']+)'::jsonb",old[0])[1])
    assert manifest['volumeCatalog']['tables']==expected['tables']
    assert manifest['volumeCatalog']['functions']==expected['functions']
    query=reader.rstrip();assert query.endswith(');');query=query[:-2]+') INTO v_actual;'
    guard="DO $volume_catalog$\nDECLARE v_actual jsonb; v_path text;\nBEGIN\n v_path:=current_setting('search_path'); PERFORM set_config('search_path','pg_catalog',true);\n"+query+"\n PERFORM set_config('search_path',v_path,true);\n IF v_actual IS DISTINCT FROM $expected_volume$"+canonical(manifest['volumeCatalog'])+"$expected_volume$::jsonb THEN RAISE EXCEPTION 'workspace volume catalog changed'; END IF;\nEND $volume_catalog$;"
    return re.sub(pattern,lambda _:guard,source,count=1,flags=re.S)


def outputs():
    """Проверяет provenance, строит отдельные normal/backup guards, idle preconditions и SELECT-only restore037."""
    manifest = json.loads((ROOT / (PREFIX + 'schema-manifest.json')).read_text())
    assert manifest['kind'] == 'EXPECTED_SCHEMA_CONTRACT' and manifest['version'] == '20261109000100'
    for name, key in [(MIGRATION, 'migrationSHA256'), (PREFIX+'catalog.sql', 'catalogReaderSHA256'),
        ('tools/release/authority-proof-counter-migration-postcondition.sql', 'predecessorPostconditionSHA256'),
        ('tools/release/authority-proof-counter-backup-migration-postcondition.sql', 'backupPredecessorPostconditionSHA256'),
        ('tools/release/authority-proof-counter-migration-precondition.sql', 'idlePredecessorSHA256'),
        ('services/internal/control-plane/cmd/cli/workspace_backup_sql/restore_20261108000100.sql', 'restorePredecessorSHA256')]:
        assert hashlib.sha256((ROOT/name).read_bytes()).hexdigest() == manifest[key], 'authored provenance changed: '+name
    before, after = manifest['predecessorCatalog'], manifest['catalog']
    validate_authored_delta(before, after)
    expected_backup_before=backup_catalog(before)
    # Frozen036 не выдавал backup reader SELECT двум прежним approval/grant tables.
    #037 добавляет их только через собственный проверенный restore, а не через fallback.
    for name in ('integration_approval_scopes','integration_grants'):
        expected_backup_before['tables'][name]['acl']=before['tables'][name]['acl']
    assert manifest['backupPredecessorCatalog']==expected_backup_before
    reader = (ROOT/(PREFIX+'catalog.sql')).read_text()
    idle_source = (ROOT/'tools/release/authority-proof-counter-migration-precondition.sql').read_text()
    idle = " || jsonb_build_object(\n'oldClaimSessions'," + idle_source.split(" || jsonb_build_object(\n'oldClaimSessions',", 1)[1].split(END, 1)[0]
    idle = idle.split(" || jsonb_build_object('authorityProofCounterAbsent',", 1)[0]
    assert idle.count('activePersonalCredentialTests') == 1 and idle.rstrip().endswith(')')
    absent = " || jsonb_build_object('integrationResourcePoliciesAbsent',NOT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='control_plane.integration_approval_scopes'::regclass AND attname='scope_lifetime' AND NOT attisdropped) AND to_regprocedure('control_plane.integration_policy_legacy_capabilities(text,text,text,text,text)') IS NULL)"
    result, digests = {}, {'migrationSHA256': manifest['migrationSHA256']}
    for backup in (False, True):
        suffix = 'backup-' if backup else ''
        previous = (ROOT/('tools/release/authority-proof-counter-'+suffix+'migration-postcondition.sql')).read_text()
        tag = "'AUTHORITY_PROOF_COUNTER_"+('BACKUP_' if backup else '')+"V1'"
        assert previous.count(tag) == 1 and previous.count(END) == 1
        pre_catalog = manifest['backupPredecessorCatalog'] if backup else before
        post_catalog = backup_catalog(after) if backup else after
        digest = hashlib.sha256(canonical(post_catalog).encode()).hexdigest()
        digests['backup' if backup else 'normal'] = digest
        pre = previous.replace(tag, "'INTEGRATION_RESOURCE_POLICIES_"+('BACKUP_' if backup else '')+"PREDECESSOR_V1'")
        marker = "SET LOCAL search_path=pg_catalog;"
        assert pre.count(marker) >= 1
        pre = pre.replace(marker, marker+'\n'+schema_guard(reader, pre_catalog), 1)
        pre = pre.replace(END, idle+absent+END, 1)
        result[PREFIX+suffix+'migration-precondition.sql'] = '-- Generated: generate-integration-resource-policies-proof.py; frozen036 and full037 predecessor.\n'+pre
        post = replace_volume_catalog(replace_catalog(previous, schema_guard(reader, post_catalog)), manifest)
        old_catalog = json.loads(previous.split('$expected_personal$')[1])
        old_digest = hashlib.sha256(canonical(old_catalog).encode()).hexdigest()
        assert post.count(old_digest) == 1
        post = post.replace(old_digest, digest, 1)
        for name, old in before['functions'].items():
            new = after['functions'][name]
            if old != new:
                post = post.replace("md5(p.prosrc)='"+old['sourceMD5']+"'", "md5(p.prosrc)='"+new['sourceMD5']+"'")
        constraint = after['tables']['integration_invocations']['constraints']['integration_invocations_approval_check'][-1]
        pattern = r'^ AND md5\(replace\(pg_get_constraintdef\(c\.oid\).*?$'
        assert len(re.findall(pattern, post, re.M)) == 1
        post = re.sub(pattern, " AND md5(pg_get_constraintdef(c.oid))='"+hashlib.md5(constraint.encode()).hexdigest()+"')", post, count=1, flags=re.M)
        # SSH сохраняет собственные snapshot/permit guards; меняются только два
        # exact resource-kind checks, уже проверенные полным новым catalog.
        for table in ('integration_grants','integration_invocations'):
            name=table+'_resource_kind_check'
            expected=re.sub(r'\s+','',after['tables'][table]['constraints'][name][-1].replace('::text',''))
            pattern=r"(\('"+table+r"','"+name+r"',\$expected\$).*?(\$expected\$\))"
            assert len(re.findall(pattern,post))==1
            post=re.sub(pattern,lambda match:match[1]+expected+match[2],post,count=1)
        post = post.replace(tag, "'INTEGRATION_RESOURCE_POLICIES_"+('BACKUP_' if backup else '')+"V1'", 1)
        post = post.replace(END, " || jsonb_build_object('integrationResourcePoliciesCatalog',true,'integrationResourcePoliciesCatalogDigest','"+digest+"')"+END, 1)
        result[PREFIX+suffix+'migration-postcondition.sql'] = '-- Generated: generate-integration-resource-policies-proof.py; exact037 full catalog.\n'+post
    restore = (ROOT/'services/internal/control-plane/cmd/cli/workspace_backup_sql/restore_20261108000100.sql').read_text()
    assert restore.count('20261108000100::bigint') == 1
    restore = restore.replace('20261108000100::bigint', '20261109000100::bigint', 1)
    names = sorted(after['tables']); targets = ','.join('control_plane.'+name for name in names)
    acl_guard = """DO $resource_policy_backup_acl$
BEGIN
 IF EXISTS(SELECT 1 FROM pg_class r CROSS JOIN LATERAL aclexplode(r.relacl) a
  WHERE r.oid IN ("""+','.join("'control_plane."+name+"'::regclass" for name in names)+""")
  AND pg_get_userbyid(a.grantee)='kodex_backup_reader'
  AND (a.privilege_type<>'SELECT' OR a.is_grantable OR pg_get_userbyid(a.grantor)<>'control_plane_owner'))
 THEN RAISE EXCEPTION 'resource policy backup reader authority changed'; END IF;
END $resource_policy_backup_acl$;
"""
    restore = replace_volume_catalog(restore, manifest)
    restore = replace_catalog(restore, acl_guard+'REVOKE SELECT ON '+targets+' FROM kodex_backup_reader RESTRICT;\n'+schema_guard(reader,after)+'GRANT SELECT ON '+targets+' TO kodex_backup_reader;\n')
    result['services/internal/control-plane/cmd/cli/workspace_backup_sql/restore_20261109000100.sql'] = '-- Generated: generate-integration-resource-policies-proof.py; exact037 SELECT-only restore.\n'+restore
    result[PREFIX+'catalog.gen.mjs'] = '// Generated: generate-integration-resource-policies-proof.py; exact037.\nexport const integrationResourcePoliciesCatalog = Object.freeze('+canonical(digests)+');\n'
    return result


def main():
    """--check проверяет воспроизводимость, не меняя прежние либо текущие assets."""
    parser = argparse.ArgumentParser(); parser.add_argument('--check', action='store_true'); args = parser.parse_args()
    for name, raw in outputs().items():
        path = ROOT/name
        if args.check:
            assert path.read_text() == raw, 'generated037 output differs: '+name
        else:
            path.write_text(raw)


if __name__ == '__main__':
    main()
