-- name: relationship__get_by_identity :one
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
WHERE source_work_item_id = @source_work_item_id
  AND COALESCE(target_work_item_id, '00000000-0000-0000-0000-000000000000'::uuid) =
      COALESCE(@target_work_item_id::uuid, '00000000-0000-0000-0000-000000000000'::uuid)
  AND target_provider_ref = @target_provider_ref
  AND relationship_type = @relationship_type;
