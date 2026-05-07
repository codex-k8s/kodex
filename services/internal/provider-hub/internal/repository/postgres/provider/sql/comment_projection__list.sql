-- name: comment_projection__list :many
SELECT
    id,
    work_item_projection_id,
    provider_comment_id,
    kind,
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
  AND (cardinality(@kinds::text[]) = 0 OR kind = ANY(@kinds::text[]))
ORDER BY provider_updated_at DESC NULLS LAST, updated_at DESC, id
LIMIT @limit::integer OFFSET @offset::integer;
