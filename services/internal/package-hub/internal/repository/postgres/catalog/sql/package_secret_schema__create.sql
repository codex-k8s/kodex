-- name: package_secret_schema__create :exec
INSERT INTO package_hub_package_secret_schemas (
    id,
    package_version_id,
    schema_digest,
    fields,
    created_at
) VALUES (
    @id,
    @package_version_id,
    @schema_digest,
    @fields::jsonb,
    @created_at
);
