-- name: commands_resolvegate_complete_root_run :exec
UPDATE control_plane.runs
SET state='SUCCEEDED',
    finished_at=clock_timestamp(),
    version=version+1,
    updated_at=clock_timestamp()
WHERE id=$1::uuid
