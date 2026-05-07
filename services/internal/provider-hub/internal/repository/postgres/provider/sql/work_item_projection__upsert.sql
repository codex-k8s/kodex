-- name: work_item_projection__upsert :one
INSERT INTO provider_hub_work_item_projections (
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
) VALUES (
    @id,
    @provider_slug,
    @provider_work_item_id,
    @project_id,
    @repository_id,
    @repository_full_name,
    @kind,
    @number,
    @url,
    @title,
    @state,
    @work_item_type,
    @labels_json,
    @assignees_json,
    @milestone,
    @project_fields_json,
    @watermark_status,
    @watermark_json,
    @body_digest,
    @provider_updated_at,
    @synced_at,
    @drift_status,
    @version,
    @created_at,
    @updated_at
)
ON CONFLICT (provider_slug, provider_work_item_id) DO UPDATE SET
    project_id = CASE
        WHEN provider_hub_work_item_projections.provider_updated_at IS NULL
            OR (
                EXCLUDED.provider_updated_at IS NOT NULL
                AND EXCLUDED.provider_updated_at >= provider_hub_work_item_projections.provider_updated_at
            )
        THEN COALESCE(EXCLUDED.project_id, provider_hub_work_item_projections.project_id)
        ELSE provider_hub_work_item_projections.project_id
    END,
    repository_id = CASE
        WHEN provider_hub_work_item_projections.provider_updated_at IS NULL
            OR (
                EXCLUDED.provider_updated_at IS NOT NULL
                AND EXCLUDED.provider_updated_at >= provider_hub_work_item_projections.provider_updated_at
            )
        THEN COALESCE(EXCLUDED.repository_id, provider_hub_work_item_projections.repository_id)
        ELSE provider_hub_work_item_projections.repository_id
    END,
    repository_full_name = CASE
        WHEN provider_hub_work_item_projections.provider_updated_at IS NULL
            OR (
                EXCLUDED.provider_updated_at IS NOT NULL
                AND EXCLUDED.provider_updated_at >= provider_hub_work_item_projections.provider_updated_at
            )
        THEN EXCLUDED.repository_full_name
        ELSE provider_hub_work_item_projections.repository_full_name
    END,
    kind = CASE
        WHEN provider_hub_work_item_projections.provider_updated_at IS NULL
            OR (
                EXCLUDED.provider_updated_at IS NOT NULL
                AND EXCLUDED.provider_updated_at >= provider_hub_work_item_projections.provider_updated_at
            )
        THEN EXCLUDED.kind
        ELSE provider_hub_work_item_projections.kind
    END,
    number = CASE
        WHEN provider_hub_work_item_projections.provider_updated_at IS NULL
            OR (
                EXCLUDED.provider_updated_at IS NOT NULL
                AND EXCLUDED.provider_updated_at >= provider_hub_work_item_projections.provider_updated_at
            )
        THEN EXCLUDED.number
        ELSE provider_hub_work_item_projections.number
    END,
    url = CASE
        WHEN provider_hub_work_item_projections.provider_updated_at IS NULL
            OR (
                EXCLUDED.provider_updated_at IS NOT NULL
                AND EXCLUDED.provider_updated_at >= provider_hub_work_item_projections.provider_updated_at
            )
        THEN EXCLUDED.url
        ELSE provider_hub_work_item_projections.url
    END,
    title = CASE
        WHEN provider_hub_work_item_projections.provider_updated_at IS NULL
            OR (
                EXCLUDED.provider_updated_at IS NOT NULL
                AND EXCLUDED.provider_updated_at >= provider_hub_work_item_projections.provider_updated_at
            )
        THEN EXCLUDED.title
        ELSE provider_hub_work_item_projections.title
    END,
    state = CASE
        WHEN provider_hub_work_item_projections.provider_updated_at IS NULL
            OR (
                EXCLUDED.provider_updated_at IS NOT NULL
                AND EXCLUDED.provider_updated_at >= provider_hub_work_item_projections.provider_updated_at
            )
        THEN EXCLUDED.state
        ELSE provider_hub_work_item_projections.state
    END,
    work_item_type = CASE
        WHEN provider_hub_work_item_projections.provider_updated_at IS NULL
            OR (
                EXCLUDED.provider_updated_at IS NOT NULL
                AND EXCLUDED.provider_updated_at >= provider_hub_work_item_projections.provider_updated_at
            )
        THEN EXCLUDED.work_item_type
        ELSE provider_hub_work_item_projections.work_item_type
    END,
    labels_json = CASE
        WHEN provider_hub_work_item_projections.provider_updated_at IS NULL
            OR (
                EXCLUDED.provider_updated_at IS NOT NULL
                AND EXCLUDED.provider_updated_at >= provider_hub_work_item_projections.provider_updated_at
            )
        THEN EXCLUDED.labels_json
        ELSE provider_hub_work_item_projections.labels_json
    END,
    assignees_json = CASE
        WHEN provider_hub_work_item_projections.provider_updated_at IS NULL
            OR (
                EXCLUDED.provider_updated_at IS NOT NULL
                AND EXCLUDED.provider_updated_at >= provider_hub_work_item_projections.provider_updated_at
            )
        THEN EXCLUDED.assignees_json
        ELSE provider_hub_work_item_projections.assignees_json
    END,
    milestone = CASE
        WHEN provider_hub_work_item_projections.provider_updated_at IS NULL
            OR (
                EXCLUDED.provider_updated_at IS NOT NULL
                AND EXCLUDED.provider_updated_at >= provider_hub_work_item_projections.provider_updated_at
            )
        THEN EXCLUDED.milestone
        ELSE provider_hub_work_item_projections.milestone
    END,
    project_fields_json = CASE
        WHEN provider_hub_work_item_projections.provider_updated_at IS NULL
            OR (
                EXCLUDED.provider_updated_at IS NOT NULL
                AND EXCLUDED.provider_updated_at >= provider_hub_work_item_projections.provider_updated_at
            )
        THEN EXCLUDED.project_fields_json
        ELSE provider_hub_work_item_projections.project_fields_json
    END,
    watermark_status = CASE
        WHEN provider_hub_work_item_projections.provider_updated_at IS NULL
            OR (
                EXCLUDED.provider_updated_at IS NOT NULL
                AND EXCLUDED.provider_updated_at >= provider_hub_work_item_projections.provider_updated_at
            )
        THEN EXCLUDED.watermark_status
        ELSE provider_hub_work_item_projections.watermark_status
    END,
    watermark_json = CASE
        WHEN provider_hub_work_item_projections.provider_updated_at IS NULL
            OR (
                EXCLUDED.provider_updated_at IS NOT NULL
                AND EXCLUDED.provider_updated_at >= provider_hub_work_item_projections.provider_updated_at
            )
        THEN EXCLUDED.watermark_json
        ELSE provider_hub_work_item_projections.watermark_json
    END,
    body_digest = CASE
        WHEN provider_hub_work_item_projections.provider_updated_at IS NULL
            OR (
                EXCLUDED.provider_updated_at IS NOT NULL
                AND EXCLUDED.provider_updated_at >= provider_hub_work_item_projections.provider_updated_at
            )
        THEN EXCLUDED.body_digest
        ELSE provider_hub_work_item_projections.body_digest
    END,
    provider_updated_at = CASE
        WHEN provider_hub_work_item_projections.provider_updated_at IS NULL
            OR (
                EXCLUDED.provider_updated_at IS NOT NULL
                AND EXCLUDED.provider_updated_at >= provider_hub_work_item_projections.provider_updated_at
            )
        THEN EXCLUDED.provider_updated_at
        ELSE provider_hub_work_item_projections.provider_updated_at
    END,
    synced_at = CASE
        WHEN provider_hub_work_item_projections.provider_updated_at IS NULL
            OR (
                EXCLUDED.provider_updated_at IS NOT NULL
                AND EXCLUDED.provider_updated_at >= provider_hub_work_item_projections.provider_updated_at
            )
        THEN EXCLUDED.synced_at
        ELSE provider_hub_work_item_projections.synced_at
    END,
    drift_status = CASE
        WHEN provider_hub_work_item_projections.provider_updated_at IS NULL
            OR (
                EXCLUDED.provider_updated_at IS NOT NULL
                AND EXCLUDED.provider_updated_at >= provider_hub_work_item_projections.provider_updated_at
            )
        THEN EXCLUDED.drift_status
        ELSE provider_hub_work_item_projections.drift_status
    END,
    version = CASE
        WHEN provider_hub_work_item_projections.provider_updated_at IS NULL
            OR (
                EXCLUDED.provider_updated_at IS NOT NULL
                AND EXCLUDED.provider_updated_at >= provider_hub_work_item_projections.provider_updated_at
            )
        THEN provider_hub_work_item_projections.version + 1
        ELSE provider_hub_work_item_projections.version
    END,
    updated_at = CASE
        WHEN provider_hub_work_item_projections.provider_updated_at IS NULL
            OR (
                EXCLUDED.provider_updated_at IS NOT NULL
                AND EXCLUDED.provider_updated_at >= provider_hub_work_item_projections.provider_updated_at
            )
        THEN EXCLUDED.updated_at
        ELSE provider_hub_work_item_projections.updated_at
    END
RETURNING
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
    updated_at;
