#!/usr/bin/env python3
"""Формирует отдельный maintenance-профиль; исторические SQL не изменяются."""

import argparse
import hashlib
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
DIRECTORY = ROOT / "tools/release"
OUTPUT = ROOT / "services/internal/control-plane/cmd/cli/workspace_backup_sql"
TABLES = ("session_workspace_storage", "session_archives", "session_archive_tasks",
          "session_workspace_volumes", "session_workspace_volume_tasks", "session_workspace_volume_attempts")


def frozen(name, digest):
    """Читает только проверенный predecessor; не принимает изменённое тело."""
    raw = (DIRECTORY / name).read_bytes()
    assert hashlib.sha256(raw).hexdigest() == digest, "backup compatibility predecessor changed"
    return raw.decode()


def generate():
    """Встраивает неизменённые catalog guards до восстановления точных grants."""
    workspace = frozen("session-workspace-catalog-guard.sql", "892d7648fe318a41ffeec8da77dd2f1739f9e9aecb4e156ea41f26520eb825b6")
    checkpoint = frozen("workspace-checkpoint-catalog-guard.sql", "f3079ac4f4c3331cc350b2dd9c129eb2ba1819ede29b56e03b8eab4f5f3ca8f5")
    volume = frozen("workspace-volume-catalog-guard.sql", "097aacb3b0b72420aeabfa9686757ad0acce477f8117aca51aa5ca1f180b5d2e")
    restore = frozen("workspace-restore-migration-postcondition.sql", "746b7cbf9e22ea84d7fd6cc7e3ab696f5d059307b2d5997a56cf3adad2807161")
    restore_proof = restore
    restore = restore.split("-- Проверка actual restore extension", 1)[1].split("WITH RECURSIVE ssh_columns", 1)[0]
    restore = "-- Проверка actual restore extension" + restore
    header = """-- Generated: generate-workspace-backup-compatibility.py; только мигратор, без пользовательских payload.
SET LOCAL ROLE control_plane_owner;
SET LOCAL lock_timeout='8s';
SET LOCAL statement_timeout='30s';
SELECT pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended('kodex.workspace.backup-acl-maintenance.v1',0));
DO $backup_boundary$
BEGIN
 IF current_database()<>'control_plane' OR session_user<>'control_plane_migrator'
 OR NOT EXISTS(SELECT 1 FROM pg_catalog.pg_roles WHERE rolname='kodex_backup_reader'
  AND rolcanlogin AND NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole AND NOT rolinherit AND NOT rolreplication AND rolbypassrls)
 OR EXISTS(SELECT 1 FROM pg_catalog.pg_auth_members m JOIN pg_catalog.pg_roles r ON r.oid=m.member WHERE r.rolname='kodex_backup_reader')
 OR EXISTS(SELECT 1 FROM control_plane.runs WHERE state IN('QUEUED','RUNNING','CANCELLING','WAITING_HUMAN'))
 OR EXISTS(SELECT 1 FROM control_plane.run_nodes WHERE state IN('QUEUED','RUNNING','WAITING'))
 OR EXISTS(SELECT 1 FROM control_plane.session_turns WHERE state IN('QUEUED','RUNNING'))
 OR EXISTS(SELECT 1 FROM control_plane.runtime_leases WHERE state='CLAIMED')
 OR EXISTS(SELECT 1 FROM control_plane.session_archive_tasks WHERE state IN('READY','CLAIMED'))
 OR EXISTS(SELECT 1 FROM control_plane.session_workspace_storage) THEN
  RAISE EXCEPTION 'workspace backup maintenance boundary is not idle';
 END IF;
END $backup_boundary$;
"""
    # Никаких ALL PRIVILEGES/CASCADE: лишний grant является отказом, а не поводом его скрыть.
    acl_check = """DO $backup_acl$
DECLARE target text; relation oid;
BEGIN
 FOREACH target IN ARRAY ARRAY[%s] LOOP
  relation:=pg_catalog.to_regclass('control_plane.'||target);
  IF relation IS NULL THEN CONTINUE; END IF;
  IF EXISTS(SELECT 1 FROM pg_catalog.pg_class c WHERE c.oid=relation AND
    (c.relkind<>'r' OR pg_catalog.pg_get_userbyid(c.relowner)<>'control_plane_owner'))
   OR EXISTS(SELECT 1 FROM pg_catalog.pg_class c CROSS JOIN LATERAL pg_catalog.aclexplode(c.relacl) a
    WHERE c.oid=relation AND pg_catalog.pg_get_userbyid(a.grantee)='kodex_backup_reader'
      AND (a.privilege_type<>'SELECT' OR a.is_grantable OR pg_catalog.pg_get_userbyid(a.grantor)<>'control_plane_owner'))
   OR EXISTS(SELECT 1 FROM pg_catalog.pg_attribute a WHERE a.attrelid=relation AND a.attnum>0 AND a.attacl IS NOT NULL) THEN
   RAISE EXCEPTION 'workspace backup table authority changed';
  END IF;
 END LOOP;
 IF EXISTS(SELECT 1 FROM pg_catalog.pg_default_acl d CROSS JOIN LATERAL pg_catalog.aclexplode(d.defaclacl) a
  WHERE pg_catalog.pg_get_userbyid(d.defaclrole)='control_plane_owner' AND d.defaclobjtype='r'
   AND pg_catalog.pg_get_userbyid(a.grantee)='kodex_backup_reader'
   AND (d.defaclnamespace<>'control_plane'::regnamespace OR a.privilege_type<>'SELECT' OR a.is_grantable
        OR pg_catalog.pg_get_userbyid(a.grantor)<>'control_plane_owner')) THEN
  RAISE EXCEPTION 'workspace backup default authority changed';
 END IF;
END $backup_acl$;
""" % ",".join(repr(x) for x in TABLES)
    normalize = header + """DO $backup_predecessor$
BEGIN
 IF (SELECT max(version_id) FROM public.goose_db_version WHERE is_applied) IS DISTINCT FROM 20261026001500::bigint
 OR pg_catalog.to_regclass('control_plane.session_workspace_volumes') IS NOT NULL
 OR NOT EXISTS(SELECT 1 FROM pg_catalog.pg_default_acl d CROSS JOIN LATERAL pg_catalog.aclexplode(d.defaclacl) a
   WHERE d.defaclrole='control_plane_owner'::regrole AND d.defaclnamespace='control_plane'::regnamespace
    AND d.defaclobjtype='r' AND a.grantee='kodex_backup_reader'::regrole AND a.privilege_type='SELECT' AND NOT a.is_grantable) THEN
  RAISE EXCEPTION 'workspace backup maintenance requires exact015 and configured backup defaults';
 END IF;
END $backup_predecessor$;
""" + acl_check + """REVOKE SELECT ON control_plane.session_workspace_storage,control_plane.session_archives,control_plane.session_archive_tasks FROM kodex_backup_reader RESTRICT;
ALTER DEFAULT PRIVILEGES FOR ROLE control_plane_owner IN SCHEMA control_plane REVOKE SELECT ON TABLES FROM kodex_backup_reader RESTRICT;
""" + workspace
    values = {"normalize.sql": normalize}
    for version, guards in [(20261026001500, workspace), (20261026001600, workspace + checkpoint),
                            (20261026001700, workspace + checkpoint + volume),
                            (20261027000100, workspace + checkpoint + volume), (20261028000100, restore)]:
        # Повторный restore сначала снимает только уже проверенный SELECT в своей транзакции,
        # проверяет настоящий frozen catalog и затем возвращает grants. Ошибка откатывает всё.
        check_version = "DO $backup_version$ BEGIN IF (SELECT max(version_id) FROM public.goose_db_version WHERE is_applied) IS DISTINCT FROM %d::bigint THEN RAISE EXCEPTION 'workspace backup restore version changed'; END IF; END $backup_version$;\n" % version
        count = 3 if version <= 20261026001600 else 6
        tables = ",".join("control_plane." + name for name in TABLES[:count])
        values[f"restore_{version}.sql"] = header + check_version + acl_check + f"REVOKE SELECT ON {tables} FROM kodex_backup_reader RESTRICT;\n" + guards + f"GRANT SELECT ON {tables} TO kodex_backup_reader;\nALTER DEFAULT PRIVILEGES FOR ROLE control_plane_owner IN SCHEMA control_plane GRANT SELECT ON TABLES TO kodex_backup_reader;\n"
    return values, generate_proofs(acl_check, restore_proof)


def generate_proofs(acl_check, restore):
    """Проверяет actual backup authority до inverse ACL-проекции frozen catalogs."""
    predecessor = frozen("session-workspace-migration-postcondition.sql", "1fd005d4bd318a82eb4d5130588692f5afb7f9263fc329b346ba3ef75b87f44f")
    idle = frozen("workspace-restore-migration-precondition.sql", "cfbc7b55f69f8dfa6b756701cc9e32d7584b82a47ee15f21527be2e6007712c2").split("    'oldClaimSessions',", 1)[1].split("\n\n))::text", 1)[0]
    boundary = """DO $backup_reader_catalog$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_catalog.pg_roles WHERE rolname='kodex_backup_reader'
  AND rolcanlogin AND NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole AND NOT rolinherit AND NOT rolreplication AND rolbypassrls)
 OR EXISTS(SELECT 1 FROM pg_catalog.pg_auth_members m JOIN pg_catalog.pg_roles r ON r.oid=m.member WHERE r.rolname='kodex_backup_reader')
 OR NOT EXISTS(SELECT 1 FROM pg_catalog.pg_default_acl d CROSS JOIN LATERAL pg_catalog.aclexplode(d.defaclacl) a
  WHERE d.defaclrole='control_plane_owner'::regrole AND d.defaclnamespace='control_plane'::regnamespace
   AND d.defaclobjtype='r' AND a.grantee='kodex_backup_reader'::regrole AND a.privilege_type='SELECT' AND NOT a.is_grantable)
 OR EXISTS(SELECT 1 FROM pg_catalog.pg_class r WHERE r.oid IN (TARGETS)
  AND NOT EXISTS(SELECT 1 FROM pg_catalog.aclexplode(r.relacl) a
   WHERE a.grantee='kodex_backup_reader'::regrole AND a.privilege_type='SELECT' AND NOT a.is_grantable)) THEN
  RAISE EXCEPTION 'workspace backup reader catalog changed';
 END IF;
END $backup_reader_catalog$;
""" + acl_check
    output = {}
    for name, source, count, old, new in [
        ("workspace-backup-migration-precondition.sql", predecessor, 3, "SESSION_WORKSPACE_V2", "WORKSPACE_BACKUP_PREDECESSOR_V1"),
        ("workspace-backup-migration-postcondition.sql", restore, 6, "WORKSPACE_RESTORE_V2", "WORKSPACE_RESTORE_BACKUP_V1"),
    ]:
        targets = ",".join("'control_plane." + table + "'::regclass" for table in TABLES[:count])
        marker = "SET LOCAL lock_timeout = '500ms';\n"
        assert source.count(marker) == 1
        source = source.replace(marker, marker + boundary.replace("TARGETS", targets))
        occurrences = 0
        for expression in ["aclexplode(coalesce(r.relacl,acldefault('r',r.relowner)))",
                           "pg_catalog.aclexplode(coalesce(r.relacl,pg_catalog.acldefault('r',r.relowner)))"]:
            occurrences += source.count(expression)
            source = source.replace(expression, "(SELECT * FROM " + expression + " backup_acl WHERE pg_catalog.pg_get_userbyid(backup_acl.grantee) IS DISTINCT FROM 'kodex_backup_reader')")
        assert occurrences == (1 if count == 3 else 3), "backup ACL projection shape changed"
        marker = " 'profile','" + old + "',"
        assert source.count(marker) == 1
        extension = " 'profile','" + new + "',\n 'backupReaderCatalog',true,"
        if count == 3:
            extension += "\n 'database',current_database(),\n 'workspaceBackupSafeToMigrate',NOT EXISTS(SELECT 1 FROM control_plane.session_workspace_storage) AND to_regclass('control_plane.session_workspace_volumes') IS NULL,\n    'oldClaimSessions'," + idle + ","
        source = source.replace(marker, extension)
        output[name] = "-- Generated: generate-workspace-backup-compatibility.py; actual backup ACL проверяется до inverse projection.\n" + source
    targets = ",".join("pg_catalog.to_regclass('control_plane." + table + "')" for table in TABLES)
    output["workspace-backup-acl-readback.sql"] = """-- Generated: generate-workspace-backup-compatibility.py; только metadata backup ACL.
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='5s';
SET LOCAL lock_timeout='500ms';
""" + boundary.replace("TARGETS", targets) + """DO $backup_shape$
DECLARE version bigint; targets integer;
BEGIN
 SELECT max(version_id) INTO version FROM public.goose_db_version WHERE is_applied;
 SELECT count(*) INTO targets FROM pg_catalog.pg_class r WHERE r.oid IN (TARGETS);
 IF version IS NULL OR version NOT IN(20261026001500,20261026001600,20261026001700,20261027000100,20261028000100)
 OR targets<>(CASE WHEN version IN(20261026001500,20261026001600) THEN 3 ELSE 6 END) THEN
  RAISE EXCEPTION 'workspace backup restore shape changed';
 END IF;
END $backup_shape$;
SELECT json_build_object('readOnly',current_setting('transaction_read_only'),'database',current_database(),
 'gooseVersion',(SELECT max(version_id) FROM public.goose_db_version WHERE is_applied),'backupReaderCatalog',true);
ROLLBACK;
""".replace("TARGETS", targets)
    return output


def main():
    """Проверяет воспроизводимость либо записывает закрытый набор SQL CLI."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    OUTPUT.mkdir(exist_ok=True)
    sql, proofs = generate()
    for directory, values in [(OUTPUT, sql), (DIRECTORY, proofs)]:
        for name, body in values.items():
            path = directory / name
            if args.check:
                assert path.read_text() == body, "workspace backup SQL is stale"
            else:
                path.write_text(body)


if __name__ == "__main__":
    main()
