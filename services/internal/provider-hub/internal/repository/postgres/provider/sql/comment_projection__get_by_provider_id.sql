-- name: comment_projection__get_by_provider_id :one
SELECT
    id,
    work_item_projection_id,
    provider_comment_id,
    kind,
    review_state,
    author_provider_login,
    body_digest,
    summary,
    provider_created_at,
    provider_updated_at,
    version,
    created_at,
    updated_at
FROM provider_hub_comment_projections
WHERE work_item_projection_id = @work_item_projection_id
  AND provider_comment_id = @provider_comment_id
LIMIT 1;
