-- name: configuration_applyassistantplancommand_update_assistant_plans_state_version_applied_at :exec
UPDATE control_plane.assistant_plans SET state='APPLIED',version=version+1,applied_at=clock_timestamp() WHERE id=$1::uuid
