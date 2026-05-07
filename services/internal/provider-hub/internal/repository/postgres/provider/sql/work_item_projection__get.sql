-- name: work_item_projection__get :one
SELECT
    id,
    provider_slug,
    provider_work_item_id,
    project_id,
    repository_id,
    repository_full_name,
    kind,
    number,
    url,
    title,
    state,
    work_item_type,
    labels_json,
    assignees_json,
    milestone,
    project_fields_json,
    watermark_status,
    watermark_json,
    body_digest,
    provider_updated_at,
    synced_at,
    drift_status,
    version,
    created_at,
    updated_at
FROM provider_hub_work_item_projections
WHERE
    (@id::uuid IS NOT NULL AND id = @id)
    OR (
        @id::uuid IS NULL
        AND provider_slug = @provider_slug
        AND (
            (@provider_object_id <> '' AND provider_work_item_id = @provider_object_id)
            OR (@web_url <> '' AND url = @web_url)
            OR (
                @repository_full_name <> ''
                AND @kind <> ''
                AND @number::bigint > 0
                AND repository_full_name = @repository_full_name
                AND kind = @kind
                AND number = @number
            )
        )
    )
ORDER BY updated_at DESC
LIMIT 1;
