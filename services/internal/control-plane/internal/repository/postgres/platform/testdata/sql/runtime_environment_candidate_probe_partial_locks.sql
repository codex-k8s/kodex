-- name: runtime_environment_candidate_probe_partial_locks :one
WITH environment AS MATERIALIZED (
    SELECT target.ref FROM control_plane.runtime_environment_sets target
    WHERE target.ref=$1 AND target.organization_id=$3::uuid
    FOR UPDATE OF target NOWAIT
)
SELECT agent.ref FROM environment
JOIN control_plane.agents agent ON agent.id=$2::uuid AND agent.organization_id=$3::uuid
FOR UPDATE OF agent NOWAIT;
