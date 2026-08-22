-- name: platform__runtime_claimexecution_insert_runtime_leases_ref_run_id_workload_instance :exec
INSERT INTO control_plane.runtime_leases(ref,organization_id,run_id,node_id,workload_instance,fence_digest,generation,state,input_digest,expires_at) VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5,$6,$7,'CLAIMED',$8,$9)
