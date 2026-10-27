-- name: runtime_completeexecution_update_current_run_outcome :exec
WITH owned_read AS MATERIALIZED (
  SELECT EXISTS (
    SELECT 1 FROM control_plane.run_nodes node
    JOIN control_plane.integration_invocations invocation ON invocation.node_id=node.id
      AND invocation.organization_id=node.organization_id AND invocation.run_id=node.run_id AND invocation.risk='READ'
    WHERE node.run_id=$1::uuid AND node.owner_rework_gate_id IS NOT NULL
      AND NOT EXISTS (SELECT 1 FROM control_plane.run_edges next_node WHERE next_node.source_node_id=node.id AND next_node.type='CONTINUES')
  ) AS pending
)
UPDATE control_plane.runs target
SET state=CASE WHEN $2='SUCCEEDED' AND (
      EXISTS(SELECT 1 FROM control_plane.required_workflow_launches launch WHERE launch.origin_run_id=target.id AND launch.state='OPEN')
      OR control_plane.repository_bundle_callback_pending(target.organization_id,target.id)
      OR (SELECT pending FROM owned_read)) THEN 'RUNNING' ELSE $2 END,
    result_summary=$3,safe_error_code=$4,safe_error_message=$5,
    finished_at=CASE WHEN $2='SUCCEEDED' AND (
      EXISTS(SELECT 1 FROM control_plane.required_workflow_launches launch WHERE launch.origin_run_id=target.id AND launch.state='OPEN')
      OR control_plane.repository_bundle_callback_pending(target.organization_id,target.id)
      OR (SELECT pending FROM owned_read)) THEN NULL ELSE clock_timestamp() END,
    version=version+1,updated_at=clock_timestamp()
WHERE target.id=$1::uuid AND target.id<>target.root_run_id
