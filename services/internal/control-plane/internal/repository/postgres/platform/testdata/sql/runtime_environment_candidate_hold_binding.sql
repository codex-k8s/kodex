-- name: runtime_environment_candidate_hold_binding :one
SELECT binding.ref FROM control_plane.agent_runtime_environment_bindings binding WHERE binding.id=$1::uuid FOR UPDATE;
