-- name: commands_resolvegate_select_active_agent_nodes :one
SELECT count(*) + (SELECT count(*) FROM control_plane.required_workflow_launches WHERE origin_root_run_id=$1::uuid AND state='OPEN')
 + (SELECT count(*) FROM control_plane.repository_bundle_publications WHERE root_run_id=$1::uuid
      AND (state IN ('WAITING_APPROVAL','QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME') OR continuation_state='BLOCKED'))
FROM control_plane.run_nodes
WHERE root_run_id=$1::uuid
  AND type='AGENT_EXECUTION'
  AND state IN ('QUEUED','RUNNING','WAITING')
