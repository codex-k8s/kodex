#!/usr/bin/env python3
"""Выводит exact036 из frozen035, не меняя исторические schema proofs."""
import argparse
import hashlib
import json
from pathlib import Path

PINS = {
    'tools/release/jira-create-fields-migration-postcondition.sql': '9661d9258d62c79bf49a63ba82169b65e2bbd5a63b6433f663e00528b9f83ab8',
    'tools/release/jira-create-fields-backup-migration-postcondition.sql': '7c5a85f9cb3529cdfe85befe34875ba5bea061165f20fef602ee7ef86d450eed',
    'tools/release/jira-create-fields-migration-precondition.sql': '45bf3cc84f0133d4b2edb4845cd981356d3f7706e20c5a2d1b88bd81f32c1b04',
    'tools/release/jira-create-fields-backup-migration-precondition.sql': '72ff71b0ee88d7caa28f96226a030136bc61fcc6643f4d89ed2e15a6548b827b',
    'services/internal/control-plane/cmd/cli/workspace_backup_sql/restore_20261107000100.sql': '1ab26aaf815a2fcd3014379b76c796fe05dff3d47a7abedd60b9f0184c0325e1',
}
PREFIX = 'tools/release/authority-proof-counter-'
MIGRATION = 'services/internal/control-plane/cmd/cli/migrations/20261108000100_authority_proof_counter.sql'


def canonical(value):
    """Закрепляет authored metadata детерминированно; live catalog не является входом."""
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=False)


def counter_guard(root, mode):
    """Строит закрытые normal/backup/restore ACL guards из одного authored шаблона."""
    assert mode in ('normal', 'backup', 'restore')
    migration = (root / MIGRATION).read_text()
    body = migration.split('AS $function$', 1)[1].split('$function$;', 1)[0]
    constraints = {}
    for suffix, kind, noinherit, definition in (
        ('pkey', 'p', True, 'PRIMARY KEY (singleton)'),
        ('singleton_not_null', 'n', False, 'NOT NULL singleton'),
        ('revision_not_null', 'n', False, 'NOT NULL revision'),
        ('singleton_check', 'c', False, 'CHECK (singleton)'),
        ('revision_check', 'c', False, "CHECK (((revision >= 0) AND (revision <= '9007199254740991'::bigint)))"),
    ):
        constraints['authority_proof_revision_counter_' + suffix] = [kind, True, True, False, False, True, 0, noinherit, definition]
    acl = [['control_plane_owner', 'control_plane_owner', privilege, False]
           for privilege in ('DELETE', 'INSERT', 'MAINTAIN', 'REFERENCES', 'SELECT', 'TRIGGER', 'TRUNCATE', 'UPDATE')]
    acl += [['control_plane_runtime', 'control_plane_owner', privilege, False] for privilege in ('SELECT', 'UPDATE')]
    if mode == 'backup':
        acl += [['kodex_backup_reader', 'control_plane_owner', 'SELECT', False]]
    raw = (root / (PREFIX + 'catalog.sql')).read_text()
    optional_guard = 'true'
    if mode == 'restore':
        optional_guard = """NOT EXISTS(SELECT 1 FROM pg_class r CROSS JOIN LATERAL aclexplode(r.relacl) a
   WHERE r.oid=v_relation AND pg_get_userbyid(a.grantee)='kodex_backup_reader'
     AND (a.privilege_type<>'SELECT' OR a.is_grantable OR a.grantor<>'control_plane_owner'::regrole))"""
    for token, value in {
        '@CONSTRAINTS@': canonical(constraints), '@ACL@': canonical(acl),
        '@ACL_FILTER@': "AND pg_get_userbyid(a.grantee)<>'kodex_backup_reader'" if mode == 'restore' else '',
        '@OPTIONAL_BACKUP_GUARD@': optional_guard,
        '@FUNCTION_MD5@': hashlib.md5(body.encode()).hexdigest(),
    }.items():
        assert token in raw
        raw = raw.replace(token, value)
    assert '@' not in raw
    return raw


def outputs():
    """Добавляет только036 outputs, полный frozen035 catalog и отдельный counter guard."""
    root = Path(__file__).resolve().parents[2]
    for name, digest in PINS.items():
        assert hashlib.sha256((root / name).read_bytes()).hexdigest() == digest, 'frozen035 changed'
    result, digests = {}, {}
    end = ')::text FROM predecessor_proof CROSS JOIN evidence_proof;'
    # Все idle counters сохраняются из035, но фактический predecessor теперь035, не034.
    old_pre = (root / 'tools/release/jira-create-fields-migration-precondition.sql').read_text()
    idle = old_pre.split(" || jsonb_build_object(\n'oldClaimSessions',", 1)[1].split(end, 1)[0]
    idle = " || jsonb_build_object(\n'oldClaimSessions'," + idle
    assert 'activePersonalCredentialTests' in idle and 'activeRuntimeRuns' in idle
    for backup in (False, True):
        profile = 'BACKUP_' if backup else ''
        prefix = 'backup-' if backup else ''
        raw = (root / ('tools/release/jira-create-fields-' + prefix + 'migration-postcondition.sql')).read_text()
        assert raw.count(end) == 1
        old_tag = "'JIRA_CREATE_FIELDS_" + profile + "V1'"
        assert raw.count(old_tag) == 1
        pre = raw.replace(old_tag, "'AUTHORITY_PROOF_COUNTER_" + profile + "PREDECESSOR_V1'")
        absent = " || jsonb_build_object('authorityProofCounterAbsent',to_regclass('control_plane.authority_proof_revision_counter') IS NULL AND to_regprocedure('control_plane.reject_legacy_authority_proof_counter()') IS NULL AND NOT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='control_plane.installation'::regclass AND tgname='reject_legacy_authority_proof_counter'))"
        pre = pre.replace(end, idle + absent + end)
        result[PREFIX + prefix + 'migration-precondition.sql'] = '-- Generated: generate-authority-proof-counter-proof.py; exact035 idle predecessor.\n' + pre
        guard = counter_guard(root, 'backup' if backup else 'normal')
        digest = hashlib.sha256(guard.encode()).hexdigest()
        digests['backup' if backup else 'normal'] = digest
        raw = raw.replace('SET LOCAL search_path=pg_catalog;', 'SET LOCAL search_path=pg_catalog;\n' + guard, 1)
        raw = raw.replace(old_tag, "'AUTHORITY_PROOF_COUNTER_" + profile + "V1'")
        raw = raw.replace(end, " || jsonb_build_object('authorityProofCounterCatalog',true,'authorityProofCounterCatalogDigest','" + digest + "')" + end)
        result[PREFIX + prefix + 'migration-postcondition.sql'] = '-- Generated: generate-authority-proof-counter-proof.py; exact036 full predecessor and counter.\n' + raw
    restore_path = 'services/internal/control-plane/cmd/cli/workspace_backup_sql/'
    raw = (root / (restore_path + 'restore_20261107000100.sql')).read_text()
    assert raw.count('20261107000100::bigint') == 1
    raw = raw.replace('20261107000100::bigint', '20261108000100::bigint')
    # pg_get_* представления должны совпадать с READ ONLY proof, независимо от DSN search_path.
    raw = 'SET LOCAL search_path=pg_catalog;\n' + raw
    raw += counter_guard(root, 'restore')
    raw += 'GRANT SELECT ON control_plane.authority_proof_revision_counter TO kodex_backup_reader;\n'
    result[restore_path + 'restore_20261108000100.sql'] = '-- Generated: generate-authority-proof-counter-proof.py; exact036 SELECT-only restore.\n' + raw
    digests['migrationSHA256'] = hashlib.sha256((root / MIGRATION).read_bytes()).hexdigest()
    result[PREFIX + 'catalog.gen.mjs'] = '// Generated: generate-authority-proof-counter-proof.py; exact036.\nexport const authorityProofCounterCatalog = Object.freeze(' + canonical(digests) + ');\n'
    return result


def main():
    """--check подтверждает generated bytes без записи; обычный запуск пишет лишь036 outputs."""
    parser = argparse.ArgumentParser()
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[2]
    for name, raw in outputs().items():
        path = root / name
        if args.check:
            assert path.read_text() == raw, 'generated036 output differs: ' + name
        else:
            path.write_text(raw)


if __name__ == '__main__':
    main()
