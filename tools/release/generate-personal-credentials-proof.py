#!/usr/bin/env python3
"""Генерирует exact034 proof/restore поверх неизменного033 с явной заменой admission."""
import argparse
import copy
import hashlib
import json
from pathlib import Path


def outputs():
    """Сверяет schema-only provenance и выдаёт закрытые normal/backup SQL и catalog digests."""
    directory = Path(__file__).resolve().parent
    root = directory.parent.parent
    manifest = json.loads((directory / "personal-credentials-schema-manifest.json").read_text())
    migration = root / "services/internal/control-plane/cmd/cli/migrations/20261106000100_personal_integration_credentials.sql"
    predecessor = directory / "platform-oidc-identity-migration-postcondition.sql"
    backup_predecessor = directory / "platform-oidc-identity-backup-migration-postcondition.sql"
    idle_predecessor = directory / "platform-oidc-identity-migration-precondition.sql"
    restore_predecessor = root / "services/internal/control-plane/cmd/cli/workspace_backup_sql/restore_20261105000100.sql"
    reader = directory / "personal-credentials-catalog.sql"
    assert manifest["kind"] == "EXPECTED_SCHEMA_CONTRACT" and manifest["version"] == "20261106000100"
    for path, key in [(migration, "migrationSHA256"), (reader, "catalogReaderSHA256"),
                      (predecessor, "predecessorPostconditionSHA256"),
                      (backup_predecessor, "backupPredecessorPostconditionSHA256"),
                      (idle_predecessor, "idlePredecessorSHA256"),
                      (restore_predecessor, "restorePredecessorSHA256")]:
        assert hashlib.sha256(path.read_bytes()).hexdigest() == manifest[key], "frozen source changed"
    catalog = manifest["catalog"]
    assert len(catalog["tables"]) == 8 and len(catalog["functions"]) == 17 and len(catalog["triggers"]) == 17
    personal_tables = sorted(name for name in catalog["tables"] if name.startswith("integration_personal_"))
    assert len(personal_tables) == 5 and catalog["sequences"] is None
    assert all(table["owner"] == "control_plane_owner" for table in catalog["tables"].values())
    graph = catalog["purgeGraph"]
    assert graph["nodeCount"] == 111 and graph["edgeCount"] == 306
    assert graph["predecessorNodeCount"] == 109 and graph["predecessorEdgeCount"] == 303
    assert len(graph["nodes"]) == 111 and len(graph["edges"]) == 306
    admission = catalog["functions"]["control_plane.integration_grant_admission(uuid,uuid,uuid,text,text,text,text,text,text,text[])"]
    old_md5, new_md5 = manifest["admissionPredecessorSourceMD5"], admission["sourceMD5"]
    assert old_md5 != new_md5 and len(old_md5) == len(new_md5) == 32
    canonical = json.dumps(catalog, sort_keys=True, separators=(",", ":"), ensure_ascii=False)
    digest = hashlib.sha256(canonical.encode()).hexdigest()
    backup_catalog = copy.deepcopy(catalog)
    for table in backup_catalog["tables"].values():
        assert not any(row[0] == "kodex_backup_reader" for row in table["acl"])
        table["acl"].append(["kodex_backup_reader", "control_plane_owner", "SELECT", False])
        table["acl"].sort(key=lambda row: (row[0] == "PUBLIC", row[0], row[2]))
    backup_canonical = json.dumps(backup_catalog, sort_keys=True, separators=(",", ":"), ensure_ascii=False)
    backup_digest = hashlib.sha256(backup_canonical.encode()).hexdigest()
    query = reader.read_text().rstrip()
    assert query.endswith(");")
    query = query[:-2] + ") INTO v_actual;"

    def catalog_guard(expected):
        """Сверяет точную схему/ABI/RLS/ACL без нормализации фактических metadata."""
        assert "$expected_personal$" not in expected
        return "\nSET LOCAL search_path=pg_catalog;\nDO $personal_catalog$\nDECLARE v_actual jsonb;\nBEGIN\n" + query + "\nIF v_actual IS DISTINCT FROM $expected_personal$" + expected + "$expected_personal$::jsonb THEN\n RAISE EXCEPTION 'personal integration schema catalog changed';\nEND IF;\nEND $personal_catalog$;\n"

    def predecessor_authority_guard(backup):
        """До DDL сверяет unchanged owner/RLS и точный normal/backup ACL трёх прежних relations."""
        source_catalog = backup_catalog if backup else catalog
        expected = {name: {"owner": table["owner"], "kind": table["kind"], "rls": table["rls"],
                          "forceRls": table["forceRls"], "acl": table["acl"],
                          "columnACL": [[column[0], column[7]] for column in table["columns"] if column[7] is not None] or None}
                    for name, table in source_catalog["tables"].items()
                    if name in ("integration_connections", "integration_invocations", "runs")}
        assert len(expected) == 3
        encoded = json.dumps(expected, sort_keys=True, separators=(",", ":"), ensure_ascii=False)
        query = """
SET LOCAL search_path=pg_catalog;
DO $personal_predecessor_acl$
DECLARE v_actual jsonb;
BEGIN
 WITH relations AS (SELECT * FROM pg_catalog.pg_class WHERE relnamespace='control_plane'::regnamespace
  AND relname IN ('integration_connections','integration_invocations','runs'))
 SELECT jsonb_object_agg(r.relname,jsonb_build_object('owner',pg_get_userbyid(r.relowner),
  'kind',r.relkind,'rls',r.relrowsecurity,'forceRls',r.relforcerowsecurity,
  'columnACL',(SELECT jsonb_agg(jsonb_build_array(a.attname,a.attacl::text) ORDER BY a.attnum)
    FROM pg_catalog.pg_attribute a WHERE a.attrelid=r.oid AND a.attnum>0 AND NOT a.attisdropped AND a.attacl IS NOT NULL),
  'acl',(SELECT jsonb_agg(jsonb_build_array(CASE WHEN a.grantee=0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END,
    pg_get_userbyid(a.grantor),a.privilege_type,a.is_grantable) ORDER BY a.grantee=0,pg_get_userbyid(a.grantee),a.privilege_type)
    FROM aclexplode(coalesce(r.relacl,acldefault('r',r.relowner))) a))) INTO v_actual FROM relations r;
 IF v_actual IS DISTINCT FROM $expected_personal_acl$""" + encoded + """$expected_personal_acl$::jsonb THEN
  RAISE EXCEPTION 'personal integration predecessor authority changed';
 END IF;
END $personal_predecessor_acl$;
"""
        return query

    idle_source = idle_predecessor.read_text()
    idle = "\n 'oldClaimSessions'," + idle_source.split(" 'oldClaimSessions',", 1)[1].split(" 'platformOIDCAbsent',", 1)[0]
    assert "activeRuntimeRuns" in idle and "WAITING_HUMAN" in idle
    absent = """ 'personalCredentialsAbsent',NOT EXISTS(SELECT 1 FROM pg_catalog.pg_class r
 WHERE r.relnamespace='control_plane'::regnamespace AND r.relname LIKE 'integration_personal_%')
 AND NOT EXISTS(SELECT 1 FROM pg_catalog.pg_proc p WHERE p.pronamespace='control_plane'::regnamespace
  AND (p.proname LIKE 'guard_personal_%' OR p.proname IN ('cancel_personal_tests_on_connection_change','integration_connection_configuration_ready')))
 AND NOT EXISTS(SELECT 1 FROM pg_catalog.pg_attribute a WHERE a.attrelid='control_plane.integration_connections'::regclass AND a.attname='credential_policy' AND NOT a.attisdropped)
 AND NOT EXISTS(SELECT 1 FROM pg_catalog.pg_attribute a WHERE a.attrelid='control_plane.integration_invocations'::regclass AND a.attname='claimed_recovery_read_only' AND NOT a.attisdropped)
 AND NOT EXISTS(SELECT 1 FROM pg_catalog.pg_constraint c WHERE c.connamespace='control_plane'::regnamespace AND c.conname IN
 ('integration_connections_personal_owner_key','personal_invocation_root_owner_key','personal_invocation_connection_key')),
 'activeIntegrationInvocations',(SELECT count(*) FROM control_plane.integration_invocations WHERE state NOT IN ('SUCCEEDED','FAILED','CANCELLED')),
 'activeIntegrationConnectionTests',(SELECT count(*) FROM control_plane.integration_connection_tests WHERE state NOT IN ('SUCCEEDED','FAILED','CANCELLED')),
"""
    result = {}
    for backup in (False, True):
        parent = (backup_predecessor if backup else predecessor).read_text()
        parent_profile = "PLATFORM_OIDC_IDENTITY_" + ("BACKUP_" if backup else "") + "V1"
        marker = "'profile','" + parent_profile + "',"
        assert parent.count(marker) == 1 and parent.count("md5(p.prosrc)='" + old_md5 + "'") == 1
        for before in (True, False):
            source = parent
            if before:
                timeout = "SET LOCAL lock_timeout = '500ms';"
                assert source.count(timeout) == 1
                source = source.replace(timeout, timeout + predecessor_authority_guard(backup), 1)
            if not before:
                source = source.replace("md5(p.prosrc)='" + old_md5 + "'", "md5(p.prosrc)='" + new_md5 + "'", 1)
                # Полный actual034 graph/body проверяется собственным guard ниже.
                # Исторические projections исключают только два проверенных новых nodes;
                # workspace_guards сохраняет actual graph для проверки тела purge function.
                projection = "WHERE c.contype='f' AND c.connamespace='control_plane'::regnamespace AND c.conrelid NOT IN ("
                assert source.count(projection) == 2
                source = source.replace(projection, projection + "'control_plane.integration_personal_invocation_credentials'::regclass,'control_plane.integration_personal_invocation_admissions'::regclass,")
                timeout = "SET LOCAL lock_timeout = '500ms';"
                assert source.count(timeout) == 1
                source = source.replace(timeout, timeout + catalog_guard(backup_canonical if backup else canonical), 1)
            profile = "PERSONAL_INTEGRATION_CREDENTIALS_" + ("BACKUP_" if backup else "") + ("PREDECESSOR_V1" if before else "V1")
            source = source.replace(marker, "'profile','" + profile + "'," + (" 'database',current_database()," + idle + absent if before else ""), 1)
            if not before:
                tail = ")::text FROM predecessor_proof CROSS JOIN evidence_proof;"
                assert source.count(tail) == 1
                source = source.replace(tail, " || jsonb_build_object('personalCredentialsCatalog',true,'personalCredentialsCatalogDigest','" + (backup_digest if backup else digest) + "')" + tail, 1)
            name = "personal-credentials-" + ("backup-" if backup else "") + "migration-" + ("precondition" if before else "postcondition") + ".sql"
            result[directory / name] = "-- Generated: generate-personal-credentials-proof.py; frozen033 сохранён.\n" + source
    # Restore нормализует только SELECT восьми явно проверенных таблиц. WRITE,
    # grant option, column ACL и неизвестные роли не исправляются автоматически.
    names = sorted(catalog["tables"])
    targets = ",".join("control_plane." + name for name in names)
    restore = restore_predecessor.read_text()
    assert restore.count("20261105000100") == 1
    restore = restore.replace("20261105000100", "20261106000100", 1)
    marker = "GRANT SELECT ON control_plane.oidc_identity_observations,"
    assert restore.count(marker) == 1
    boundary = """DO $personal_backup_acl$
BEGIN
 IF EXISTS(SELECT 1 FROM pg_catalog.pg_class c CROSS JOIN LATERAL pg_catalog.aclexplode(c.relacl) a
  WHERE c.oid IN (""" + ",".join("'control_plane." + name + "'::regclass" for name in names) + """)
   AND pg_catalog.pg_get_userbyid(a.grantee)='kodex_backup_reader'
   AND (a.privilege_type<>'SELECT' OR a.is_grantable OR pg_catalog.pg_get_userbyid(a.grantor)<>'control_plane_owner')) THEN
  RAISE EXCEPTION 'personal backup reader authority changed';
 END IF;
END $personal_backup_acl$;
"""
    restore = restore.replace(marker, boundary + "REVOKE SELECT ON " + targets + " FROM kodex_backup_reader RESTRICT;\n" + catalog_guard(canonical) + "GRANT SELECT ON " + targets + " TO kodex_backup_reader;\n" + marker, 1)
    result[restore_predecessor.with_name("restore_20261106000100.sql")] = "-- Generated: generate-personal-credentials-proof.py; exact034 backup ACL restore.\n" + restore
    result[directory / "personal-credentials-catalog.gen.mjs"] = "// Generated: generate-personal-credentials-proof.py; exact034 catalog.\nexport const personalCredentialsCatalogDigest = '" + digest + "';\nexport const personalCredentialsBackupCatalogDigest = '" + backup_digest + "';\n"
    projection = {key: value for key, value in graph.items() if key not in ("nodes", "edges")}
    result[directory / "personal-credentials-catalog.gen.mjs"] += "export const personalCredentialsPurgeGraph = Object.freeze(" + json.dumps(projection, sort_keys=True, separators=(",", ":")) + ");\n"
    return result


def main():
    """Записывает собственные generated assets либо проверяет их точную воспроизводимость."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    arguments = parser.parse_args()
    for path, content in outputs().items():
        if arguments.check:
            assert path.read_text() == content, "personal schema generated file differs"
        else:
            path.write_text(content)


if __name__ == "__main__":
    main()
