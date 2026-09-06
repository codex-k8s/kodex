-- name: instruction_impact_snapshot :one
SELECT a.id::text,COALESCE(a.system_key,''),a.version,p.id::text,p.ref,
 i.ref,i.version_number,i.state,i.content,i.digest,b.ref,b.version,active.ref,
 NOT EXISTS(SELECT 1 FROM control_plane.managed_configuration_bindings m WHERE m.organization_id=a.organization_id
  AND m.project_id IS NOT DISTINCT FROM a.project_id AND m.configuration_kind='PROMPT_TEMPLATE'
  AND m.consumer_kind='AGENT' AND m.consumer_ref=a.ref)
FROM control_plane.agents a
JOIN control_plane.projects p ON p.id=a.project_id AND p.organization_id=a.organization_id
JOIN control_plane.agent_instruction_bindings b ON b.agent_id=a.id AND b.organization_id=a.organization_id
JOIN control_plane.instruction_versions active ON active.id=b.instruction_id AND active.agent_id=a.id AND active.organization_id=a.organization_id
JOIN control_plane.instruction_versions i ON i.agent_id=a.id AND i.organization_id=a.organization_id
WHERE a.organization_id=@organization_id::uuid AND a.ref=@agent_ref AND a.state<>'ARCHIVED'
 AND ((@revision_ref='' AND i.state IN ('DRAFT','VALID','INVALID')) OR i.ref=@revision_ref)
FOR UPDATE OF a,b,i;
