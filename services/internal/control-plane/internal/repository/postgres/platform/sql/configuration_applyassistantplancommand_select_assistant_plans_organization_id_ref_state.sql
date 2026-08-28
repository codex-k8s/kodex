-- name: configuration_applyassistantplancommand_select_assistant_plans_organization_id_ref_state :one
SELECT plan.id::text,plan.conversation_ref,plan.operations,plan.version,COALESCE(project.ref,'')
FROM control_plane.assistant_plans AS plan
JOIN control_plane.assistant_conversations AS conversation
  ON conversation.organization_id=plan.organization_id
 AND conversation.ref=plan.conversation_ref
LEFT JOIN control_plane.projects AS project
  ON project.organization_id=conversation.organization_id
 AND project.id=conversation.project_id
WHERE plan.organization_id=$1::uuid AND plan.ref=$2 AND plan.state='PROPOSED'
FOR UPDATE OF plan
