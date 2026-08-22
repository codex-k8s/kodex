-- name: platform__workers_claimdueschedules_insert_schedule_occurrences_ref_schedule_id_state :exec
INSERT INTO control_plane.schedule_occurrences(ref,organization_id,schedule_id,scheduled_for,state,lease_ref,fence_digest,generation,workload_instance,lease_expires_at) VALUES($1,$2::uuid,$3::uuid,$4,'CLAIMED',$5,$6,1,$7,$8)
