-- name: resumable_sessions__candidates :many
WITH latest AS MATERIALIZED (
    SELECT DISTINCT ON (session.id)
           run.ref, run.version, run.created_at, session.organization_id,
           session.id AS session_id, session.ref AS session_ref,
           project.id AS project_id, project.ref AS project_ref,
           session.target_type, session.target_ref, session.provider_account_id,
           account.ref AS account_ref
    FROM control_plane.sessions session
    JOIN control_plane.projects project ON project.id = session.project_id
    JOIN control_plane.provider_accounts account ON account.id = session.provider_account_id
    JOIN control_plane.runs run ON run.session_id = session.id
      AND run.target_type = session.target_type AND run.target_ref = session.target_ref
    WHERE session.organization_id = @organization_id::uuid
      AND session.state = 'ACTIVE' AND run.state = 'SUCCEEDED'
      AND (@target_type = '' OR (session.target_type = @target_type AND session.target_ref = @target_ref))
      AND (@project_ref = '' OR project.ref = @project_ref)
      AND (@authority_project_id = '' OR project.id = NULLIF(@authority_project_id, '')::uuid)
      AND NOT EXISTS (SELECT 1 FROM control_plane.session_turns turn
                      WHERE turn.session_id = session.id AND turn.state IN ('QUEUED', 'RUNNING'))
      AND (@query = '' OR strpos(lower(run.title), lower(@query)) > 0
                      OR strpos(lower(run.task), lower(@query)) > 0)
      AND EXISTS (SELECT 1 FROM control_plane.catalog_access_targets target
          WHERE target.organization_id = run.organization_id AND target.kind = 'RUN' AND target.id = run.id
            AND control_plane.catalog_resource_visible(run.organization_id, @actor_id::uuid, 'run.view',
                target.kind, target.id, target.project_id, target.owner_id, target.related_ids, transaction_timestamp()))
    ORDER BY session.id, run.created_at DESC, run.ref DESC
), target_keys AS MATERIALIZED (
    SELECT DISTINCT organization_id, project_id, target_type, target_ref FROM latest
), targets AS MATERIALIZED (
    SELECT key.organization_id, key.project_id, key.target_type, key.target_ref,
           CASE WHEN key.target_type = 'WORKFLOW' THEN workflow_version.spec ELSE NULL END AS target_spec,
	       CASE WHEN key.target_type = 'AGENT' THEN ARRAY[key.target_ref]
	            ELSE ARRAY(SELECT DISTINCT ref FROM (
	                SELECT workflow_version.spec->>'CoordinatorAgentRef' AS ref
	                UNION ALL
	                SELECT step->>'AgentRef'
	                FROM jsonb_array_elements(COALESCE(workflow_version.spec->'Steps', '[]')) step
	            ) refs WHERE ref IS NOT NULL)
	       END AS agent_refs,
	       CASE WHEN key.target_type = 'AGENT' THEN agent.id
	            ELSE workflow.coordinator_agent_id END AS configuration_agent_id
    FROM target_keys key
	 LEFT JOIN control_plane.agents agent
      ON key.target_type = 'AGENT' AND agent.organization_id = key.organization_id
     AND agent.project_id = key.project_id AND agent.ref = key.target_ref
	  AND agent.enabled AND agent.state = 'READY'
	  AND EXISTS (SELECT 1 FROM control_plane.instruction_versions instruction
	              JOIN control_plane.agent_instruction_bindings binding ON binding.instruction_id = instruction.id
	              WHERE binding.agent_id = agent.id AND binding.organization_id = agent.organization_id
	                AND instruction.agent_id = agent.id AND instruction.state = 'PUBLISHED')
	 LEFT JOIN control_plane.workflows workflow
      ON key.target_type = 'WORKFLOW' AND workflow.organization_id = key.organization_id
     AND workflow.project_id = key.project_id AND workflow.ref = key.target_ref
	  AND workflow.state = 'PUBLISHED'
	 LEFT JOIN control_plane.workflow_versions workflow_version
	   ON workflow_version.workflow_id = workflow.id
	  AND workflow_version.organization_id = workflow.organization_id
	  AND workflow_version.version_number = workflow.published_version
	 LEFT JOIN control_plane.agents coordinator
	   ON coordinator.id = workflow.coordinator_agent_id
	  AND coordinator.organization_id = workflow.organization_id
	  AND coordinator.project_id = workflow.project_id
	  AND coordinator.enabled AND coordinator.state = 'READY'
    JOIN control_plane.catalog_access_targets access_target
      ON access_target.organization_id = key.organization_id
     AND access_target.kind = key.target_type AND access_target.ref = key.target_ref
     AND access_target.project_id = key.project_id
	 WHERE (agent.id IS NOT NULL OR (workflow.id IS NOT NULL AND workflow_version.id IS NOT NULL AND coordinator.id IS NOT NULL))
      AND control_plane.catalog_resource_visible(key.organization_id, @actor_id::uuid,
          CASE key.target_type WHEN 'AGENT' THEN 'agent.launch' ELSE 'workflow.launch' END,
          access_target.kind, access_target.id, access_target.project_id, access_target.owner_id,
          access_target.related_ids, transaction_timestamp())
), ready_targets AS MATERIALIZED (
    SELECT target.*
    FROM targets target
    WHERE control_plane.agent_runtime_contract_ready(target.organization_id, target.project_id, target.agent_refs,
        @role_runtime_contract_revision, @role_runtime_contract_sha256, @default_role_image_digest)
), prepared AS (
    SELECT latest.*, target.target_spec, target.agent_refs,
           config.provider, config.model, policy.account_candidates, overlay.content AS overlay,
           binding.catalog_revision, binding.catalog_digest, binding.models,
           account.definition_key AS binding_provider,
           binding_policy.id::text AS binding_policy_id, binding_policy.ref AS binding_policy_ref,
           binding_policy.version_number AS binding_policy_version, binding_policy.digest AS binding_policy_digest
    FROM latest
    JOIN ready_targets target
      ON target.organization_id = latest.organization_id AND target.project_id = latest.project_id
     AND target.target_type = latest.target_type AND target.target_ref = latest.target_ref
	 JOIN control_plane.agents configuration_agent
	   ON configuration_agent.organization_id = latest.organization_id
	  AND configuration_agent.project_id = latest.project_id
	  AND configuration_agent.id = target.configuration_agent_id
    JOIN control_plane.agent_runtime_config_versions config
      ON config.id = configuration_agent.current_runtime_config_id
    JOIN control_plane.provider_account_policy_versions policy ON policy.id = config.provider_account_policy_id
    JOIN control_plane.agent_config_overlay_versions overlay ON overlay.id = configuration_agent.current_config_overlay_id
    JOIN control_plane.session_model_catalog_bindings binding
      ON binding.session_id = latest.session_id AND binding.organization_id = latest.organization_id
     AND binding.provider_account_id = latest.provider_account_id
    JOIN control_plane.provider_accounts account
      ON account.id = binding.provider_account_id AND account.organization_id = binding.organization_id
    JOIN control_plane.provider_account_policy_versions binding_policy
      ON binding_policy.id = binding.provider_account_policy_id
     AND binding_policy.organization_id = binding.organization_id
)
SELECT ref, version, session_id::text, session_ref, project_id::text, project_ref,
       target_type, target_ref, account_ref, target_spec, agent_refs,
       provider, model, account_candidates, overlay,
       catalog_revision, catalog_digest, models, binding_provider,
       binding_policy_id, binding_policy_ref, binding_policy_version, binding_policy_digest
FROM prepared
WHERE ref > @after_ref
ORDER BY ref
LIMIT @limit;
