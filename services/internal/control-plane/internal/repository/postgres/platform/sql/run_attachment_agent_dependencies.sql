-- name: run_attachment_agent_dependencies :one
SELECT a.version, a.capabilities, COALESCE(i.ref, ''), COALESCE(i.digest, '')
FROM control_plane.agents a
LEFT JOIN LATERAL (
 SELECT source.ref,source.digest FROM (
  SELECT revision.ref,revision.digest,1 AS priority
  FROM control_plane.managed_configuration_bindings binding
  JOIN control_plane.managed_configuration_sets configuration ON configuration.id=binding.configuration_set_id
   AND configuration.organization_id=a.organization_id AND configuration.project_id IS NOT DISTINCT FROM a.project_id
   AND configuration.kind='PROMPT_TEMPLATE'
  JOIN control_plane.managed_configuration_revisions revision ON revision.id=binding.configuration_revision_id
   AND revision.configuration_set_id=configuration.id AND revision.state IN ('PUBLISHED','SUPERSEDED')
  WHERE binding.organization_id=a.organization_id AND binding.project_id IS NOT DISTINCT FROM a.project_id
   AND binding.configuration_kind='PROMPT_TEMPLATE' AND binding.consumer_kind='AGENT' AND binding.consumer_ref=a.ref
  UNION ALL
  SELECT instruction.ref,instruction.digest,2
  FROM control_plane.agent_instruction_bindings binding
  JOIN control_plane.instruction_versions instruction ON instruction.id=binding.instruction_id
   AND instruction.agent_id=a.id AND instruction.organization_id=a.organization_id AND instruction.state='PUBLISHED'
  WHERE binding.agent_id=a.id AND binding.organization_id=a.organization_id
 ) source ORDER BY source.priority LIMIT 1
) i ON true
WHERE a.organization_id=@organization_id::uuid AND a.project_id=@project_id::uuid
  AND a.ref=@agent_ref
