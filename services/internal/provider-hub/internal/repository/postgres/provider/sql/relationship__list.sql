-- name: relationship__list :many
SELECT
    id,
    source_work_item_id,
    target_work_item_id,
    target_provider_ref,
    relationship_type,
    source,
    confidence,
    version,
    created_at
FROM provider_hub_relationships
WHERE (
    @work_item_projection_id::uuid IS NULL
    OR source_work_item_id = @work_item_projection_id
    OR target_work_item_id = @work_item_projection_id
)
  AND (cardinality(@relationship_types::text[]) = 0 OR relationship_type = ANY(@relationship_types::text[]))
  AND (cardinality(@sources::text[]) = 0 OR source = ANY(@sources::text[]))
  AND (cardinality(@confidence_levels::text[]) = 0 OR confidence = ANY(@confidence_levels::text[]))
ORDER BY created_at DESC, id
LIMIT @limit::integer OFFSET @offset::integer;
