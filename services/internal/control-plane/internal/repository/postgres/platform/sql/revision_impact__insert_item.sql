-- name: revision_impact__insert_item :exec
INSERT INTO control_plane.revision_impact_items(plan_id,ref,snapshot)
VALUES(@plan_id::uuid,@ref,@snapshot::jsonb);
