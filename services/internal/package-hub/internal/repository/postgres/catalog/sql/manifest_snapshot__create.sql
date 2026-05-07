-- name: manifest_snapshot__create :exec
INSERT INTO package_hub_manifest_snapshots (
    id,
    package_version_id,
    schema_version,
    payload,
    validation_status,
    validation_errors,
    created_at
) VALUES (
    @id,
    @package_version_id,
    @schema_version,
    @payload::jsonb,
    @validation_status,
    @validation_errors::jsonb,
    @created_at
);
