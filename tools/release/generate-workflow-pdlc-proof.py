#!/usr/bin/env python3
"""Добавляет exact031 proof поверх замороженного030 без изменения старых validators.

Manifest содержит только schema metadata, снятые public disposable PostgreSQL
сценарием с KODEX_PDLC_SCHEMA_CAPTURE=1. Бизнес-строки и credentials не читаются.
"""

import argparse
import hashlib
import json
import re
from pathlib import Path


DIRECTORY = Path(__file__).resolve().parent
ROOT = DIRECTORY.parent.parent
CLI = ROOT / "services/internal/control-plane/cmd/cli"
VERSION = "20261029000300"
MIGRATION = VERSION + "_workflow_pdlc_evidence.sql"
CHANGED = (
    "guard_workflow_effect_records", "guard_workflow_attempt_journal",
    "workflow_evidence_intent_valid", "workflow_evidence_retains_revision",
    "workflow_evidence_outcome_valid", "workflow_evidence_execution_assigned",
)
FROZEN = {
    "github-pr-migration-postcondition.sql": "e2e7c2baf434c5bca633583aac82124d832a17f37c6b2313acbd88256009e7c8",
    "github-pr-backup-migration-postcondition.sql": "eae233b6c8f6908b9166fe4ee55b4b376038451debf42f47167f177ab6a4d072",
    "workspace-restore-migration-precondition.sql": "cfbc7b55f69f8dfa6b756701cc9e32d7584b82a47ee15f21527be2e6007712c2",
}


def frozen(name: str) -> str:
    """Читает только заранее закреплённый repo-owned источник."""
    source = (DIRECTORY / name).read_text()
    assert hashlib.sha256(source.encode()).hexdigest() == FROZEN[name], "frozen predecessor changed"
    return source


def catalog() -> tuple[dict, list[str], str]:
    """Связывает снятый actual catalog с точными compiled migration bytes и14 новыми bodies."""
    data = json.loads((DIRECTORY / "workflow-pdlc-schema-manifest.json").read_text())
    source = (CLI / "migrations" / MIGRATION).read_text()
    assert data["migrationSHA256"] == hashlib.sha256(source.encode()).hexdigest(), "PDLC capture source changed"
    assert data["catalogQuerySHA256"] == hashlib.sha256((DIRECTORY / "workflow-pdlc-catalog-readback.sql").read_bytes()).hexdigest(), "PDLC catalog query capture changed"
    functions = data["catalog"]["functions"]
    assert data["catalog"]["functionCount"] == len(functions) == 20
    new = re.findall(r"CREATE FUNCTION control_plane\.(\w+)\(.*?\n.*?AS (\$[a-z_]*\$)(.*?)\2;", source, re.S)
    assert len(new) == 14 and len({name for name, _, _ in new}) == 14, "PDLC new function registry changed"
    for name, _, body in new:
        matches = [meta for signature, meta in functions.items() if signature.startswith("control_plane." + name + "(")]
        assert len(matches) == 1 and matches[0]["sourceSHA256"] == hashlib.sha256(body.encode()).hexdigest(), "PDLC function capture changed"
    assert {signature.split(".", 1)[1].split("(", 1)[0] for signature in functions} == set(CHANGED) | {row[0] for row in new}
    canonical = json.dumps(data["catalog"], ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    return data["catalog"], [row[0] for row in new], hashlib.sha256(canonical.encode()).hexdigest()


def catalog_guard(expected: dict) -> str:
    """Проверяет full bodies, signatures, argument/output metadata, ACL, triggers и bounds."""
    query = (DIRECTORY / "workflow-pdlc-catalog-readback.sql").read_text().rstrip()
    assert query.endswith(");") and query.count("\nSELECT jsonb_build_object(") == 1
    query = query[:-1] + " INTO v_actual;"
    literal = json.dumps(expected, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    return """-- Actual031 проверяется до исторической проекции, в том же snapshot.
DO $workflow_pdlc_catalog$
DECLARE v_actual jsonb;
BEGIN
""" + query + "\n IF v_actual IS DISTINCT FROM $catalog$" + literal + """$catalog$::jsonb THEN
    RAISE EXCEPTION 'workflow PDLC schema catalog changed';
 END IF;
END $workflow_pdlc_catalog$;
"""


def successor(source: str, metadata: dict) -> str:
    """Сверяет реальные031 bodies вместо inverse SSH body; остальные030 проверки сохраняет."""
    pattern = r"CASE WHEN p\.oid='control_plane\.guard_workflow_effect_records\(\)'::regprocedure THEN replace\(p\.prosrc,\$combined_deployment\$.*?\$combined_deployment\$,''\) ELSE p\.prosrc END"
    source, count = re.subn(pattern, "p.prosrc", source, flags=re.S)
    assert count == 2, "frozen effect projection changed"
    old = "md5(replace(v_source,v_extension,''))"
    assert source.count(old) == 1
    source = source.replace(old, "md5(v_source)", 1)
    for name in CHANGED:
        matches = re.findall(r"\('control_plane\." + name + r"\([^']*\)','([a-f0-9]{32})'", source)
        assert matches and len(set(matches)) == 1, "frozen function pin changed"
        actual = [value for signature, value in metadata["functions"].items() if signature.startswith("control_plane." + name + "(")]
        assert len(actual) == 1
        source = source.replace(matches[0], actual[0]["sourceMD5"])
    old = "CHECK(((jsonb_array_length(workflow_effect_intents)<=16)AND(jsonb_array_length(workflow_effect_rejoins)<=16)))"
    assert source.count(old) == 1
    return source.replace(old, old.replace("<=16", "<=64"))


def proof(backup: bool, before: bool, metadata: dict, names: list[str], digest: str) -> str:
    """Собирает самостоятельный read-only pre/post SQL без runtime include либо данных владельца."""
    source = frozen("github-pr-" + ("backup-" if backup else "") + "migration-postcondition.sql")
    marker = " 'profile','GITHUB_PR_COLLABORATION_" + ("BACKUP_" if backup else "") + "V1',"
    assert source.count(marker) == 1
    profile = "GITHUB_PDLC_" + ("BACKUP_" if backup else "") + ("PREDECESSOR_V1" if before else "V1")
    fields = " 'profile','" + profile + "',"
    if before:
        missing = ",".join("'" + name + "'" for name in names)
        fields += "\n 'workflowPDLCAbsent',NOT EXISTS(SELECT 1 FROM pg_proc WHERE pronamespace='control_plane'::regnamespace AND proname IN(" + missing + ")),"
        idle = frozen("workspace-restore-migration-precondition.sql").split("    'oldClaimSessions',", 1)[1].split("\n\n))::text", 1)[0]
        fields += "\n 'database',current_database(),\n 'oldClaimSessions'," + idle + ","
    else:
        source = successor(source, metadata)
        start = "SET LOCAL lock_timeout = '500ms';\n"
        assert source.count(start) == 1
        source = source.replace(start, start + catalog_guard(metadata), 1)
        fields += "\n 'workflowPDLCCatalog',true,\n 'workflowPDLCFunctions',14,\n 'workflowPDLCCatalogDigest','" + digest + "',"
    return "-- Generated: generate-workflow-pdlc-proof.py; exact031 без расширения030.\n" + source.replace(marker, fields, 1)


def restore(metadata: dict) -> str:
    """Добавляет031 catalog fence к неизменённой closed backup SELECT процедуре030."""
    source = (CLI / "workspace_backup_sql/restore_20261029000200.sql").read_text()
    assert hashlib.sha256(source.encode()).hexdigest() == "44315bd04de49e45339c42b5250e5f84cfb0f0e5af5e2747c44d02ef9dbc4bdb"
    assert source.count("20261029000200") == 1
    source = source.replace("20261029000200", VERSION, 1)
    old = "md5(replace(v_source,v_extension,''))<>'7010547e11bc8d1e8f67ac56feaba030'"
    assert source.count(old) == 1
    effect = metadata["functions"]["control_plane.guard_workflow_effect_records()"]["sourceMD5"]
    source = source.replace(old, "md5(v_source)<>'" + effect + "'", 1)
    marker = "GRANT SELECT ON control_plane.session_workspace_storage,"
    assert source.count(marker) == 1
    return "-- Generated: generate-workflow-pdlc-proof.py; exact backup restore031.\n" + source.replace(marker, catalog_guard(metadata) + "\n" + marker, 1)


def main() -> None:
    """Генерирует фиксированные SQL/JS либо проверяет committed bytes без изменений."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    metadata, names, digest = catalog()
    outputs = {}
    for backup in (False, True):
        for before in (False, True):
            name = "workflow-pdlc-" + ("backup-" if backup else "") + "migration-" + ("precondition" if before else "postcondition") + ".sql"
            outputs[DIRECTORY / name] = proof(backup, before, metadata, names, digest)
    outputs[CLI / "workspace_backup_sql" / ("restore_" + VERSION + ".sql")] = restore(metadata)
    outputs[DIRECTORY / "workflow-pdlc-catalog-guard.sql"] = "-- Generated: generate-workflow-pdlc-proof.py; metadata-only catalog fence.\n" + catalog_guard(metadata)
    outputs[DIRECTORY / "workflow-pdlc-catalog.gen.mjs"] = "// Generated: generate-workflow-pdlc-proof.py; fixed schema commitment.\nexport const workflowPDLCCatalogDigest = '" + digest + "';\n"
    for path, content in outputs.items():
        if args.check:
            assert path.read_text() == content, "PDLC generated file differs"
        else:
            path.write_text(content)


if __name__ == "__main__":
    main()
