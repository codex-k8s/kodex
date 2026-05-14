-- name: flow_version__supersede :exec
UPDATE agent_manager_flow_versions
SET status = 'superseded'
WHERE flow_id = @flow_id
  AND id <> @id
  AND status = 'active';
