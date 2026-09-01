-- name: configuration_rejectassistantplan_select_plan :one
SELECT id::text,state,version,current_revision
FROM control_plane.assistant_plans
WHERE organization_id=$1::uuid AND ref=$2
FOR UPDATE
