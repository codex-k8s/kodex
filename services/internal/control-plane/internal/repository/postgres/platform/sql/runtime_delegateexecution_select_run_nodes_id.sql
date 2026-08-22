-- name: runtime_delegateexecution_select_run_nodes_id :one
SELECT 'platform.run.delegate'=ANY(a.capabilities) FROM control_plane.run_nodes n JOIN control_plane.agents a ON a.id=n.agent_id WHERE n.id=$1::uuid
