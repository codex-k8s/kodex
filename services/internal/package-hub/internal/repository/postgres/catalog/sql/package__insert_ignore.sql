-- name: package__insert_ignore :exec
INSERT INTO package_hub_packages (
    id,
    source_id,
    slug,
    package_kind,
    publisher_ref,
    display_name,
    description,
    icon_object_uri,
    commercial_status,
    trust_status,
    status,
    version,
    created_at,
    updated_at
) VALUES (
    @id,
    @source_id::uuid,
    @slug,
    @package_kind,
    @publisher_ref,
    @display_name::jsonb,
    @description::jsonb,
    @icon_object_uri,
    @commercial_status,
    @trust_status,
    @status,
    @version,
    @created_at,
    @updated_at
)
ON CONFLICT DO NOTHING;
