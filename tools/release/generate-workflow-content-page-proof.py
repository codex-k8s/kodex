#!/usr/bin/env python3
"""Генерирует forward032 и самостоятельные exact schema proofs для #1975.

Снятый каталог031 остаётся неизменным. Ожидаемый каталог032 отличается только
телом одной функции; native upgrade обязан независимо сверить реальный каталог.
"""

import argparse
import copy
import hashlib
import json
import re
import importlib.util
from pathlib import Path

DIRECTORY = Path(__file__).resolve().parent
CLI = DIRECTORY.parent.parent / "services/internal/control-plane/cmd/cli"
VERSION = "20261030000100"
PREVIOUS = "20261029000300"
SIGNATURE = "control_plane.workflow_pdlc_result_valid(jsonb,jsonb,jsonb[])"


def predecessor_module():
    """Использует прежнюю чистую генерацию guard без исполнения credential snippets."""
    spec = importlib.util.spec_from_file_location("pdlc_proof", DIRECTORY / "generate-workflow-pdlc-proof.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def main():
    """Фиксирует один exact diff тела и проверяет воспроизводимость всех артефактов."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    predecessor = predecessor_module()
    old, _, old_digest = predecessor.catalog()
    source = (CLI / "migrations" / (PREVIOUS + "_workflow_pdlc_evidence.sql")).read_text()
    matches = re.findall(r"CREATE FUNCTION control_plane\.workflow_pdlc_result_valid\(.*?\n\$\$;", source, re.S)
    assert len(matches) == 1
    definition = matches[0]
    assert definition.count("'max_bytes'") == 3
    assert definition.count("v_max NOT BETWEEN 1 AND 16384") == 1
    definition = definition.replace("CREATE FUNCTION", "CREATE OR REPLACE FUNCTION", 1).replace("'max_bytes'", "'maximum_bytes'").replace("v_max NOT BETWEEN 1 AND 16384", "v_max NOT BETWEEN 4 AND 16384")
    body = definition.split("AS $$", 1)[1].rsplit("$$;", 1)[0]
    expected = copy.deepcopy(old)
    expected["functions"][SIGNATURE]["sourceMD5"] = hashlib.md5(body.encode()).hexdigest()
    expected["functions"][SIGNATURE]["sourceSHA256"] = hashlib.sha256(body.encode()).hexdigest()
    canonical = json.dumps(expected, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    digest = hashlib.sha256(canonical.encode()).hexdigest()
    guard = predecessor.catalog_guard
    migration = """-- Generated: generate-workflow-content-page-proof.py; #1975.
-- +goose Up
SET LOCAL lock_timeout='8s';
SET LOCAL statement_timeout='30s';
-- +goose StatementBegin
DO $predecessor$
BEGIN
 IF (SELECT max(version_id) FROM public.goose_db_version WHERE is_applied) IS DISTINCT FROM 20261029000300::bigint THEN
    RAISE EXCEPTION 'workflow content page requires exact migration031';
 END IF;
END $predecessor$;
-- +goose StatementEnd
SET ROLE control_plane_owner;
-- +goose StatementBegin
""" + guard(old) + "-- +goose StatementEnd\n-- +goose StatementBegin\n" + definition + "\n-- +goose StatementEnd\nRESET ROLE;\n-- +goose Down\n-- +goose StatementBegin\nDO $$ BEGIN RAISE EXCEPTION 'workflow content page migration is forward-only'; END $$;\n-- +goose StatementEnd\n"
    outputs = {CLI / "migrations" / (VERSION + "_workflow_content_page.sql"): migration}
    idle_source = (DIRECTORY / "workflow-pdlc-migration-precondition.sql").read_text()
    idle = "\n 'oldClaimSessions'," + idle_source.split(" 'oldClaimSessions',", 1)[1].split(" 'githubAutonomousWrites',", 1)[0]
    assert "activeRuntimeRuns" in idle and "WAITING_HUMAN" in idle
    for backup in (False, True):
        base = "workflow-pdlc-" + ("backup-" if backup else "") + "migration-postcondition.sql"
        old_proof = (DIRECTORY / base).read_text()
        old_profile = "GITHUB_PDLC_" + ("BACKUP_" if backup else "") + "V1"
        for before in (False, True):
            profile = "WORKFLOW_CONTENT_PAGE_" + ("BACKUP_" if backup else "") + ("PREDECESSOR_V1" if before else "V1")
            proof = old_proof
            if before:
                fields = " 'database',current_database()," + idle + " 'workflowContentPagePredecessor',true,\n"
            else:
                proof = proof.replace(guard(old), guard(expected), 1)
                assert guard(expected) in proof
                proof = proof.replace(old_digest, digest)
                fields = " 'workflowContentPageCatalog',true,\n"
            marker = " 'profile','" + old_profile + "',"
            assert proof.count(marker) == 1
            proof = proof.replace(marker, " 'profile','" + profile + "',\n" + fields, 1)
            outputs[DIRECTORY / ("workflow-content-page-" + ("backup-" if backup else "") + "migration-" + ("precondition" if before else "postcondition") + ".sql")] = "-- Generated: generate-workflow-content-page-proof.py; frozen031 сохранён.\n" + proof
    restore = (CLI / "workspace_backup_sql" / ("restore_" + PREVIOUS + ".sql")).read_text()
    assert restore.count(PREVIOUS) == 1 and restore.count(guard(old)) == 1
    outputs[CLI / "workspace_backup_sql" / ("restore_" + VERSION + ".sql")] = restore.replace(PREVIOUS, VERSION).replace(guard(old), guard(expected), 1)
    outputs[DIRECTORY / "workflow-content-page-catalog.gen.mjs"] = "// Generated: generate-workflow-content-page-proof.py; expected032 catalog.\nexport const workflowContentPageCatalogDigest = '" + digest + "';\n"
    outputs[DIRECTORY / "workflow-content-page-schema-manifest.json"] = json.dumps({"kind": "EXPECTED_SCHEMA_CONTRACT", "version": VERSION, "predecessorCatalogDigest": old_digest, "catalogDigest": digest, "migrationSHA256": hashlib.sha256(migration.encode()).hexdigest(), "catalog": expected}, ensure_ascii=False, indent=2) + "\n"
    for path, content in outputs.items():
        if args.check:
            assert path.read_text() == content, "content page generated file differs"
        else:
            path.write_text(content)


if __name__ == "__main__":
    main()

