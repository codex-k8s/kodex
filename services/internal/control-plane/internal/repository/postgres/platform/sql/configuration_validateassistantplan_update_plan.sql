-- name: configuration_validateassistantplan_update_plan :exec
UPDATE control_plane.assistant_plans
SET state=$2,validated_revision=$3,validation_problems=$4,validated_at=clock_timestamp(),version=version+1
WHERE id=$1::uuid
