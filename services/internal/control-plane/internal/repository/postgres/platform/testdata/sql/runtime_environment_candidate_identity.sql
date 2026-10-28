-- name: runtime_environment_candidate_identity :one
SELECT agent.id::text, environment.ref, version.id::text, version.digest,
       binding.id::text, binding.ref, binding.version, binding.digest, agent.organization_id::text
FROM control_plane.agents agent
JOIN control_plane.agent_runtime_environment_bindings binding ON binding.agent_id=agent.id
JOIN control_plane.runtime_environment_sets environment ON environment.id=binding.environment_set_id
JOIN control_plane.runtime_environment_versions version ON version.environment_set_id=environment.id AND version.ref=$2
WHERE agent.ref=$1;
