-- name: prompt_continuation_template :one
SELECT revision.ref, revision.digest, revision.content
FROM control_plane.managed_configuration_bindings binding
JOIN control_plane.agents agent ON agent.ref = binding.consumer_ref
 AND agent.organization_id = binding.organization_id
 AND agent.project_id IS NOT DISTINCT FROM binding.project_id
JOIN control_plane.managed_configuration_sets configuration ON configuration.id = binding.configuration_set_id
 AND configuration.organization_id = binding.organization_id AND configuration.kind = 'PROMPT_TEMPLATE'
 AND configuration.project_id IS NOT DISTINCT FROM binding.project_id
JOIN control_plane.managed_configuration_revisions revision ON revision.id = binding.configuration_revision_id
 AND revision.organization_id = binding.organization_id AND revision.configuration_set_id = configuration.id
WHERE binding.organization_id = @organization_id::uuid AND binding.consumer_ref = @agent_ref
  AND binding.consumer_kind = 'AGENT_CONTINUATION' AND binding.configuration_kind = 'PROMPT_TEMPLATE'
  AND revision.state IN ('PUBLISHED', 'SUPERSEDED') AND revision.published_at IS NOT NULL;
