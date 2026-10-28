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
    return values


def main():
    """Проверяет воспроизводимость либо записывает закрытый набор SQL CLI."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    OUTPUT.mkdir(exist_ok=True)
    for name, body in generate().items():
        path = OUTPUT / name
        if args.check:
            assert path.read_text() == body, "workspace backup SQL is stale"
        else:
            path.write_text(body)


if __name__ == "__main__":
    main()
