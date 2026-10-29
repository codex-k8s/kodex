#!/usr/bin/env python3
"""Сохраняет exact029 и добавляет закрытый admission GitHub6.3 без новых таблиц."""

import argparse
import hashlib
import json
import re
from pathlib import Path

DIRECTORY = Path(__file__).resolve().parent
ROOT = DIRECTORY.parent.parent
MIGRATIONS = ROOT / "services/internal/control-plane/cmd/cli/migrations"
VERSION = "20261029000200"
CURRENT_DIGEST = "72833e586647539dc1b33bbefa80605c22c6847cc7689fe0a626e28d64c55af5"
PREVIOUS_DIGEST = "96809e1f78320bc5788eb0e39c672818d8b37d03efbf91f1f083936161eb7a13"
OLD_OPERATIONS = ("github.issue.comment.create", "github.issue.comment.update", "github.pull_request.create", "github.pull_request.update", "github.pull_request.review.create")
NEW_OPERATIONS = OLD_OPERATIONS + ("github.issue.create", "github.issue.update", "github.repository.content.create", "github.repository.content.update", "github.pull_request.comment.create", "github.pull_request.comment.update", "github.pull_request.ready")


def literal(value: str) -> str:
    """Кодирует статические repo-owned SQL metadata, без пользовательских payload."""
    return "'" + value.replace("'", "''") + "'"


def approval_guard() -> tuple[str, str, str, str]:
    """Расширяет только закрытый allowlist, сохраняя полный остальной constraint/trigger."""
    source = (MIGRATIONS / "20261004000400_integration_grant_approval_policy.sql").read_text()
    old = re.findall(r"FUNCTION control_plane.protect_email_mailbox_gate\(\).*?AS \$\$(.*?)\$\$;", source, re.S)
    assert len(old) == 1 and hashlib.md5(old[0].encode()).hexdigest() == "2f240b237995bcbb71eb7a5888db344f"
    before = "'github.issue.comment.create','github.issue.comment.update','github.pull_request.create',\n                                 'github.pull_request.update','github.pull_request.review.create'"
    assert old[0].count(before) == 1, "GitHub PR write guard predecessor changed"
    body = old[0].replace(before, ",".join(literal(x) for x in NEW_OPERATIONS), 1)
    old_array = "ARRAY[" + ", ".join(literal(x) + "::text" for x in OLD_OPERATIONS) + "]"
    new_array = "ARRAY[" + ", ".join(literal(x) + "::text" for x in NEW_OPERATIONS) + "]"
    ddl = """-- +goose StatementBegin
DO $write_guard$
DECLARE
 v_before text := """ + literal(old_array) + ";\n v_after text := " + literal(new_array) + """;
 v_definition text;
BEGIN
 SELECT pg_get_constraintdef(oid) INTO STRICT v_definition FROM pg_constraint
 WHERE conrelid='control_plane.integration_invocations'::regclass AND conname='integration_invocations_approval_check';
 IF md5(v_definition)<>'87f6bc2dd7ef67bf3685d20f753fb474'
 OR (length(v_definition)-length(replace(v_definition,v_before,'')))/length(v_before)<>1
 OR NOT EXISTS(SELECT 1 FROM pg_proc WHERE oid='control_plane.protect_email_mailbox_gate()'::regprocedure
   AND md5(prosrc)='2f240b237995bcbb71eb7a5888db344f' AND pg_get_userbyid(proowner)='control_plane_owner' AND NOT prosecdef)
 THEN RAISE EXCEPTION 'GitHub autonomous write predecessor changed'; END IF;
 ALTER TABLE control_plane.integration_invocations DROP CONSTRAINT integration_invocations_approval_check;
 EXECUTE 'ALTER TABLE control_plane.integration_invocations ADD CONSTRAINT integration_invocations_approval_check ' || replace(v_definition,v_before,v_after);
END $write_guard$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.protect_email_mailbox_gate() RETURNS trigger LANGUAGE plpgsql AS $$""" + body + "$$;\n-- +goose StatementEnd\n"
    return ddl, body, old_array, new_array


def approval_proof(precondition: bool) -> str:
    """Сверяет весь constraint обратной точной проекцией и полный body/ACL trigger."""
    _, body, old_array, new_array = approval_guard()
    expression = "pg_get_constraintdef(c.oid)" if precondition else "replace(pg_get_constraintdef(c.oid)," + literal(new_array) + "," + literal(old_array) + ")"
    presence = "" if precondition else " AND (length(pg_get_constraintdef(c.oid))-length(replace(pg_get_constraintdef(c.oid)," + literal(new_array) + ",'')))/" + str(len(new_array)) + "=1"
    digest = "2f240b237995bcbb71eb7a5888db344f" if precondition else hashlib.md5(body.encode()).hexdigest()
    return """EXISTS(SELECT 1 FROM pg_constraint c WHERE c.conrelid='control_plane.integration_invocations'::regclass
 AND c.conname='integration_invocations_approval_check' AND c.contype='c' AND c.convalidated AND c.conislocal AND NOT c.connoinherit
 AND md5(""" + expression + """ )='87f6bc2dd7ef67bf3685d20f753fb474'""" + presence + """ )
 AND EXISTS(SELECT 1 FROM pg_proc p WHERE p.oid='control_plane.protect_email_mailbox_gate()'::regprocedure
 AND md5(p.prosrc)='""" + digest + """' AND pg_get_userbyid(p.proowner)='control_plane_owner'
 AND NOT p.prosecdef AND p.provolatile='v' AND p.prorettype='trigger'::regtype
 AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql') AND p.proconfig IS NULL
 AND has_function_privilege('control_plane_runtime',p.oid,'EXECUTE')
 AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
 WHERE a.grantee NOT IN (0,'control_plane_owner'::regrole,'control_plane_runtime'::regrole) OR a.is_grantable OR a.privilege_type<>'EXECUTE'))
 AND EXISTS(SELECT 1 FROM pg_trigger t WHERE t.tgrelid='control_plane.integration_invocations'::regclass
 AND t.tgfoid='control_plane.protect_email_mailbox_gate()'::regprocedure AND t.tgenabled='O'
 AND md5(pg_get_triggerdef(t.oid))='b99f51573b8e4a909423c4bd058b1002')"""


def frozen(path: Path, digest: str) -> str:
    """Читает только известные байты предшественника без исторических правок."""
    content = path.read_bytes()
    assert hashlib.sha256(content).hexdigest() == digest, "GitHub PR frozen predecessor changed"
    return content.decode()


def migration() -> tuple[str, str, str]:
    """Продлевает закрытый helper exact снимком6.2 и проверяет старый body до DDL."""
    source = (MIGRATIONS / "20261029000100_github_historical_grant_admission.sql").read_text()
    matches = re.findall(r"AS \$legacy\$(.*?)\$legacy\$;", source, re.S)
    assert len(matches) == 1, "GitHub PR predecessor helper shape changed"
    old = matches[0]
    assert hashlib.md5(old.encode()).hexdigest() == "03d434e634700cf4829bfac937192590", "GitHub PR predecessor helper changed"
    snapshot = frozen(ROOT / "libs/go/integrationpackage/compatibility/github-6.2.0.yaml", "fc21532a268f1474328e1819138bd8686d14aeaaa0fea9c0ff85902b48c5618a")
    catalog = []
    for block in re.findall(r"^    - key: github\..*?(?=^    - key: |\Z)", snapshot, re.M | re.S):
        item = {}
        for key in ("key", "name", "operation", "risk"):
            value = re.findall(r"^" + ("    - " if key == "key" else "      ") + key + r": ([^\n]+)$", block, re.M)
            assert len(value) == 1, "GitHub PR snapshot catalog shape changed"
            item[key] = value[0]
        catalog.append(item)
    assert len(catalog) == 42, "GitHub PR snapshot capability count changed"
    marker = "    ('6.2.0','" + PREVIOUS_DIGEST + "')\n) THEN CASE"
    assert old.count(marker) == 1 and old.count("    ELSE NULL::jsonb END ELSE NULL::jsonb END;") == 1
    new = old.replace(marker, marker.replace("\n) THEN CASE", ",\n    ('6.3.0','" + CURRENT_DIGEST + "')\n) THEN CASE"), 1)
    row = "    WHEN (p_current_version,p_current_digest)=('6.3.0','" + CURRENT_DIGEST + "') AND (p_version,p_digest)=('6.2.0','" + PREVIOUS_DIGEST + "') THEN $capabilities$" + json.dumps(catalog, ensure_ascii=False, separators=(",", ":")) + "$capabilities$::jsonb\n"
    new = new.replace("    ELSE NULL::jsonb END ELSE NULL::jsonb END;", row + "    ELSE NULL::jsonb END ELSE NULL::jsonb END;", 1)
    header = """-- Generated: generate-github-pr-proof.py; точные неизменяемые снимки, только вперёд.
-- +goose Up
SET ROLE control_plane_owner;
-- +goose StatementBegin
DO $predecessor$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_proc p WHERE p.oid='control_plane.integration_github_legacy_capabilities(text,text,text,text)'::regprocedure
   AND md5(p.prosrc)='03d434e634700cf4829bfac937192590'
   AND pg_get_userbyid(p.proowner)='control_plane_owner' AND NOT p.prosecdef)
 THEN RAISE EXCEPTION 'GitHub PR admission predecessor changed'; END IF;
END $predecessor$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.integration_github_legacy_capabilities(p_current_version text,p_current_digest text,p_version text,p_digest text)
RETURNS jsonb LANGUAGE sql IMMUTABLE SET search_path TO pg_catalog, control_plane
AS $legacy$"""
    return header + new + "$legacy$;\n-- +goose StatementEnd\n" + approval_guard()[0] + "RESET ROLE;\n", old, new


def proof(backup: bool, precondition: bool, old: str, new: str) -> str:
    """Проверяет новый actual helper до явной обратной проекции неизменённого029."""
    filename = "github-autonomous-" + ("backup-" if backup else "") + "migration-postcondition.sql"
    digest = "7c9fdd148e5b69b76ff74f29b01b92a204ed6b838269c7b819a0c85ac7d3dda0" if backup else "928e00424c2ee03baebe9051109d505e244068f89c846c6737600e385f34c1dd"
    source = frozen(DIRECTORY / filename, digest)
    marker = " 'profile','GITHUB_AUTONOMOUS_PDLC_" + ("BACKUP_" if backup else "") + "V1',"
    assert source.count(marker) == 1
    profile = "GITHUB_PR_COLLABORATION_" + ("BACKUP_" if backup else "") + ("PREDECESSOR_V1" if precondition else "V1")
    fields = " 'profile','" + profile + "',"
    fields += "\n '" + ("githubPRWritePredecessor" if precondition else "githubAutonomousWrites") + "'," + approval_proof(precondition) + ","
    if precondition:
        idle = frozen(DIRECTORY / "workspace-restore-migration-precondition.sql", "cfbc7b55f69f8dfa6b756701cc9e32d7584b82a47ee15f21527be2e6007712c2")
        idle = idle.split("    'oldClaimSessions',", 1)[1].split("\n\n))::text", 1)[0]
        fields += "\n 'database',current_database(),\n 'oldClaimSessions'," + idle + ","
    else:
        source = source.replace(" 'githubHistoricalCatalog',", " 'githubPRCatalog',", 1)
        old_hash, new_hash = hashlib.md5(old.encode()).hexdigest(), hashlib.md5(new.encode()).hexdigest()
        assert source.count(old_hash) == 1
        source = source.replace(old_hash, new_hash, 1)
    return "-- Generated: generate-github-pr-proof.py; exact029 и отдельный PR admission.\n" + source.replace(marker, fields, 1)


def main() -> None:
    """Записывает воспроизводимые файлы либо сверяет неизменённые текущие bytes."""
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    content, old, new = migration()
    outputs = {MIGRATIONS / (VERSION + "_github_pr_collaboration_admission.sql"): content}
    for backup in (False, True):
        for precondition in (False, True):
            name = "github-pr-" + ("backup-" if backup else "") + "migration-" + ("precondition" if precondition else "postcondition") + ".sql"
            outputs[DIRECTORY / name] = proof(backup, precondition, old, new)
    directory = MIGRATIONS.parent / "workspace_backup_sql"
    restore = frozen(directory / "restore_20261029000100.sql", "b1ae257ebd6890852ebc0b9f1825d0eacbafa1ac51acb940ce6073ef56424726")
    assert restore.count("20261029000100") == 1
    outputs[directory / ("restore_" + VERSION + ".sql")] = restore.replace("20261029000100", VERSION, 1).replace("generate-github-autonomous-proof.py", "generate-github-pr-proof.py", 1)
    for path, content in outputs.items():
        if args.check:
            assert path.read_text() == content, "GitHub PR generated file differs"
        else:
            path.write_text(content)


if __name__ == "__main__":
    main()
