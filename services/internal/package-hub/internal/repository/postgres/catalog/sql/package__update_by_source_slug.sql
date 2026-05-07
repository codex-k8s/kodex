-- name: package__update_by_source_slug :one
WITH existing AS (
    SELECT
        id,
        (
            package_kind IS DISTINCT FROM @package_kind
            OR publisher_ref IS DISTINCT FROM @publisher_ref
            OR display_name IS DISTINCT FROM @display_name::jsonb
            OR description IS DISTINCT FROM @description::jsonb
            OR icon_object_uri IS DISTINCT FROM @icon_object_uri
            OR commercial_status IS DISTINCT FROM @commercial_status
            OR trust_status IS DISTINCT FROM @trust_status
            OR status IS DISTINCT FROM @status
        ) AS changed
    FROM package_hub_packages
    WHERE COALESCE(source_id, '00000000-0000-0000-0000-000000000000'::uuid) =
          COALESCE(@source_id::uuid, '00000000-0000-0000-0000-000000000000'::uuid)
      AND slug = @slug
    FOR UPDATE
),
updated AS (
    UPDATE package_hub_packages p
    SET
        package_kind = @package_kind,
        publisher_ref = @publisher_ref,
        display_name = @display_name::jsonb,
        description = @description::jsonb,
        icon_object_uri = @icon_object_uri,
        commercial_status = @commercial_status,
        trust_status = @trust_status,
        status = @status,
        version = CASE WHEN existing.changed THEN p.version + 1 ELSE p.version END,
        updated_at = CASE WHEN existing.changed THEN @updated_at ELSE p.updated_at END
    FROM existing
    WHERE p.id = existing.id
    RETURNING
        p.id,
        p.source_id,
        p.slug,
        p.package_kind,
        p.publisher_ref,
        p.display_name,
        p.description,
        p.icon_object_uri,
        p.commercial_status,
        p.trust_status,
        p.status,
        p.version,
        p.created_at,
        p.updated_at,
        existing.changed
)
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
    updated_at,
    false AS inserted,
    changed
FROM updated;
