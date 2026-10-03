-- name: project_assistant__plan_conversation :one
SELECT conversation.ref
FROM control_plane.assistant_plans plan
JOIN control_plane.assistant_conversations conversation
  ON conversation.ref = plan.conversation_ref AND conversation.organization_id = plan.organization_id
WHERE plan.organization_id = @organization_id::uuid AND plan.ref = @plan_ref
  AND conversation.created_by = @actor_id::uuid;
