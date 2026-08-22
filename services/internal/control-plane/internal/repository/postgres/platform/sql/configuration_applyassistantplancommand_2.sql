-- name: platform__configuration_applyassistantplancommand_2 :exec
UPDATE control_plane.assistant_plans SET state='APPLIED',version=version+1,applied_at=clock_timestamp() WHERE id=$1::uuid
