-- name: relationship__upsert :one
INSERT INTO provider_hub_relationships (
    id,
    source_work_item_id,
    target_work_item_id,
    target_provider_ref,
    relationship_type,
    source,
    confidence,
    created_at
) VALUES (
    @id,
    @source_work_item_id,
    @target_work_item_id,
    @target_provider_ref,
    @relationship_type,
    @source,
    @confidence,
    @created_at
)
ON CONFLICT (
    source_work_item_id,
    COALESCE(target_work_item_id, '00000000-0000-0000-0000-000000000000'::uuid),
    target_provider_ref,
    relationship_type
) DO UPDATE SET
    source = EXCLUDED.source,
    confidence = EXCLUDED.confidence
RETURNING
    id,
    source_work_item_id,
    target_work_item_id,
    target_provider_ref,
    relationship_type,
    source,
    confidence,
    created_at;
