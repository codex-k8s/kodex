-- name: skill_bundle_disable_bindings :exec
UPDATE control_plane.agent_context_bindings SET enabled=false,version=version+1,updated_at=clock_timestamp()
WHERE organization_id=$1::uuid AND skill_bundle_id=$2::uuid AND enabled;
