-- name: secret_draft_impact_items_insert :exec
INSERT INTO control_plane.runtime_secret_draft_impact_items(ref,plan_id,snapshot)
SELECT item->>'Ref',@plan_id::uuid,item->'Consumer' FROM jsonb_array_elements(@items::jsonb) item;
