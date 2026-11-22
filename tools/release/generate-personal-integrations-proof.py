#!/usr/bin/env python3
"""Генерирует проверки переходов 046–049 из квалифицированных снимков PostgreSQL."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import stat

ROOT = Path(__file__).resolve().parents[2]
RELEASE = ROOT / "tools/release"
CLI = ROOT / "services/internal/control-plane/cmd/cli"
MANIFEST = RELEASE / "personal-integrations-schema-manifest.json"
INITIALIZER = ROOT / "deploy/k8s/base/platform-state/postgresql/reconcile-runtime-credentials.sh"
MODES = ("normal", "backup", "table-only-backup")
VERSIONS = (20261114140000, 20261114150000, 20261114160000, 20261114170000)
VERSION_NUMBER = "180003"
CAPTURE_SCHEMA = "kodex.integration-cohort-capture.v1"
CAPTURE_PURPOSE = "SCHEMA_CAPTURE_ONLY"
MAX_CAPTURE_FILE = 16 << 20
TABLE_ONLY_INITIALIZER = (
    "SET ROLE control_plane_owner; GRANT SELECT ON ALL TABLES IN SCHEMA control_plane,public TO kodex_backup_reader; "
    "ALTER DEFAULT PRIVILEGES FOR ROLE control_plane_owner IN SCHEMA control_plane GRANT SELECT ON TABLES TO kodex_backup_reader; RESET ROLE"
)
SHA = re.compile(r"[a-f0-9]{64}")
MD5 = re.compile(r"[a-f0-9]{32}")
CATALOG_HASHES = (
    "catalogDigest", "tablesDigest", "functionsDigest", "triggersDigest",
    "authorityDigest", "purgeGraphDigest",
)
CATALOG_COUNTS = ("tableCount", "functionCount", "triggerCount", "purgeNodeCount", "purgeEdgeCount")
CATALOG_FINGERPRINTS = ("purgeNodeFingerprint", "purgeEdgeFingerprint")
CATALOG_KEYS = {"tableDefaultACL", *CATALOG_HASHES, *CATALOG_COUNTS, *CATALOG_FINGERPRINTS}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def unique(fields):
    result = {}
    for key, value in fields:
        require(key not in result, "duplicate proof field")
        result[key] = value
    return result


def decode(raw):
    def invalid_constant(_value):
        raise ValueError("non-JSON number in schema proof")
    return json.loads(raw, object_pairs_hook=unique, parse_constant=invalid_constant)


def read_json(path):
    with path.open("rb") as stream:
        raw = stream.read(MAX_CAPTURE_FILE + 1)
    require(0 < len(raw) <= MAX_CAPTURE_FILE, "schema proof size is invalid")
    return decode(raw)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True)


def marked(text, start, end):
    require(text.count(start) == 1 and text.count(end) == 1, "canonical SQL markers changed")
    return text.split(start, 1)[1].split(end, 1)[0]


def query(name):
    return (RELEASE / name).read_text().strip().removesuffix(";")


def initializer_sql():
    return marked(INITIALIZER.read_text(), "-- BEGIN CANONICAL CONTROL PLANE BACKUP ACL\n",
                  "-- END CANONICAL CONTROL PLANE BACKUP ACL")


def capture_file(directory, name):
    require(Path(name).name == name and name not in ("", ".", ".."), "capture filename is invalid")
    with os.fdopen(os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=directory), "rb") as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o600 and
                info.st_uid == os.geteuid() and info.st_nlink == 1 and
                (0 < info.st_size <= MAX_CAPTURE_FILE or name == "prewarm.log" and info.st_size == 0),
                "capture file custody or size differs")
        raw = stream.read(info.st_size + 1)
        after = os.fstat(stream.fileno())
        require(len(raw) == info.st_size == after.st_size and info.st_mtime_ns == after.st_mtime_ns,
                "capture file changed while reading")
        return raw


def stage_files(mode):
    return [f"{mode}-{stage:03d}.{suffix}.json" for stage in range(46, 50)
            for suffix in ("summary", "catalog", "seal")]


def index_entries(entries, names):
    require(type(entries) is list and [entry["file"] for entry in entries] == names, "capture artifact set differs")
    for entry in entries:
        require(set(entry) == {"file", "sha256", "bytes"} and
                type(entry["bytes"]) is int and
                (0 < entry["bytes"] <= MAX_CAPTURE_FILE or entry["file"] == "prewarm.log" and entry["bytes"] == 0) and
                type(entry["sha256"]) is str and SHA.fullmatch(entry["sha256"]),
                "capture artifact metadata is invalid")
    return {entry["file"]: (entry["bytes"], entry["sha256"]) for entry in entries}


def migration_pins(version):
    migrations = []
    for file in sorted((CLI / "migrations").glob("*.sql")):
        match = re.fullmatch(r"([0-9]{14})_[a-z0-9_]+\.sql", file.name)
        require(match is not None, "unexpected migration filename")
        number = int(match.group(1))
        if number <= version:
            migrations.append({"version": number, "file": file.name, "sha256": digest(file.read_bytes())})
    require([entry["version"] for entry in migrations] == [row[0] for row in expected_history(version)[1:]],
            "duplicate or missing migration source")
    return migrations


def capture_sources():
    catalog = (RELEASE / "managed-kubernetes-catalog.sql").read_bytes()
    head = catalog.rfind(b"SELECT jsonb_build_object(\n")
    require(head >= 0 and catalog.rstrip().endswith(b"FROM complete_catalog;"), "canonical catalog selector changed")
    return {
        "catalogReaderSHA256": digest(catalog),
        "legacyRestoreCatalogReaderSHA256": digest((RELEASE / "telegram-notifications-restore-catalog.sql").read_bytes()),
        "fullCatalogProjectionSHA256": digest(catalog[:head] + b"SELECT content::text FROM complete_catalog;"),
        "canonicalBackupInitializerSHA256": digest(initializer_sql().encode()),
    }


def read_capture(directory_path, source, index_sha):
    require(re.fullmatch(r"[a-f0-9]{40}", source) and set(source) != {"0"} and SHA.fullmatch(index_sha),
            "exact capture source and completion SHA are required")
    require(directory_path.is_absolute() and directory_path.resolve() == directory_path, "private capture path is invalid")
    directory = os.open(directory_path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        info = os.fstat(directory)
        require(stat.S_IMODE(info.st_mode) == 0o700 and info.st_uid == os.geteuid(), "capture directory custody differs")
        raw_index = capture_file(directory, "capture.complete.json")
        index = decode(raw_index)
        common = {"schema": CAPTURE_SCHEMA, "purpose": CAPTURE_PURPOSE, "sourceSHA": source}
        require(digest(raw_index) == index_sha and set(index) == {*common, "modes", "files"} and
                all(index[key] == value for key, value in common.items()) and index["modes"] == list(MODES),
                "complete three-mode capture proof differs")
        names = ["prewarm.preflight.json", "prewarm.log"]
        for mode in MODES:
            names.extend([f"{mode}.preflight.json", f"{mode}.run.log", f"{mode}.complete.json", *stage_files(mode)])
        entries = index_entries(index["files"], names)
        for name in names:
            raw = capture_file(directory, name)
            require((len(raw), digest(raw)) == entries[name], "capture artifact changed: " + name)
        snapshots = {}
        sources = capture_sources()
        for mode in MODES:
            marker_name = f"{mode}.complete.json"
            raw = capture_file(directory, marker_name)
            require((len(raw), digest(raw)) == entries[marker_name], "mode marker changed")
            marker = decode(raw)
            binding = {**common, "mode": mode, "sealedGooseHead": VERSIONS[-1]}
            require(set(marker) == {*binding, "files"} and all(marker[key] == value for key, value in binding.items()),
                    "mode marker differs")
            require(index_entries(marker["files"], stage_files(mode)) ==
                    {name: entries[name] for name in stage_files(mode)}, "mode artifact fingerprints differ")
            snapshots[mode] = {}
            for stage, version in enumerate(VERSIONS, 46):
                prefix = f"{mode}-{stage:03d}"
                stage_artifacts = {}
                for suffix in ("summary", "catalog", "seal"):
                    name = f"{prefix}.{suffix}.json"
                    raw = capture_file(directory, name)
                    require((len(raw), digest(raw)) == entries[name], "stage artifact changed: " + name)
                    stage_artifacts[suffix] = raw
                snapshots[mode][str(version)] = validate_capture_stage(stage_artifacts, common, sources, mode, stage, version)
        return {
            "version": 1, "serverVersionNum": int(VERSION_NUMBER),
            "sourceSHA": source, "captureIndexSHA256": index_sha,
            "predecessorManifestSHA256": digest((RELEASE / "telegram-notifications-schema-manifest.json").read_bytes()),
            "backupInitializerSHA256": sources["canonicalBackupInitializerSHA256"],
            "catalogReaderSHA256": sources["catalogReaderSHA256"],
            "migrationPins": migration_pins(VERSIONS[-1]), "snapshots": snapshots,
        }
    finally:
        os.close(directory)


def validate_capture_stage(artifacts, common, sources, mode, stage, version):
    summary = decode(artifacts["summary"])
    catalog = decode(artifacts["catalog"])
    seal = decode(artifacts["seal"])
    require(type(catalog) is dict and set(catalog) == {
        "roles", "memberships", "schema", "defaultACL", "otherRelations", "types",
        "tables", "functions", "triggers", "purgeGraph", "sequences",
    }, "captured full schema catalog shape differs")
    require(set(summary) == {*CATALOG_KEYS, "restoreCatalogDigest", "backupInitializerSHA256"}, "summary fields differ")
    restore = summary.pop("restoreCatalogDigest")
    initializer_sha = summary.pop("backupInitializerSHA256")
    validate_catalog(summary)
    require(initializer_sha == sources["canonicalBackupInitializerSHA256"] and type(restore) is str and SHA.fullmatch(restore) and
            summary["catalogDigest"] == digest(artifacts["catalog"]), "capture summary provenance differs")
    for name in ("tables", "functions", "triggers"):
        require(type(catalog[name]) is dict and summary[name[:-1] + "Count"] == len(catalog[name]),
                "capture schema object count differs")
    acl = {"normal": "", "backup": initializer_sql(), "table-only-backup": TABLE_ONLY_INITIALIZER}
    identity = {
        **common, **sources, "mode": mode, "stage": f"{stage:03d}",
        "gooseSerialization": "COMPACT_SORTED_KEYS_JSON_V1",
        "migrations": migration_pins(version), "legacyRestoreCatalogDigest": restore,
        "modeInitializerSHA256": digest(acl[mode].encode()),
        "canonicalBackupInitializerApplied": mode == "backup",
        "summarySHA256": digest(artifacts["summary"]), "catalogSHA256": summary["catalogDigest"],
    }
    require(set(seal) == {*identity, "gooseHead", "gooseHistorySHA256", "gooseSealSHA256"} and
            all(seal[key] == value for key, value in identity.items()), "stage seal source or context differs")
    head = seal["gooseHead"]
    require(set(head) == {"head", "history", "serverVersionNum"} and
            head["head"] == version and head["serverVersionNum"] == VERSION_NUMBER,
            "Goose head does not match the stage")
    history = head["history"]
    require(type(history) is list and all(type(row) is list and len(row) == 3 and
            type(row[0]) is int and row[0] > 0 and type(row[1]) is int and row[2] is True for row in history),
            "captured Goose ledger row is invalid")
    ids = [row[0] for row in history]
    require(ids == sorted(set(ids)) and [row[1:] for row in history] == expected_history(version),
            "captured Goose ledger is not exact")
    require(seal["gooseHistorySHA256"] == digest(canonical(history).encode()) and
            seal["gooseSealSHA256"] == digest(canonical(head).encode()), "Goose ledger provenance differs")
    return {"catalog": summary, "restoreCatalogDigest": restore}


def validate_catalog(value):
    require(type(value) is dict and set(value) == CATALOG_KEYS, "catalog keys differ")
    require(all(type(value[key]) is str and SHA.fullmatch(value[key]) for key in CATALOG_HASHES),
            "catalog digest is invalid")
    require(all(type(value[key]) is str and MD5.fullmatch(value[key]) for key in CATALOG_FINGERPRINTS),
            "purge fingerprint is invalid")
    require(all(type(value[key]) is int and 0 < value[key] <= 100000 for key in CATALOG_COUNTS),
            "catalog count is invalid")
    acl = value["tableDefaultACL"]
    require(acl is None or (type(acl) is list and 0 < len(acl) <= 16 and
                          all(type(item) is str and 0 < len(item) <= 1024 for item in acl)),
            "table default ACL is invalid")


def qualified_predecessor_catalogs():
    path = CLI / "migrations/20261114150000_managed_kubernetes.sql"
    guard = marked(path.read_text(), "-- BEGIN GENERATED MANAGED KUBERNETES PREDECESSOR\n",
                   "-- END GENERATED MANAGED KUBERNETES PREDECESSOR")
    matches = re.findall(r"\$predecessor(\d+)\$(\{[^\n]+?\})\$predecessor\1\$", guard)
    require([index for index, _ in matches] == ["0", "1", "2"], "generated 047 predecessor guards differ")
    return {mode: decode(value) for mode, (_, value) in zip(MODES, matches, strict=True)}


def validate_manifest(manifest):
    predecessor_raw = (RELEASE / "telegram-notifications-schema-manifest.json").read_bytes()
    predecessor = decode(predecessor_raw)
    initializer_sha = digest(initializer_sql().encode())
    require(set(manifest) == {"version", "serverVersionNum", "sourceSHA", "captureIndexSHA256",
                              "backupInitializerSHA256", "catalogReaderSHA256", "predecessorManifestSHA256",
                              "migrationPins", "snapshots"} and
            re.fullmatch(r"[a-f0-9]{40}", manifest["sourceSHA"]) and SHA.fullmatch(manifest["captureIndexSHA256"]) and
            manifest["migrationPins"] == migration_pins(VERSIONS[-1]), "manifest source or migration pins changed")
    require(manifest["version"] == 1 and manifest["serverVersionNum"] == int(VERSION_NUMBER) and
            manifest["predecessorManifestSHA256"] == digest(predecessor_raw) and
            manifest["backupInitializerSHA256"] == initializer_sha and
            predecessor["migrationVersion"] == VERSIONS[0] and
            predecessor["backupInitializer"]["sqlSHA256"] == initializer_sha,
            "qualified 046 predecessor or canonical initializer changed")
    require(manifest["catalogReaderSHA256"] == digest((RELEASE / "managed-kubernetes-catalog.sql").read_bytes()),
            "catalog SQL source changed")
    require(set(manifest["snapshots"]) == set(MODES), "snapshot modes differ")
    predecessor_catalogs = qualified_predecessor_catalogs()
    for mode in MODES:
        snapshots = manifest["snapshots"][mode]
        require(set(snapshots) == set(map(str, VERSIONS)), "snapshot versions differ")
        for version, entry in snapshots.items():
            require(set(entry) == {"catalog", "restoreCatalogDigest"}, "snapshot entry differs")
            validate_catalog(entry["catalog"])
            require(type(entry["restoreCatalogDigest"]) is str and SHA.fullmatch(entry["restoreCatalogDigest"]),
                    "restore catalog digest is invalid")
            if version == str(VERSIONS[0]):
                require(entry["catalog"] == predecessor_catalogs[mode] and
                        entry["restoreCatalogDigest"] == predecessor["snapshots"][mode + "-after"]["restoreCatalogDigest"],
                        "captured 046 does not match the qualified predecessor")
    for version in VERSIONS:
        hashes = {manifest["snapshots"][mode][str(version)]["catalog"]["catalogDigest"] for mode in MODES}
        require(len(hashes) == len(MODES), "snapshot modes are indistinguishable")


def expected_history(version):
    versions = [
        int(path.name.split("_", 1)[0])
        for path in (CLI / "migrations").glob("*.sql")
        if re.fullmatch(r"[0-9]{14}_[a-z0-9_]+\.sql", path.name)
    ]
    require(len(versions) == len(set(versions)) and version in versions, "migration versions are invalid")
    return [[number, True] for number in [0] + sorted(number for number in versions if number <= version)]


def history_matches(version):
    history = canonical(expected_history(version))
    return f"""(SELECT jsonb_agg(jsonb_build_array(version_id,is_applied) ORDER BY id)
 FROM public.goose_db_version) IS NOT DISTINCT FROM $history${history}$history$::jsonb"""


def ddl_boundary(expected_version):
    return f"""IF session_user<>'control_plane_migrator' OR current_database()<>'control_plane'
 OR current_setting('server_version_num')::int<>{VERSION_NUMBER} OR NOT ({history_matches(expected_version)})
 THEN RAISE EXCEPTION 'Personal integration migration database or schema changed';END IF;
"""


def activity_fields():
    telegram = (RELEASE / "telegram-notifications-migration-precondition.sql").read_text()
    start = " 'activeRuntimeLeases'"
    end = ")::text;\nROLLBACK;"
    return start + marked(telegram, start, end)


def idle_checks():
    workspace = query("github-repository-scope-workspace-idle.sql")
    return f"""IF ({workspace}) IS DISTINCT FROM true THEN RAISE EXCEPTION 'Personal integration workspace owner is not idle';END IF;
 SELECT jsonb_build_object({activity_fields()}) INTO activity;
 IF EXISTS(SELECT 1 FROM jsonb_each_text(activity) WHERE value::bigint<>0)
 THEN RAISE EXCEPTION 'Personal integration migration owner is not idle';END IF;
"""


def match_catalog(query_sql, values, error):
    serialized = [f"$expected{index}${canonical(value)}$expected{index}$::jsonb" for index, value in enumerate(values)]
    mismatch = " AND ".join("actual IS DISTINCT FROM " + value for value in serialized)
    return f"SELECT ({query_sql}) INTO actual;\nIF {mismatch} THEN RAISE EXCEPTION '{error}';END IF;\n"


def render_checks(manifest):
    outputs = {}
    snapshots = manifest["snapshots"]
    catalog = query("managed-kubernetes-catalog.sql")
    public_sequence = query("github-repository-scope-public-sequence.sql")
    sequence_guard = f"IF ({public_sequence}) IS DISTINCT FROM true THEN RAISE EXCEPTION 'Personal integration public sequence boundary changed';END IF;\n"
    idle = idle_checks()
    sql_owner = """SET LOCAL search_path=pg_catalog;SET LOCAL ROLE control_plane_owner;
SET LOCAL lock_timeout='8s';SET LOCAL statement_timeout='30s';
SELECT pg_advisory_xact_lock(hashtextextended('kodex.workspace.backup-acl-maintenance.v1',0));
"""
    for version in VERSIONS:
        values = {mode: snapshots[mode][str(version)]["catalog"] for mode in MODES}
        for mode in ("normal", "backup"):
            exact = match_catalog(catalog, [values[mode]], "Personal integration schema catalog changed")
            outputs[CLI / f"workspace_backup_sql/check_integrations_{version}_{mode}.sql"] = (
                "-- Generated: generate-personal-integrations-proof.py.\n" + sql_owner
                + "DO $personal_integration_schema$ DECLARE actual jsonb; activity jsonb; BEGIN\n"
                + ddl_boundary(version) + sequence_guard + exact + idle
                + "END $personal_integration_schema$;\nRESET ROLE;\n"
            )
        if version == VERSIONS[0]:
            continue
        known = match_catalog(catalog, list(values.values()), "Personal integration backup restore catalog changed")
        final = match_catalog(catalog, [values["backup"]], "Personal integration backup restore final catalog changed")
        outputs[CLI / f"workspace_backup_sql/restore_{version}.sql"] = (
            "-- Generated: generate-personal-integrations-proof.py. Только известные ACL на точной схеме.\n"
            + sql_owner + "DO $personal_integration_restore$ DECLARE actual jsonb; activity jsonb; BEGIN\n"
            + ddl_boundary(version) + sequence_guard + idle
            + """IF EXISTS(SELECT 1 FROM pg_class WHERE relnamespace='public'::regnamespace AND relkind IN('r','p','v','m','f')
 AND oid<>'public.goose_db_version'::regclass)
 THEN RAISE EXCEPTION 'Personal integration backup restore public relation boundary changed';END IF;
"""
            + known + initializer_sql() + final + "END $personal_integration_restore$;\nRESET ROLE;\n"
        )
    return outputs


def render_release_proofs(manifest):
    outputs = {}
    snapshots = manifest["snapshots"]
    catalog = query("managed-kubernetes-catalog.sql")
    workspace = query("github-repository-scope-workspace-idle.sql")
    sequence = query("github-repository-scope-public-sequence.sql")
    for mode in ("normal", "backup"):
        profile = "personal-integrations" + ("-backup" if mode == "backup" else "")
        qualification = "CANONICAL_BACKUP" if mode == "backup" else "NONE"
        for phase, version in (("precondition", VERSIONS[0]), ("postcondition", VERSIONS[-1])):
            expected = snapshots[mode][str(version)]["catalog"]
            proof = profile.upper().replace("-", "_")
            if phase == "precondition":
                proof += "_PREDECESSOR"
            fields = f"""'profile','{proof}_V1',
 'database',current_database(),'readOnly',current_setting('transaction_read_only'),
 'gooseVersion',(SELECT max(version_id) FROM public.goose_db_version WHERE is_applied),
 'serverVersionNum',current_setting('server_version_num')::int,
 'historyMatches',({history_matches(version)}),
 'backupQualification','{qualification}','publicSequenceSafe',({sequence}),
 'schemaCatalog',value,
 'schemaMatches',value=$expected${canonical(expected)}$expected$::jsonb"""
            if phase == "precondition":
                fields += f",'workspaceOwnerIdle',({workspace})," + activity_fields()
            outputs[RELEASE / f"{profile}-migration-{phase}.sql"] = (
                "-- Generated: generate-personal-integrations-proof.py.\n"
                "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;\n"
                "SET LOCAL statement_timeout='15s';SET LOCAL lock_timeout='500ms';SET LOCAL search_path=pg_catalog;\n"
                f"WITH qualified_catalog(value) AS MATERIALIZED ({catalog})\n"
                f"SELECT jsonb_build_object({fields})::text FROM qualified_catalog;\nROLLBACK;\n"
            )
    # Повтор не маскирует промежуточную версию под046 либо049: все четыре
    # допустимые версии сверяются со своими captured catalog/history того же mode.
    for mode in ("normal", "backup"):
        profile = "personal-integrations" + ("-backup" if mode == "backup" else "")
        qualification = "CANONICAL_BACKUP" if mode == "backup" else "NONE"
        head = "(SELECT max(version_id) FROM public.goose_db_version WHERE is_applied)"
        histories = "CASE " + head + " " + " ".join(
            f"WHEN {version} THEN ({history_matches(version)})" for version in VERSIONS) + " ELSE false END"
        catalogs = "CASE " + head + " " + " ".join(
            f"WHEN {version} THEN value=$expected${canonical(snapshots[mode][str(version)]['catalog'])}$expected$::jsonb"
            for version in VERSIONS) + " ELSE false END"
        fields = f"""'profile','{profile.upper().replace('-', '_')}_RECOVERY_V1',
 'database',current_database(),'readOnly',current_setting('transaction_read_only'),
 'gooseVersion',{head},'serverVersionNum',current_setting('server_version_num')::int,
 'historyMatches',({histories}),'schemaMatches',({catalogs}),
 'backupQualification','{qualification}','publicSequenceSafe',({sequence}),
 'schemaCatalog',value,'workspaceOwnerIdle',({workspace}),""" + activity_fields()
        outputs[RELEASE / f"{profile}-migration-recovery.sql"] = (
            "-- Generated: generate-personal-integrations-proof.py.\n"
            "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;\n"
            "SET LOCAL statement_timeout='15s';SET LOCAL lock_timeout='500ms';SET LOCAL search_path=pg_catalog;\n"
            f"WITH qualified_catalog(value) AS MATERIALIZED ({catalog})\n"
            f"SELECT jsonb_build_object({fields})::text FROM qualified_catalog;\nROLLBACK;\n"
        )
    return outputs


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    parser.add_argument("--capture-directory", type=Path)
    parser.add_argument("--capture-source-sha")
    parser.add_argument("--capture-index-sha256")
    args = parser.parse_args()
    capture = (args.capture_directory, args.capture_source_sha, args.capture_index_sha256)
    require(not any(capture) or (all(capture) and not args.check), "capture requires a directory and all immutable pins")
    manifest = read_capture(*capture) if all(capture) else read_json(MANIFEST)
    validate_manifest(manifest)
    if all(capture):
        MANIFEST.write_text(json.dumps(manifest, sort_keys=True, indent=2) + "\n")
    for path, expected in (render_checks(manifest) | render_release_proofs(manifest)).items():
        if args.check:
            require(path.is_file() and path.read_text() == expected,
                    "generated proof differs: " + str(path.relative_to(ROOT)))
        else:
            path.write_text(expected)


if __name__ == "__main__":
    main()
