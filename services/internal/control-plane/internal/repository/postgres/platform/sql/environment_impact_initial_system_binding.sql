-- name: environment_impact_initial_system_binding :one
SELECT EXISTS (
    SELECT 1
    FROM control_plane.agent_runtime_environment_bindings binding
    JOIN control_plane.agents agent ON agent.id=binding.agent_id AND agent.organization_id=binding.organization_id
    JOIN control_plane.runtime_environment_sets environment ON environment.id=binding.environment_set_id AND environment.organization_id=binding.organization_id
    JOIN control_plane.runtime_environment_versions target ON target.id=environment.current_version_id
      AND target.environment_set_id=environment.id AND target.organization_id=binding.organization_id
    JOIN control_plane.runtime_environment_versions source ON source.id=target.parent_version_id
      AND source.environment_set_id=environment.id AND source.organization_id=binding.organization_id
    WHERE binding.organization_id=@organization_id::uuid AND binding.ref=@binding_ref AND binding.version=@binding_version
      AND binding.environment_version_id IS NULL AND agent.ref=@agent_ref AND agent.version=@agent_version
      AND agent.project_id IS NULL AND agent.system_key='system-assistant' AND agent.state<>'ARCHIVED'
      AND environment.ref=@environment_ref AND environment.scope_kind='ORGANIZATION'
      AND environment.project_id IS NULL AND environment.state='ACTIVE'
      AND source.ref=@source_revision_ref AND target.ref=@target_revision_ref
);
