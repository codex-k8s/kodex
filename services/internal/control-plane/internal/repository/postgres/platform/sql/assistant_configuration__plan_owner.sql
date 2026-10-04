-- name: assistant_configuration__plan_owner :one
SELECT plan.conversation_ref,plan.operations
FROM control_plane.assistant_plans plan
WHERE plan.organization_id=@organization_id::uuid AND plan.ref=@plan_ref;
