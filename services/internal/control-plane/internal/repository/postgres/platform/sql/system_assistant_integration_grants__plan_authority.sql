-- name: system_assistant_integration_grants__plan_authority :one
SELECT plan.operations
FROM control_plane.assistant_plans plan
JOIN control_plane.assistant_conversations conversation
  ON conversation.ref=plan.conversation_ref AND conversation.organization_id=plan.organization_id
WHERE plan.organization_id=$1::uuid AND conversation.created_by=$2::uuid AND plan.ref=$3;
