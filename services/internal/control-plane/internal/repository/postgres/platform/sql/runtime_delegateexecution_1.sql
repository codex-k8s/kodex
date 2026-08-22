-- name: platform__runtime_delegateexecution_1 :one
SELECT 'platform.run.delegate'=ANY(a.capabilities) FROM control_plane.run_nodes n JOIN control_plane.agents a ON a.id=n.agent_id WHERE n.id=$1::uuid
