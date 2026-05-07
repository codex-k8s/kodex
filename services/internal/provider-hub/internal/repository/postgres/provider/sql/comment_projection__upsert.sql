-- name: comment_projection__upsert :one
INSERT INTO provider_hub_comment_projections (
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
) VALUES (
    @id,
    @work_item_projection_id,
    @provider_comment_id,
    @kind,
    @author_provider_login,
    @body_digest,
    @summary,
    @provider_created_at,
    @provider_updated_at,
    @version,
    @created_at,
    @updated_at
)
ON CONFLICT (work_item_projection_id, provider_comment_id) DO UPDATE SET
    kind = EXCLUDED.kind,
    author_provider_login = EXCLUDED.author_provider_login,
    body_digest = EXCLUDED.body_digest,
    summary = EXCLUDED.summary,
    provider_created_at = COALESCE(EXCLUDED.provider_created_at, provider_hub_comment_projections.provider_created_at),
    provider_updated_at = EXCLUDED.provider_updated_at,
    version = provider_hub_comment_projections.version + 1,
    updated_at = EXCLUDED.updated_at
RETURNING
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
    updated_at;
