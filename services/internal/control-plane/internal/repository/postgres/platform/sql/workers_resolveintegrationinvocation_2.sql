-- name: platform__workers_resolveintegrationinvocation_2 :exec
INSERT INTO control_plane.integration_invocations(ref,organization_id,run_id,node_id,connection_id,grant_id,capability_key,operation,input_digest,bounded_input,effect_fence_digest,state) VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,$7,$7,$8,$9,$10,'READY')
