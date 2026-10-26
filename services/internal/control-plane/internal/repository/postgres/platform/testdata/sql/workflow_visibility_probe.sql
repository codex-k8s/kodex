-- name: workflow_visibility_probe :one
WITH execution AS (
    SELECT node.*,root.project_id,root.initiated_by AS actor_id
    FROM control_plane.run_nodes node
    JOIN control_plane.runs root ON root.id=node.root_run_id AND root.organization_id=node.organization_id
    WHERE node.ref=@node_ref
), pins AS (
    SELECT pin.* FROM execution
    CROSS JOIN LATERAL control_plane.workflow_runtime_artifact_pins(
        CASE WHEN @fault='tenant' THEN '00000000-0000-4000-8000-000000000001'::uuid ELSE execution.organization_id END,
        CASE WHEN @fault='node' THEN '00000000-0000-4000-8000-000000000001'::uuid
             WHEN @fault='unassigned' THEN (SELECT ordinary.id FROM control_plane.run_nodes ordinary
                 WHERE ordinary.organization_id=execution.organization_id AND ordinary.run_id=execution.run_id
                     AND ordinary.workflow_business_attempt=0 ORDER BY ordinary.id LIMIT 1)
             ELSE execution.id END
    ) pin
)
SELECT count(*) FROM pins JOIN control_plane.artifact_history artifact
    ON artifact.id=pins.artifact_id AND artifact.revision_id=pins.revision_id
CROSS JOIN execution
WHERE CASE WHEN @fault='revision' THEN
    control_plane.runtime_workflow_input_visible(execution.organization_id,execution.id,'00000000-0000-4000-8000-000000000001'::uuid)
ELSE control_plane.runtime_file_source_visible(execution.organization_id,
    CASE WHEN @fault='actor' THEN '00000000-0000-4000-8000-000000000001'::uuid ELSE execution.actor_id END,
    CASE WHEN @fault='project' THEN '00000000-0000-4000-8000-000000000001'::uuid ELSE execution.project_id END,
    CASE WHEN @fault='agent' THEN '00000000-0000-4000-8000-000000000001'::uuid ELSE execution.agent_id END,
    artifact.id,'RUN_RESULT','',execution.id) END;
