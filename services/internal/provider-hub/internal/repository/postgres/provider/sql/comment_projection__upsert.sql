-- name: comment_projection__upsert :one
INSERT INTO provider_hub_comment_projections (
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
) VALUES (
    @id,
    @work_item_projection_id,
    @provider_comment_id,
    @kind,
    @review_state,
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
    kind = CASE
        WHEN provider_hub_comment_projections.provider_updated_at IS NULL
            OR (
                EXCLUDED.provider_updated_at IS NOT NULL
                AND EXCLUDED.provider_updated_at >= provider_hub_comment_projections.provider_updated_at
            )
        THEN EXCLUDED.kind
        ELSE provider_hub_comment_projections.kind
    END,
    review_state = CASE
        WHEN provider_hub_comment_projections.provider_updated_at IS NULL
            OR (
                EXCLUDED.provider_updated_at IS NOT NULL
                AND EXCLUDED.provider_updated_at >= provider_hub_comment_projections.provider_updated_at
            )
        THEN EXCLUDED.review_state
        ELSE provider_hub_comment_projections.review_state
    END,
    author_provider_login = CASE
        WHEN provider_hub_comment_projections.provider_updated_at IS NULL
            OR (
                EXCLUDED.provider_updated_at IS NOT NULL
                AND EXCLUDED.provider_updated_at >= provider_hub_comment_projections.provider_updated_at
            )
        THEN EXCLUDED.author_provider_login
        ELSE provider_hub_comment_projections.author_provider_login
    END,
    body_digest = CASE
        WHEN provider_hub_comment_projections.provider_updated_at IS NULL
            OR (
                EXCLUDED.provider_updated_at IS NOT NULL
                AND EXCLUDED.provider_updated_at >= provider_hub_comment_projections.provider_updated_at
            )
        THEN EXCLUDED.body_digest
        ELSE provider_hub_comment_projections.body_digest
    END,
    summary = CASE
        WHEN provider_hub_comment_projections.provider_updated_at IS NULL
            OR (
                EXCLUDED.provider_updated_at IS NOT NULL
                AND EXCLUDED.provider_updated_at >= provider_hub_comment_projections.provider_updated_at
            )
        THEN EXCLUDED.summary
        ELSE provider_hub_comment_projections.summary
    END,
    provider_created_at = CASE
        WHEN provider_hub_comment_projections.provider_updated_at IS NULL
            OR (
                EXCLUDED.provider_updated_at IS NOT NULL
                AND EXCLUDED.provider_updated_at >= provider_hub_comment_projections.provider_updated_at
            )
        THEN COALESCE(EXCLUDED.provider_created_at, provider_hub_comment_projections.provider_created_at)
        ELSE provider_hub_comment_projections.provider_created_at
    END,
    provider_updated_at = CASE
        WHEN provider_hub_comment_projections.provider_updated_at IS NULL
            OR (
                EXCLUDED.provider_updated_at IS NOT NULL
                AND EXCLUDED.provider_updated_at >= provider_hub_comment_projections.provider_updated_at
            )
        THEN EXCLUDED.provider_updated_at
        ELSE provider_hub_comment_projections.provider_updated_at
    END,
    version = CASE
        WHEN provider_hub_comment_projections.provider_updated_at IS NULL
            OR (
                EXCLUDED.provider_updated_at IS NOT NULL
                AND EXCLUDED.provider_updated_at >= provider_hub_comment_projections.provider_updated_at
            )
        THEN provider_hub_comment_projections.version + 1
        ELSE provider_hub_comment_projections.version
    END,
    updated_at = CASE
        WHEN provider_hub_comment_projections.provider_updated_at IS NULL
            OR (
                EXCLUDED.provider_updated_at IS NOT NULL
                AND EXCLUDED.provider_updated_at >= provider_hub_comment_projections.provider_updated_at
            )
        THEN EXCLUDED.updated_at
        ELSE provider_hub_comment_projections.updated_at
    END
RETURNING
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
    updated_at;
