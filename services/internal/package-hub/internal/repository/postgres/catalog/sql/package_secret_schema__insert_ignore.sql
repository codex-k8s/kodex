-- name: package_secret_schema__insert_ignore :exec
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
)
ON CONFLICT (package_version_id, schema_digest) DO NOTHING;
