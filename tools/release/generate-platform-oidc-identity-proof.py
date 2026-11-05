#!/usr/bin/env python3
"""Генерирует exact proof033 поверх неизменного read-only predecessor032."""
import argparse
import copy
import hashlib
import json
from pathlib import Path


def outputs():
    """Проверяет происхождение synthetic schema snapshot и строит закрытый SQL/JS контракт."""
    directory = Path(__file__).resolve().parent
    root = directory.parent.parent
    manifest = json.loads((directory / "platform-oidc-identity-schema-manifest.json").read_text())
    migration = root / "services/internal/control-plane/cmd/cli/migrations/20261105000100_platform_oidc_identity.sql"
    predecessor = directory / "workflow-content-page-migration-postcondition.sql"
    reader = directory / "platform-oidc-identity-catalog.sql"
    assert manifest["kind"] == "EXPECTED_SCHEMA_CONTRACT" and manifest["version"] == "20261105000100"
    backup_predecessor = directory / "workflow-content-page-backup-migration-postcondition.sql"
    restore_predecessor = root / "services/internal/control-plane/cmd/cli/workspace_backup_sql/restore_20261030000100.sql"
    idle_predecessor = directory / "workflow-content-page-migration-precondition.sql"
    for path, key in [(migration, "migrationSHA256"), (predecessor, "predecessorPostconditionSHA256"),
                      (reader, "catalogReaderSHA256"), (backup_predecessor, "backupPredecessorPostconditionSHA256"),
                      (restore_predecessor, "restorePredecessorSHA256"), (idle_predecessor, "idlePredecessorSHA256")]:
        assert hashlib.sha256(path.read_bytes()).hexdigest() == manifest[key], "frozen source changed"
    catalog = manifest["catalog"]
    assert len(catalog["tables"]) == 4 and len(catalog["functions"]) == 3 and len(catalog["triggers"]) == 4
    canonical = json.dumps(catalog, sort_keys=True, separators=(",", ":"), ensure_ascii=False)
    digest = hashlib.sha256(canonical.encode()).hexdigest()
    query = reader.read_text().rstrip()
    assert query.endswith(");")
    query = query[:-2] + ") INTO v_actual;"
    def catalog_guard(expected):
        """Сверяет весь exact catalog без удаления неизвестных ACL либо нормализации actual metadata."""
        return """
SET LOCAL search_path=pg_catalog;
DO $oidc_identity_catalog$
DECLARE v_actual jsonb;
BEGIN
""" + query + "\nIF v_actual IS DISTINCT FROM $expected_oidc$" + expected + "$expected_oidc$::jsonb THEN\n" + "    RAISE EXCEPTION 'platform OIDC identity schema catalog changed';\nEND IF;\nEND $oidc_identity_catalog$;\n"

    backup_catalog = copy.deepcopy(catalog)
    for table in backup_catalog["tables"].values():
        table["acl"].append(["kodex_backup_reader", "control_plane_owner", "SELECT", False])
        table["acl"].sort(key=lambda row: (row[0] == "PUBLIC", row[0], row[2]))
    backup_canonical = json.dumps(backup_catalog, sort_keys=True, separators=(",", ":"), ensure_ascii=False)
    backup_digest = hashlib.sha256(backup_canonical.encode()).hexdigest()
    idle_source = idle_predecessor.read_text()
    idle = "\n 'oldClaimSessions'," + idle_source.split(" 'oldClaimSessions',", 1)[1].split(" 'workflowContentPagePredecessor',", 1)[0]
    assert "activeRuntimeRuns" in idle and "WAITING_HUMAN" in idle
    absent = """ 'platformOIDCAbsent',NOT EXISTS(SELECT 1 FROM pg_catalog.pg_class r
  WHERE r.relnamespace='control_plane'::regnamespace AND r.relname IN
  ('oidc_identity_policies','oidc_identity_policy_current','oidc_identity_sessions','oidc_identity_observations','oidc_identity_session_revisions'))
 AND NOT EXISTS(SELECT 1 FROM pg_catalog.pg_proc p WHERE p.pronamespace='control_plane'::regnamespace AND p.proname LIKE 'guard_oidc_identity_%')
 AND NOT EXISTS(SELECT 1 FROM pg_catalog.pg_constraint WHERE conrelid='control_plane.subjects'::regclass AND conname='subjects_oidc_owner_key'),
"""
    output = {}
    for backup in (False, True):
        parent = (backup_predecessor if backup else predecessor).read_text()
        parent_profile = "WORKFLOW_CONTENT_PAGE_" + ("BACKUP_" if backup else "") + "V1"
        prefix = "platform-oidc-identity-" + ("backup-" if backup else "") + "migration-"
        for before in (False, True):
            source = parent
            marker = "SET LOCAL lock_timeout = '500ms';"
            assert source.count(marker) == 1
            if not before:
                source = source.replace(marker, marker + catalog_guard(backup_canonical if backup else canonical), 1)
            head = "SELECT jsonb_build_object(\n 'profile','" + parent_profile + "',"
            tail = ")::text FROM predecessor_proof CROSS JOIN evidence_proof;"
            assert source.count(head) == 1 and source.count(tail) == 1
            profile = "PLATFORM_OIDC_IDENTITY_" + ("BACKUP_" if backup else "") + ("PREDECESSOR_V1" if before else "V1")
            fields = " 'database',current_database()," + idle + absent if before else ""
            source = source.replace(head, "SELECT (jsonb_build_object(\n 'profile','" + profile + "',\n" + fields, 1)
            extension = "" if before else " || jsonb_build_object('platformOIDCCatalog',true,'platformOIDCCatalogDigest','" + (backup_digest if backup else digest) + "')"
            source = source.replace(tail, ")" + extension + ")::text FROM predecessor_proof CROSS JOIN evidence_proof;", 1)
            output[directory / (prefix + ("precondition" if before else "postcondition") + ".sql")] = "-- Generated: generate-platform-oidc-identity-proof.py; frozen032 не изменён.\n" + source
    targets = ",".join("control_plane." + name for name in sorted(catalog["tables"]))
    restore = restore_predecessor.read_text()
    assert restore.count("20261030000100") == 1
    marker = "GRANT SELECT ON control_plane.session_workspace_storage,"
    assert restore.count(marker) == 1
    restore = restore.replace("20261030000100", "20261105000100", 1)
    restore = restore.replace(marker, "REVOKE SELECT ON " + targets + " FROM kodex_backup_reader RESTRICT;\n" + catalog_guard(canonical) +
                              "GRANT SELECT ON " + targets + " TO kodex_backup_reader;\n" + marker, 1)
    output[restore_predecessor.with_name("restore_20261105000100.sql")] = "-- Generated: generate-platform-oidc-identity-proof.py; exact033 backup ACL restore.\n" + restore
    output[directory / "platform-oidc-identity-catalog.gen.mjs"] = "// Generated: generate-platform-oidc-identity-proof.py; exact033 catalog.\nexport const platformOIDCIdentityCatalogDigest = '" + digest + "';\nexport const platformOIDCIdentityBackupCatalogDigest = '" + backup_digest + "';\n"
    return output


def main():
    """Записывает proof либо проверяет его воспроизводимость без изменения файлов."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    for path, content in outputs().items():
        if args.check:
            assert path.read_text() == content, "platform OIDC proof generated file differs"
        else:
            path.write_text(content)


if __name__ == "__main__":
    main()
