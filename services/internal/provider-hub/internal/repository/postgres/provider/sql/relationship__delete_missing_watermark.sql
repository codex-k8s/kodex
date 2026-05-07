-- name: relationship__delete_missing_watermark :exec
DELETE FROM provider_hub_relationships
WHERE source_work_item_id = @source_work_item_id
  AND source = 'watermark'
  AND relationship_type = ANY(@relationship_types::text[])
  AND id <> ALL(@relationship_ids::uuid[]);
