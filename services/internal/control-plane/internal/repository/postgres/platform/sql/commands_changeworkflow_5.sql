-- name: platform__commands_changeworkflow_5 :exec
UPDATE control_plane.workflows SET state='VALID',version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid
