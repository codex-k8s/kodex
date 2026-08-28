-- name: configuration_mark_assistant_plan_stale :exec
UPDATE control_plane.assistant_plans
SET state='STALE',validation_problems=$2,version=version+1
WHERE id=$1::uuid
