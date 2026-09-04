-- name: managed_configuration_effective_prompt :one
SELECT source.ref, source.content, source.digest, source.revision, source.created_at, source.published_at
FROM control_plane.agents agent
JOIN LATERAL (
    SELECT revision.ref, revision.content, revision.digest, revision.revision,
           revision.created_at, revision.published_at, 1 AS priority
    FROM control_plane.managed_configuration_bindings binding
    JOIN control_plane.managed_configuration_sets configuration
      ON configuration.id = binding.configuration_set_id
     AND configuration.kind = 'PROMPT_TEMPLATE'
     AND configuration.organization_id = agent.organization_id
     AND configuration.project_id = agent.project_id
    JOIN control_plane.managed_configuration_revisions revision
      ON revision.id = binding.configuration_revision_id
     AND revision.configuration_set_id = configuration.id
     AND revision.state IN ('PUBLISHED', 'SUPERSEDED')
    WHERE binding.organization_id = agent.organization_id
      AND binding.project_id = agent.project_id
      AND binding.configuration_kind = 'PROMPT_TEMPLATE'
      AND binding.consumer_kind = 'AGENT'
      AND binding.consumer_ref = agent.ref

    UNION ALL

    SELECT instruction.ref, instruction.content, instruction.digest,
           instruction.version_number::bigint, instruction.created_at, instruction.published_at, 2
    FROM control_plane.instruction_versions instruction
    WHERE instruction.agent_id = agent.id
      AND instruction.state = 'PUBLISHED'
) source ON true
WHERE agent.organization_id = @organization_id::uuid
  AND agent.ref = @agent_ref
ORDER BY source.priority, source.revision DESC
LIMIT 1;
