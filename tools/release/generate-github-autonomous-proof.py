#!/usr/bin/env python3
"""Добавляет exact GitHub admission к неизменённой проверке workspace028."""

import argparse
import hashlib
import re
from pathlib import Path

DIRECTORY = Path(__file__).resolve().parent
ROOT = DIRECTORY.parent.parent
MIGRATIONS = ROOT / "services/internal/control-plane/cmd/cli/migrations"


def generate(backup: bool = False, precondition: bool = False) -> str:
    """Сохраняет прежние schema assertions и проверяет полные новые function bodies."""
    filename, digest, old_profile = (
        ("workspace-backup-migration-postcondition.sql", "8850531f3a7a712aa92822ba6cf863b7944ee804f66df5435684c10bbb9ba83a", "WORKSPACE_RESTORE_BACKUP_V1")
        if backup else ("workspace-restore-migration-postcondition.sql", "746b7cbf9e22ea84d7fd6cc7e3ab696f5d059307b2d5997a56cf3adad2807161", "WORKSPACE_RESTORE_V2")
    )
    source = (DIRECTORY / filename).read_text()
    assert hashlib.sha256(source.encode()).hexdigest() == digest, "GitHub frozen predecessor proof changed"
    migration = (MIGRATIONS / "20261029000100_github_historical_grant_admission.sql").read_text()
    predecessor = (MIGRATIONS / "20260908000300_email_legacy_package_admission.sql").read_text()
    helper = re.findall(r"AS \$legacy\$(.*?)\$legacy\$;", migration, re.S)
    admission = re.findall(r"AS \$\$(.*?)\$\$;", predecessor, re.S)
    replacements = re.findall(r"v_(before|after) text := E'((?:''|[^'])*)';", migration)
    assert len(helper) == len(admission) == 1 and len(replacements) == 2, "GitHub admission migration shape changed"
    fragments = {key: value.replace("''", "'").replace("\\n", "\n") for key, value in replacements}
    assert admission[0].count(fragments["before"]) == 1, "GitHub admission predecessor changed"
    modified = admission[0].replace(fragments["before"], fragments["after"])
    rows = (
        ("integration_github_legacy_capabilities(text,text,text,text)", helper[0], "i", "jsonb", ["p_current_version", "p_current_digest", "p_version", "p_digest"]),
        ("integration_grant_admission(uuid,uuid,uuid,text,text,text,text,text,text,text[])", modified, "s", "record", None),
    )
    checks = []
    for signature, body, volatility, result, names in rows:
        digest = hashlib.md5(body.encode()).hexdigest()
        checks.append("""EXISTS(SELECT 1 FROM pg_proc p WHERE p.oid='control_plane.""" + signature + """'::regprocedure
 AND md5(p.prosrc)='""" + digest + """' AND pg_get_userbyid(p.proowner)='control_plane_owner'
 AND NOT p.prosecdef AND p.provolatile='""" + volatility + """' AND p.prorettype='""" + result + """'::regtype
 AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='sql')
 AND p.proconfig=ARRAY['search_path=pg_catalog, control_plane']
 AND has_function_privilege('control_plane_runtime',p.oid,'EXECUTE')
 AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
 WHERE a.grantee NOT IN ('control_plane_owner'::regrole,'control_plane_runtime'::regrole)
 OR a.grantee='control_plane_runtime'::regrole AND (a.is_grantable OR a.privilege_type<>'EXECUTE'))""" +
                      (" AND p.proargnames=ARRAY[" + ",".join("'" + n + "'" for n in names) + "]" if names else "") + ")")
    marker = " 'profile','" + old_profile + "',"
    assert source.count(marker) == 1, "GitHub proof predecessor identity changed"
    profile = "GITHUB_AUTONOMOUS_PDLC_" + ("BACKUP_" if backup else "") + ("PREDECESSOR_V1" if precondition else "V1")
    fields = " 'profile','" + profile + "',\n"
    if precondition:
        idle_source = (DIRECTORY / "workspace-restore-migration-precondition.sql").read_text()
        assert hashlib.sha256(idle_source.encode()).hexdigest() == "cfbc7b55f69f8dfa6b756701cc9e32d7584b82a47ee15f21527be2e6007712c2", "GitHub idle predecessor changed"
        idle = idle_source.split("    'oldClaimSessions',", 1)[1].split("\n\n))::text", 1)[0]
        fields += " 'database',current_database(),\n 'githubHistoricalAbsent',to_regprocedure('control_plane.integration_github_legacy_capabilities(text,text,text,text)') IS NULL AND EXISTS(SELECT 1 FROM pg_proc p WHERE p.oid='control_plane.integration_grant_admission(uuid,uuid,uuid,text,text,text,text,text,text,text[])'::regprocedure AND md5(p.prosrc)='" + hashlib.md5(admission[0].encode()).hexdigest() + "' AND pg_get_userbyid(p.proowner)='control_plane_owner' AND NOT p.prosecdef),\n 'oldClaimSessions'," + idle + ","
    else:
        fields += " 'githubHistoricalCatalog'," + " AND ".join(checks) + ","
    source = source.replace(marker, fields)
    return "-- Generated: generate-github-autonomous-proof.py; полный028 и exact GitHub admission.\n" + source


def main() -> None:
    """Записывает воспроизводимый SQL либо проверяет уже опубликованные bytes."""
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    for backup in (False, True):
        for precondition in (False, True):
            path = DIRECTORY / ("github-autonomous-" + ("backup-" if backup else "") + "migration-" + ("precondition" if precondition else "postcondition") + ".sql")
            content = generate(backup, precondition)
            if args.check:
                assert path.read_text() == content, "GitHub admission proof differs from generated source"
            else:
                path.write_text(content)
    restore_directory = MIGRATIONS.parent / "workspace_backup_sql"
    restore = (restore_directory / "restore_20261028000100.sql").read_text()
    assert hashlib.sha256(restore.encode()).hexdigest() == "eb561159ce99a349c28975a7af0acb8596131586c54ea0444c23b7518fc6ccb3", "GitHub backup restore predecessor changed"
    assert restore.count("20261028000100") == 1, "GitHub backup restore version shape changed"
    content = restore.replace("20261028000100", "20261029000100", 1).replace("generate-workspace-backup-compatibility.py", "generate-github-autonomous-proof.py", 1)
    path = restore_directory / "restore_20261029000100.sql"
    if args.check:
        assert path.read_text() == content, "GitHub backup restore differs from generated source"
    else:
        path.write_text(content)


if __name__ == "__main__":
    main()
