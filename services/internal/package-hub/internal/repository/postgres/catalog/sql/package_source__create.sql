-- name: package_source__create :exec
INSERT INTO package_hub_package_sources (
    id,
    organization_id,
    slug,
    display_name,
    source_kind,
    repository_ref,
    catalog_endpoint_ref,
    status,
    last_sync_at,
    last_error,
    version,
    created_at,
    updated_at
) VALUES (
    @id,
    @organization_id::uuid,
    @slug,
    @display_name,
    @source_kind,
    @repository_ref,
    @catalog_endpoint_ref,
    @status,
    @last_sync_at::timestamptz,
    @last_error,
    @version,
    @created_at,
    @updated_at
);
