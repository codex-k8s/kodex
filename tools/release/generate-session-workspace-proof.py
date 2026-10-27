#!/usr/bin/env python3
"""Создаёт отдельный015 proof полного014 inverse projection и точного workspace.

Исходный014 SQL не меняется. Проекция явно вложена в predecessorProjection;
реальные версия и полный граф возвращаются отдельно. Каталог нового объекта
проверяется до проекции в том же read-only snapshot.
"""

import argparse
import hashlib
from pathlib import Path

DIRECTORY = Path(__file__).resolve().parent
BASE_SHA256 = "0b7c5563a78b800d7fc648e0ee6a7caa48c22da588eb4738782fa63fce44e76c"
WORKSPACE = "'control_plane.session_workspace_storage'::regclass"
ACTUAL_CTES = """workspace_nodes(id) AS (
 SELECT 'control_plane.projects'::regclass::oid UNION
 SELECT c.conrelid FROM workspace_nodes n JOIN pg_constraint c ON c.confrelid=n.id
 WHERE c.contype='f' AND c.connamespace='control_plane'::regnamespace
), workspace_node_proof AS (
 SELECT count(*) AS count,md5(string_agg(n.nspname||'.'||r.relname,E'\\n' ORDER BY n.nspname,r.relname)) AS digest
 FROM workspace_nodes JOIN pg_class r ON r.oid=workspace_nodes.id JOIN pg_namespace n ON n.oid=r.relnamespace
), workspace_edge_proof AS (
 SELECT count(*) AS count,md5(string_agg(cn.nspname||'.'||cc.relname||'|'||c.conname||'|'||pn.nspname||'.'||pc.relname||'|'||c.confdeltype::text,
 E'\\n' ORDER BY cn.nspname,cc.relname,c.conname)) AS digest
 FROM pg_constraint c JOIN pg_class cc ON cc.oid=c.conrelid JOIN pg_namespace cn ON cn.oid=cc.relnamespace
 JOIN pg_class pc ON pc.oid=c.confrelid JOIN pg_namespace pn ON pn.oid=pc.relnamespace
 WHERE c.contype='f' AND c.conrelid IN(SELECT id FROM workspace_nodes) AND c.confrelid IN(SELECT id FROM workspace_nodes)
), workspace_guards AS (
 SELECT format('IF v_nodes <> %s OR v_node_fingerprint <> %L THEN',n.count,n.digest) AS nodes,
 format('IF v_edges <> %s OR v_edge_fingerprint <> %L THEN',e.count,e.digest) AS edges
 FROM workspace_node_proof n CROSS JOIN workspace_edge_proof e
), """


def replace_exact(source: str, old: str, new: str, count: int = 1) -> str:
    """Проверяет точное число преобразований перед выпуском immutable SQL."""
    assert source.count(old) == count, "workspace predecessor shape changed"
    return source.replace(old, new)


def inverse(expression: str, edge_cte: str) -> str:
    """Восстанавливает только два точных graph guard фрагмента для inverse source proof."""
    return (
        f"replace(replace({expression},(SELECT nodes FROM workspace_guards),"
        "'IF v_nodes <> 105 OR v_node_fingerprint <> ''f51a0de25bd942a250997775617eeaee'' THEN'),"
        "(SELECT edges FROM workspace_guards),"
        f"format('IF v_edges <> 287 OR v_edge_fingerprint <> %L THEN',(SELECT digest FROM {edge_cte})))"
    )


def compose(base: str, catalog: str) -> str:
    """Сохраняет полный014 proof и отдельно доказывает actual015 graph/source/catalog."""
    source = replace_exact(base, "WITH RECURSIVE runtime_members", catalog + "\nWITH RECURSIVE " + ACTUAL_CTES + "runtime_members")
    # Только два явно названных predecessor graph CTE; actual graph выше не фильтруется.
    for name in ("nodes", "evidence_nodes"):
        needle = f"SELECT c.conrelid FROM {name} n JOIN pg_constraint c ON c.confrelid=n.relation_id\n WHERE c.contype='f' AND c.connamespace='control_plane'::regnamespace"
        source = replace_exact(source, needle, needle + f" AND c.conrelid<>{WORKSPACE}")
    source = replace_exact(source, "SELECT pg_get_functiondef('control_plane.purge_project_database(uuid,uuid,text)'::regprocedure) AS definition",
                           "SELECT " + inverse("pg_get_functiondef('control_plane.purge_project_database(uuid,uuid,text)'::regprocedure)", "edge_proof") + " AS definition")
    for expression, cte in (("function_row.prosrc", "edge_proof"), ("p.prosrc", "evidence_edge_proof")):
        needle = f"md5(replace({expression},format('IF v_edges <> 287"
        source = replace_exact(source, needle, f"md5(replace({inverse(expression, cte)},format('IF v_edges <> 287")
    source = replace_exact(source, "FROM public.goose_db_version)", "FROM public.goose_db_version WHERE version_id<20261026001500)", 2)
    tail = "SELECT (predecessor_proof.proof || evidence_proof.proof)::text\nFROM predecessor_proof CROSS JOIN evidence_proof;"
    final = """SELECT jsonb_build_object(
 'profile','SESSION_WORKSPACE_V2',
 'readOnly',current_setting('transaction_read_only'),
 'gooseVersion',(SELECT max(version_id) FILTER(WHERE is_applied) FROM public.goose_db_version),
 'workspaceCatalog',true,
 'purgeNodes',(SELECT count FROM workspace_node_proof),
 'purgeEdges',(SELECT count FROM workspace_edge_proof),
 'purgeNodeFingerprint',(SELECT digest FROM workspace_node_proof),
 'purgeEdgeFingerprint',(SELECT digest FROM workspace_edge_proof),
 'workspacePurgeGuard',EXISTS(SELECT 1 FROM pg_proc p CROSS JOIN workspace_guards g
   WHERE p.oid='control_plane.purge_project_database(uuid,uuid,text)'::regprocedure
     AND regexp_count(p.prosrc,'IF v_nodes <>')=1 AND regexp_count(p.prosrc,'IF v_edges <>')=1
     AND position(g.nodes IN p.prosrc)>0 AND position(g.edges IN p.prosrc)>0),
 'predecessorProjection',predecessor_proof.proof || evidence_proof.proof
)::text FROM predecessor_proof CROSS JOIN evidence_proof;"""
    return "-- Generated: generate-session-workspace-proof.py; actual015 и inverse014 не смешиваются.\n" + replace_exact(source, tail, final)


def main() -> None:
    """Отказывает при drift источника либо невоспроизводимом SQL без изменения predecessor."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    arguments = parser.parse_args()
    base = (DIRECTORY / "workflow-review-qa-evidence-migration-postcondition.sql").read_bytes()
    assert hashlib.sha256(base).hexdigest() == BASE_SHA256, "frozen014 proof changed"
    catalog = (DIRECTORY / "session-workspace-catalog-guard.sql").read_text()
    result = compose(base.decode(), catalog)
    output = DIRECTORY / "session-workspace-migration-postcondition.sql"
    if arguments.check:
        assert output.read_text() == result, "workspace proof drift"
    else:
        output.write_text(result)


if __name__ == "__main__":
    main()
