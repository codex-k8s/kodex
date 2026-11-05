#!/usr/bin/env python3
"""Генерирует exact proof033 поверх неизменного read-only predecessor032."""
import argparse
import hashlib
import json
from pathlib import Path


def outputs():
    """Проверяет происхождение synthetic schema snapshot и строит закрытый SQL/JS контракт."""
    directory = Path(__file__).resolve().parent
    root = directory.parent.parent
    manifest = json.loads((directory / "platform-oidc-identity-schema-manifest.json").read_text())
    migration = root / "services/internal/control-plane/cmd/cli/migrations/20261105000100_platform_oidc_identity.sql"
    predecessor = directory / "workflow-content-page-migration-postcondition.sql"
    reader = directory / "platform-oidc-identity-catalog.sql"
    assert manifest["kind"] == "EXPECTED_SCHEMA_CONTRACT" and manifest["version"] == "20261105000100"
    for path, key in [(migration, "migrationSHA256"), (predecessor, "predecessorPostconditionSHA256"), (reader, "catalogReaderSHA256")]:
        assert hashlib.sha256(path.read_bytes()).hexdigest() == manifest[key], "frozen source changed"
    catalog = manifest["catalog"]
    assert len(catalog["tables"]) == 4 and len(catalog["functions"]) == 3 and len(catalog["triggers"]) == 4
    canonical = json.dumps(catalog, sort_keys=True, separators=(",", ":"), ensure_ascii=False)
    digest = hashlib.sha256(canonical.encode()).hexdigest()
    query = reader.read_text().rstrip()
    assert query.endswith(");")
    query = query[:-2] + ") INTO v_actual;"
    guard = """
SET LOCAL search_path=pg_catalog;
DO $oidc_identity_catalog$
DECLARE v_actual jsonb;
BEGIN
""" + query + "\nIF v_actual IS DISTINCT FROM $expected_oidc$" + canonical + "$expected_oidc$::jsonb THEN\n" + "    RAISE EXCEPTION 'platform OIDC identity schema catalog changed';\nEND IF;\nEND $oidc_identity_catalog$;\n"
    source = predecessor.read_text()
    marker = "SET LOCAL lock_timeout = '500ms';"
    assert source.count(marker) == 1
    source = source.replace(marker, marker + guard, 1)
    head = "SELECT jsonb_build_object(\n 'profile','WORKFLOW_CONTENT_PAGE_V1',"
    tail = ")::text FROM predecessor_proof CROSS JOIN evidence_proof;"
    assert source.count(head) == 1 and source.count(tail) == 1
    source = source.replace(head, "SELECT (jsonb_build_object(\n 'profile','PLATFORM_OIDC_IDENTITY_V1',", 1)
    source = source.replace(tail, ") || jsonb_build_object('platformOIDCCatalog',true,'platformOIDCCatalogDigest','" + digest + "'))::text FROM predecessor_proof CROSS JOIN evidence_proof;", 1)
    return {
        directory / "platform-oidc-identity-migration-postcondition.sql": "-- Generated: generate-platform-oidc-identity-proof.py; frozen032 не изменён.\n" + source,
        directory / "platform-oidc-identity-catalog.gen.mjs": "// Generated: generate-platform-oidc-identity-proof.py; exact033 catalog.\nexport const platformOIDCIdentityCatalogDigest = '" + digest + "';\n",
    }


def main():
    """Записывает proof либо проверяет его воспроизводимость без изменения файлов."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    for path, content in outputs().items():
        if args.check:
            assert path.read_text() == content, "platform OIDC proof generated file differs"
        else:
            path.write_text(content)


if __name__ == "__main__":
    main()
