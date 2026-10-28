#!/usr/bin/env python3
"""Формирует отдельный combined017+SSH proof; исторические verifier не меняются.

Полный фактический граф сохраняется. Только вложенная014 проекция исключает
четыре новые таблицы и ровно одну проверенную DEPLOYMENT-ветку guard.
"""

import argparse
import hashlib
import json
from pathlib import Path

DIRECTORY = Path(__file__).resolve().parent
ROOT = DIRECTORY.parent.parent
MIGRATIONS = ROOT / "services/internal/control-plane/cmd/cli/migrations"


def frozen(name: str, digest: str) -> str:
    """Не разрешает молча изменить входной исторический proof."""
    raw = (DIRECTORY / name).read_bytes()
    assert hashlib.sha256(raw).hexdigest() == digest, "combined predecessor changed"
    return raw.decode()


def exact(source: str, old: str, new: str, count: int = 1) -> str:
    """Проверяет число замен до получения воспроизводимого SQL."""
    assert source.count(old) == count, "combined predecessor shape changed"
    return source.replace(old, new)


def generate() -> dict[str, str]:
    """Собирает read-only catalog guard, полный postcondition и отдельный010 precondition."""
    source = frozen("workspace-checkpoint-migration-postcondition.sql", "76528e1cd7ef049f89de743094edabc56b06ae5790588938bfb67e4330724b2f")
    ssh = frozen("ssh-compose-migration-postcondition.sql", "ac3f6d6efc5cbf11d2c23941c255623c26599ac8ddff04853515724e897ddc62")
    pre = frozen("ssh-compose-migration-precondition.sql", "ed3df9c1fede8de706b18124db8a7aaaa8b0113032f3420fff142d7404e2dfb9")
    readback = (ROOT / "services/internal/control-plane/cmd/cli/testdata/sql/volume_catalog_readback.sql").read_text().strip()
    expected = json.loads((DIRECTORY / "workspace-volume-catalog-fingerprints.json").read_text())
    query = exact(readback, "\n);", "\n) INTO v_actual;")
    guard = """-- Точный017 каталог, включая parent-side FK и origin-close triggers; business rows не читаются.
DO $volume_catalog$
DECLARE v_actual jsonb; v_path text;
BEGIN
 v_path:=current_setting('search_path'); PERFORM set_config('search_path','pg_catalog',true);
""" + query + "\n PERFORM set_config('search_path',v_path,true);\n IF v_actual IS DISTINCT FROM '" + json.dumps(expected, sort_keys=True, separators=(",", ":")) + "'::jsonb THEN\n RAISE EXCEPTION 'workspace volume catalog changed';\n END IF;\nEND $volume_catalog$;\n"
    migration = (MIGRATIONS / "20261027000100_ssh_compose_runtime.sql").read_text()
    start = "v_deployment:=$deployment$"
    assert migration.count(start) == 1, "SSH evidence source changed"
    deployment = migration.split(start)[1].split("$deployment$;")[0]
    extension = deployment + "\n                    "
    assert "$combined_deployment$" not in extension
    projection = """-- Actual SSH extension проверяется целиком до обратной014 проекции.
DO $combined_effect$
DECLARE v_source text; v_extension text:=$combined_deployment$""" + extension + """$combined_deployment$;
BEGIN
 SELECT prosrc INTO STRICT v_source FROM pg_catalog.pg_proc
 WHERE oid='control_plane.guard_workflow_effect_records()'::regprocedure;
 IF (length(v_source)-length(replace(v_source,v_extension,'')))/length(v_extension)<>1
 OR md5(replace(v_source,v_extension,''))<>'7010547e11bc8d1e8f67ac56feaba030' THEN
 RAISE EXCEPTION 'combined SSH evidence guard changed';
 END IF;
END $combined_effect$;

"""
    source = exact(source, "-- Frozen016 archive/task metadata;", guard + "\n" + projection + "-- Frozen016 archive/task metadata;")
    source = exact(source, "AND c.conrelid<>'control_plane.session_workspace_storage'::regclass", "AND c.conrelid NOT IN ('control_plane.session_workspace_storage'::regclass,'control_plane.session_workspace_volumes'::regclass,'control_plane.session_workspace_volume_tasks'::regclass,'control_plane.session_workspace_volume_attempts'::regclass)", 2)
    normalized = "CASE WHEN p.oid='control_plane.guard_workflow_effect_records()'::regprocedure THEN replace(p.prosrc,$combined_deployment$" + extension + "$combined_deployment$,'') ELSE p.prosrc END AS prosrc"
    source = exact(source, "), function_rows AS (\n SELECT expected.*,p.oid,p.prosrc,p.proowner", "), function_rows AS (\n SELECT expected.*,p.oid," + normalized + ",p.proowner")
    source = exact(source, "SELECT e.*,p.oid,p.prosrc,p.proowner", "SELECT e.*,p.oid," + normalized + ",p.proowner")
    ssh_ctes = ssh.split("), ssh_columns(", 1)[1].split("\nSELECT jsonb_build_object(", 1)[0]
    source = exact(source, "WITH RECURSIVE workspace_nodes(id) AS (", "WITH RECURSIVE ssh_columns(" + ssh_ctes + ", workspace_nodes(id) AS (")
    ssh_fields = ssh.split(" 'sshComposeProfile',", 1)[1].split(" 'workflowColumns',", 1)[0]
    source = exact(source, " 'workspaceCatalog',true,", " 'workspaceCatalog',true,\n 'volumeCatalog',true,\n 'sshComposeProfile'," + ssh_fields)
    source = exact(source, "'profile','WORKSPACE_CHECKPOINT_V2'", "'profile','WORKSPACE_VOLUME_SSH_V1'")
    source = exact(source, "-- Generated: generate-workspace-checkpoint-proof.py; actual016 и inverse014 не смешиваются.", "-- Generated: generate-workspace-volume-ssh-proof.py; actual017+SSH и inverse014 не смешиваются.")
    # Остальные обязательные поля baseline010 и счётчики активной работы сохранены побайтно.
    absent = """ 'workspaceVolumeSSHAbsent',NOT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname='control_plane' AND c.relname IN ('session_workspace_storage','session_workspace_volumes','session_workspace_volume_tasks','session_workspace_volume_attempts'))
 AND NOT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='control_plane.run_nodes'::regclass AND attnum>0 AND NOT attisdropped
 AND attname IN ('owner_rework_gate_id','owner_rework_origin_node_id','workflow_rework_gate_id'))
 AND NOT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid IN ('control_plane.session_archives'::regclass,'control_plane.session_archive_tasks'::regclass)
 AND attnum>0 AND NOT attisdropped AND attname='workspace_id')
 AND NOT EXISTS(SELECT 1 FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='control_plane'
 AND p.proname IN ('legacy_workflow_step_origin','workflow_exception_context_node','workflow_evidence_intent_valid','guard_workspace_volume_custody')),
"""
    pre = exact(pre, " 'sshComposeAbsent',", absent + " 'sshComposeAbsent',")
    return {"workspace-volume-catalog-guard.sql": guard,
            "workspace-volume-ssh-migration-postcondition.sql": source,
            "workspace-volume-ssh-migration-precondition.sql": pre}


def main() -> None:
    """Записывает отдельные файлы либо проверяет их точное воспроизведение."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    for name, content in generate().items():
        if args.check:
            assert (DIRECTORY / name).read_text() == content, "combined proof drift"
        else:
            (DIRECTORY / name).write_text(content)


if __name__ == "__main__":
    main()
