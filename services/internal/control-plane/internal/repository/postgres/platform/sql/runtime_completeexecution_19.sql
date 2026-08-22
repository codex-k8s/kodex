-- name: platform__runtime_completeexecution_19 :exec
UPDATE control_plane.runs SET state='WAITING_HUMAN',version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid
