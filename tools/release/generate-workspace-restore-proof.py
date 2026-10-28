#!/usr/bin/env python3
"""Формирует отдельный restore proof с точной обратной проекцией017+SSH.

Исторические проверки не меняются. Перед обратной проекцией проверяются
полное тело нового guard, новый индекс и единственное расширение CHECK.
"""

import argparse
import hashlib
import re
from pathlib import Path

DIRECTORY = Path(__file__).resolve().parent
ROOT = DIRECTORY.parent.parent
MIGRATIONS = ROOT / "services/internal/control-plane/cmd/cli/migrations"


def exact(source: str, old: str, new: str, count: int = 1) -> str:
    """Меняет только заранее проверенную единственную форму frozen source."""
    assert source.count(old) == count, "restore predecessor shape changed"
    return source.replace(old, new)


def guard_body(name: str) -> str:
    """Возвращает полное тело конкретного versioned trigger без смысловой нормализации."""
    source = (MIGRATIONS / name).read_text()
    matches = re.findall(r"CREATE (?:OR REPLACE )?FUNCTION control_plane\.guard_workspace_archive_task\(\) RETURNS trigger\nLANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog AS \$\$(.*?)\$\$;", source, re.S)
    assert len(matches) == 1, "restore guard shape changed"
    return matches[0]


def generate() -> dict[str, str]:
    """Сохраняет весь actual graph и старый каталог после exact inverse трёх изменений."""
    old = guard_body("20261026001600_workspace_checkpoint_tasks.sql")
    new = guard_body("20261028000100_workspace_restore_owner.sql")
    source = (DIRECTORY / "workspace-volume-ssh-migration-postcondition.sql").read_text()
    assert hashlib.sha256(source.encode()).hexdigest() == "72a55bbdb046f1cc534e8fe4d940684ecc4b28b90b8776ec358a8665ae19dea1", "restore frozen predecessor changed"
    # Только source hash подменяется историческим значением, после проверки actual нового guard.
    old_hash, new_hash = (hashlib.md5(body.encode()).hexdigest() for body in (old, new))
    normalize = "CASE WHEN p.oid='control_plane.guard_workspace_archive_task()'::regprocedure THEN '" + old_hash + "' ELSE md5(p.prosrc) END"
    source = exact(source, "md5(p.prosrc),pg_get_userbyid(p.proowner)", normalize + ",pg_get_userbyid(p.proowner)", 2)
    index = "CREATE UNIQUE INDEX session_archive_tasks_workspace_restore ON control_plane.session_archive_tasks USING btree (workspace_id, workspace_generation) WHERE ((workspace_id IS NOT NULL) AND (kind = 'RESTORE'::text))"
    source = exact(source, "FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid WHERE i.indrelid=r.oid)",
                   "FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid WHERE i.indrelid=r.oid AND c.relname<>'session_archive_tasks_workspace_restore')")
    # Удаляется ровно добавленный литерал из actual CHECK, остальные правила и ACL сохраняются.
    old_check = "pg_get_constraintdef(c.oid)) ORDER BY c.conname) FROM pg_constraint c WHERE c.conrelid='control_plane.session_workspace_storage'::regclass)"
    expression = "CASE WHEN c.conname='session_workspace_storage_block_reason_check' THEN replace(pg_get_constraintdef(c.oid),$literal$, 'RESTORE_UNCONFIRMED'::text$literal$,'') ELSE pg_get_constraintdef(c.oid) END"
    source = exact(source, old_check, expression + ") ORDER BY c.conname) FROM pg_constraint c WHERE c.conrelid='control_plane.session_workspace_storage'::regclass)")
    actual = """-- Проверка actual restore extension до любой inverse projection.
DO $restore_catalog$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_catalog.pg_proc p
  WHERE p.oid='control_plane.guard_workspace_archive_task()'::regprocedure
   AND md5(p.prosrc)='""" + new_hash + """')
 OR NOT EXISTS(SELECT 1 FROM pg_catalog.pg_index i JOIN pg_catalog.pg_class c ON c.oid=i.indexrelid
   WHERE c.relname='session_archive_tasks_workspace_restore' AND i.indrelid='control_plane.session_archive_tasks'::regclass
    AND i.indisunique AND i.indisvalid AND i.indisready AND i.indislive
    AND pg_catalog.pg_get_indexdef(i.indexrelid)=$index$""" + index + """$index$)
 OR NOT EXISTS(SELECT 1 FROM pg_catalog.pg_constraint c WHERE c.conrelid='control_plane.session_workspace_storage'::regclass
   AND c.conname='session_workspace_storage_block_reason_check' AND c.contype='c' AND c.convalidated AND c.conenforced
   AND pg_catalog.pg_get_constraintdef(c.oid)=$check$CHECK ((block_reason = ANY (ARRAY[''::text, 'POD_MISSING'::text, 'POD_UNREACHABLE'::text, 'PVC_MISSING'::text, 'PVC_REPLACED'::text, 'OWNERSHIP_CHANGED'::text, 'WRITER_JOIN_UNCONFIRMED'::text, 'RESTORE_UNCONFIRMED'::text])))$check$) THEN
  RAISE EXCEPTION 'workspace restore catalog changed';
 END IF;
END $restore_catalog$;

"""
    source = exact(source, "-- Точный017 каталог,", actual + "-- Точный017 каталог,")
    source = exact(source, "'profile','WORKSPACE_VOLUME_SSH_V1'", "'profile','WORKSPACE_RESTORE_V2'")
    source = exact(source, " 'volumeCatalog',true,", " 'volumeCatalog',true,\n 'restoreCatalog',true,")
    source = "-- Generated: generate-workspace-restore-proof.py; actual restore и inverse017+SSH проверяются отдельно.\n" + source
    return {"workspace-restore-migration-postcondition.sql": source,
            "workspace-restore-migration-precondition.sql": (DIRECTORY / "workspace-volume-ssh-migration-precondition.sql").read_text()}


def main() -> None:
    """Проверяет воспроизводимость либо записывает новый отдельный proof."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    arguments = parser.parse_args()
    for name, source in generate().items():
        if arguments.check:
            assert (DIRECTORY / name).read_text() == source, "restore proof drift"
        else:
            (DIRECTORY / name).write_text(source)


if __name__ == "__main__":
    main()
