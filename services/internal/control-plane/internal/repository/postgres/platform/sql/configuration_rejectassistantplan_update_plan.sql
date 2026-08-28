-- name: configuration_rejectassistantplan_update_plan :exec
UPDATE control_plane.assistant_plans SET state='REJECTED',version=version+1 WHERE id=$1::uuid
