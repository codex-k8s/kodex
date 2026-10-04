-- name: credential_projection_resolve_runtime :one
SELECT revision.provider_account_ref,
       revision.provider_credential_revision_ref,
       revision.provider_credential_revision_number,
       revision.provider_secret_name,
       revision.provider_secret_uid::text,
       revision.provider_secret_resource_version,
       revision.provider_credential_sha256,
       revision.safe_snapshot -> 'secretProjections',
       lease.expires_at
FROM control_plane.runtime_leases lease
JOIN control_plane.runtime_revisions revision ON revision.id = lease.runtime_revision_id
JOIN control_plane.runs root_run ON root_run.id = revision.root_run_id
JOIN control_plane.subjects actor ON actor.id = root_run.initiated_by AND actor.organization_id = revision.organization_id
JOIN control_plane.sessions session ON session.id = revision.session_id
LEFT JOIN control_plane.session_turns turn ON turn.id = revision.turn_id
JOIN control_plane.provider_accounts account ON account.id = revision.provider_account_id
JOIN control_plane.agents agent ON agent.id = revision.agent_id AND agent.organization_id = revision.organization_id
LEFT JOIN control_plane.assistant_conversations conversation
  ON conversation.session_id = session.id AND conversation.organization_id = revision.organization_id
  AND conversation.assistant_agent_id = agent.id
LEFT JOIN control_plane.project_assistant_profiles assistant_profile
  ON assistant_profile.id = conversation.assistant_profile_id
  AND assistant_profile.agent_id = agent.id AND assistant_profile.organization_id = revision.organization_id
  AND assistant_profile.project_id = agent.project_id
WHERE lease.organization_id = @organization_id::uuid
  AND actor.active
  AND revision.organization_id = @organization_id::uuid
  AND root_run.initiated_by = @actor_id::uuid
  AND ((NOT @system_assistant::boolean AND revision.project_id = @project_id::uuid
      AND agent.project_id = @project_id::uuid
      AND (revision.safe_snapshot ->> 'assistantScope' = 'NONE'
        OR (revision.safe_snapshot ->> 'assistantScope' = 'PROJECT'
          AND conversation.assistant_scope = 'PROJECT' AND conversation.state = 'ACTIVE'
          AND conversation.created_by = @actor_id::uuid AND assistant_profile.ref = revision.safe_snapshot ->> 'assistantProfileRef'
          AND conversation.project_id = @project_id::uuid
          AND session.target_ref = agent.ref AND root_run.target_ref = agent.ref)))
    OR (@system_assistant::boolean AND @project_id::uuid IS NULL
      AND revision.safe_snapshot ->> 'assistantScope' = 'SYSTEM'
      AND root_run.project_id IS NOT DISTINCT FROM revision.project_id
      AND session.project_id IS NOT DISTINCT FROM revision.project_id
      AND agent.system_key = 'system-assistant' AND agent.project_id IS NULL
      AND session.target_type = 'SYSTEM_ASSISTANT' AND session.target_ref = agent.ref
      AND root_run.target_type = 'SYSTEM_ASSISTANT' AND root_run.target_ref = agent.ref
      AND conversation.assistant_scope = 'SYSTEM' AND conversation.assistant_profile_id IS NULL
      AND conversation.state = 'ACTIVE' AND conversation.created_by = @actor_id::uuid
      AND conversation.project_id IS NOT DISTINCT FROM revision.project_id
      AND session.created_by = @actor_id::uuid AND session.state = 'ACTIVE'
      AND session.organization_id = revision.organization_id AND root_run.organization_id = revision.organization_id
      AND revision.run_id = root_run.id AND root_run.session_id = session.id
      AND turn.session_id = session.id AND turn.run_id = root_run.id AND turn.organization_id = revision.organization_id))
  AND lease.ref = @lease_ref
  AND lease.workload_instance = @workload_instance
  AND lease.generation = @generation
  AND lease.state = 'CLAIMED'
  AND lease.expires_at > clock_timestamp()
  AND (@fence = '' OR lease.fence_digest = encode(digest(convert_to(@fence, 'UTF8'), 'sha256'), 'hex'))
  AND revision.ref = @runtime_revision_ref
  AND revision.revision_digest = @runtime_revision_digest
  AND revision.generation = @generation
  AND revision.attempt = @attempt
  AND revision.input_digest = @input_digest
  AND session.ref = @session_ref
  AND COALESCE(turn.ref, '') = @turn_ref
  AND ((account.enabled AND account.state = 'AUTHORIZED') OR account.state = 'DELETING')
  AND account.current_credential_revision_id = revision.provider_credential_revision_id;
