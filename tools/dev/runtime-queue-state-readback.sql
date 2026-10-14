-- Read-only диагностика двух явно выбранных root; без task text, SQL text и credentials.
BEGIN READ ONLY;
SELECT jsonb_build_object('at', clock_timestamp(), 'nodes', COALESCE(jsonb_agg(item), '[]'::jsonb))
FROM (
  SELECT jsonb_build_object('run', run.ref, 'runState', run.state, 'root', root.ref,
    'session', session.ref, 'storageState', storage.state, 'node', node.ref,
    'nodeState', node.state, 'step', node.workflow_step_key, 'type', node.type,
    'account', account.ref, 'accountState', account.state, 'accountEnabled', account.enabled,
    'agent', agent.ref, 'overlayState', overlay.state, 'environmentState', environment_set.state,
    'environmentVersion', environment.version_number, 'image', image.ref,
    'imageAdmission', image.admission_state, 'imagePromotion', image.promotion_state,
    'imageContractRevision', image.role_runtime_contract_revision, 'imageContractSHA256', image.role_runtime_contract_sha256,
    'imageProjectMatches', image.project_id IS NOT DISTINCT FROM agent.project_id,
    'recipeProjectMatches', recipe.project_id IS NOT DISTINCT FROM agent.project_id,
    'environmentProjectMatches', environment_set.project_id IS NOT DISTINCT FROM agent.project_id,
    'runtimeConfigPresent', runtime_config.id IS NOT NULL, 'runtimeModel', runtime_config.model,
    'instructionCount', (SELECT count(*) FROM control_plane.agent_instruction_bindings instruction_binding
      JOIN control_plane.instruction_versions instruction ON instruction.id=instruction_binding.instruction_id
      WHERE instruction_binding.agent_id=agent.id AND instruction.state='PUBLISHED'),
    'accountActiveExecutions', control_plane.provider_account_active_executions(run.organization_id,account.id),
    'concurrency', root.concurrency_limit,
    'activeRootAgents', (SELECT count(*) FROM control_plane.run_nodes active
      WHERE active.root_run_id = root.id AND active.type='AGENT_EXECUTION' AND active.state='RUNNING'
        AND active.workflow_step_key NOT LIKE 'workflow.coordinator.%'),
    'blockedDependencies', (SELECT COALESCE(jsonb_agg(jsonb_build_object('ref', dependency.ref, 'state', dependency.state)), '[]'::jsonb)
      FROM control_plane.run_edges edge JOIN control_plane.run_nodes dependency ON dependency.id=edge.source_node_id
      WHERE edge.target_node_id=node.id AND edge.type='WAITING_FOR' AND dependency.state<>'SUCCEEDED'),
    'leases', (SELECT COALESCE(jsonb_agg(jsonb_build_object('ref', lease.ref, 'state', lease.state,
      'expired', lease.expires_at<=clock_timestamp(), 'generation', lease.generation)), '[]'::jsonb)
      FROM control_plane.runtime_leases lease WHERE lease.node_id=node.id)) AS item
  FROM control_plane.run_nodes node
  JOIN control_plane.runs run ON run.id=node.run_id
  JOIN control_plane.runs root ON root.id=run.root_run_id
  JOIN control_plane.sessions session ON session.id=run.session_id
  LEFT JOIN control_plane.session_storage storage ON storage.session_id=session.id
  LEFT JOIN control_plane.provider_accounts account ON account.id=session.provider_account_id
  LEFT JOIN control_plane.agents agent ON agent.id=node.agent_id
  LEFT JOIN control_plane.agent_config_overlay_versions overlay ON overlay.id=agent.current_config_overlay_id
  LEFT JOIN control_plane.agent_runtime_environment_bindings binding ON binding.agent_id=agent.id
  LEFT JOIN control_plane.runtime_environment_sets environment_set ON environment_set.id=binding.environment_set_id
  LEFT JOIN control_plane.runtime_environment_versions environment ON environment.id=
    CASE WHEN agent.project_id IS NULL AND agent.system_key='system-assistant' AND binding.environment_version_id IS NULL
      THEN environment_set.current_version_id ELSE binding.environment_version_id END
  LEFT JOIN control_plane.image_artifacts image ON image.id=environment.role_image_artifact_id
  LEFT JOIN control_plane.role_image_recipes recipe ON recipe.id=image.recipe_id
  LEFT JOIN control_plane.agent_runtime_config_versions runtime_config ON runtime_config.id=agent.current_runtime_config_id
  WHERE root.ref IN (:'task_first_run', :'task_second_run')
  ORDER BY node.created_at, node.id LIMIT 64
) nodes;
COMMIT;
