-- name: workflow_effect__append :exec
UPDATE control_plane.run_nodes node
SET workflow_effect_intents=CASE WHEN @kind='intent' THEN node.workflow_effect_intents || jsonb_build_array(@record::jsonb) ELSE node.workflow_effect_intents END,
    workflow_effect_rejoins=CASE WHEN @kind='rejoin' THEN node.workflow_effect_rejoins || jsonb_build_array(@record::jsonb) ELSE node.workflow_effect_rejoins END
WHERE node.organization_id=@organization_id::uuid AND node.root_run_id=@root_run_id::uuid AND node.id=@node_id::uuid
    AND @kind IN ('intent','rejoin');
