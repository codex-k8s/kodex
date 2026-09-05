-- name: prompt_preview_agent :one
SELECT agent.ref, agent.name, agent.purpose, agent.version,
       COALESCE(project.ref, ''), COALESCE(project.name, ''),
       COALESCE(project.language, 'en'), instruction.ref, instruction.content || CASE
           WHEN agent.system_key = 'system-assistant' AND COALESCE(assistant.owner_instructions, '') <> ''
               THEN E'\n\n<owner-instructions>\n' || assistant.owner_instructions || E'\n</owner-instructions>'
           ELSE '' END,
       instruction.digest, agent.capabilities
FROM control_plane.agents agent
LEFT JOIN control_plane.projects project ON project.id = agent.project_id
LEFT JOIN control_plane.assistant_runtime assistant ON assistant.agent_id = agent.id
JOIN LATERAL (
    SELECT source.ref, source.content, source.digest
    FROM (
        SELECT revision.ref, revision.content, revision.digest, revision.revision,
               1 AS priority
        FROM control_plane.managed_configuration_bindings binding
        JOIN control_plane.managed_configuration_sets configuration
          ON configuration.id = binding.configuration_set_id
         AND configuration.kind = 'PROMPT_TEMPLATE'
         AND configuration.organization_id = agent.organization_id
         AND configuration.project_id IS NOT DISTINCT FROM agent.project_id
        JOIN control_plane.managed_configuration_revisions revision
          ON revision.id = binding.configuration_revision_id
         AND revision.configuration_set_id = configuration.id
         AND revision.state IN ('PUBLISHED', 'SUPERSEDED')
        WHERE binding.organization_id = agent.organization_id
          AND binding.project_id IS NOT DISTINCT FROM agent.project_id
          AND binding.configuration_kind = 'PROMPT_TEMPLATE'
          AND binding.consumer_kind = 'AGENT' AND binding.consumer_ref = agent.ref
        UNION ALL
        SELECT value.ref, value.content, value.digest, value.version_number::bigint, 2
        FROM control_plane.instruction_versions value
        WHERE value.agent_id = agent.id AND value.state = 'PUBLISHED'
    ) source
    ORDER BY source.priority, source.revision DESC
    LIMIT 1
) instruction ON true
WHERE agent.organization_id = @organization_id::uuid
  AND agent.ref = @agent_ref
  AND agent.enabled AND agent.state <> 'ARCHIVED';
