-- name: context_binding_count :one
SELECT count(*) FROM control_plane.agent_context_bindings
WHERE organization_id=$1::uuid AND agent_id=$2::uuid AND enabled;
