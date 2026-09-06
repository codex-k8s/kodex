-- name: queries_attachinstructions_select_instruction_versions_organization_id_agent_id_ref :many
SELECT i.ref,i.version_number,i.state,i.content,i.digest,i.core,COALESCE(i.parent_ref,''),i.validation_problems,i.created_at,i.published_at,
 b.ref,b.version,active.ref,NOT EXISTS(SELECT 1 FROM control_plane.managed_configuration_bindings managed
  WHERE managed.organization_id=a.organization_id AND managed.project_id IS NOT DISTINCT FROM a.project_id
   AND managed.configuration_kind='PROMPT_TEMPLATE' AND managed.consumer_kind='AGENT' AND managed.consumer_ref=a.ref)
FROM control_plane.agents a
JOIN control_plane.agent_instruction_bindings b ON b.agent_id=a.id AND b.organization_id=a.organization_id
JOIN control_plane.instruction_versions active ON active.id=b.instruction_id AND active.agent_id=a.id AND active.organization_id=a.organization_id
JOIN control_plane.instruction_versions i ON i.agent_id=a.id AND i.organization_id=a.organization_id
WHERE a.organization_id=$1::uuid AND a.ref=$2
ORDER BY i.version_number DESC
