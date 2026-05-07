-- name: work_item_projection__list :many
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
WHERE (@project_id::uuid IS NULL OR project_id = @project_id)
  AND (@repository_id::uuid IS NULL OR repository_id = @repository_id)
  AND (@provider_slug = '' OR provider_slug = @provider_slug)
  AND (@repository_full_name = '' OR repository_full_name = @repository_full_name)
  AND (cardinality(@kinds::text[]) = 0 OR kind = ANY(@kinds::text[]))
  AND (cardinality(@states::text[]) = 0 OR state = ANY(@states::text[]))
  AND (cardinality(@labels::text[]) = 0 OR labels_json @> to_jsonb(@labels::text[]))
  AND (cardinality(@work_item_types::text[]) = 0 OR work_item_type = ANY(@work_item_types::text[]))
  AND (cardinality(@drift_statuses::text[]) = 0 OR drift_status = ANY(@drift_statuses::text[]))
  AND (@updated_since::timestamptz IS NULL OR provider_updated_at >= @updated_since)
ORDER BY provider_updated_at DESC NULLS LAST, updated_at DESC, id
LIMIT @limit::integer OFFSET @offset::integer;
