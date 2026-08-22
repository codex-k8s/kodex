-- name: platform__commands_changerun_update_runtime_leases_state_updated_at :exec
UPDATE control_plane.runtime_leases SET state='CANCELLED',updated_at=clock_timestamp() WHERE run_id IN (SELECT id FROM control_plane.runs WHERE root_run_id=$1::uuid) AND state='CLAIMED'
