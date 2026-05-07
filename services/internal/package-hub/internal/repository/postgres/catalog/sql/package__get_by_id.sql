-- name: package__get_by_id :one
SELECT
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
FROM package_hub_packages
WHERE id = @id;
