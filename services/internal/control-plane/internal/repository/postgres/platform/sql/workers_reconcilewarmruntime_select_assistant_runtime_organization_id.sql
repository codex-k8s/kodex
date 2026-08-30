-- name: workers_reconcilewarmruntime_select_assistant_runtime_organization_id :one
SELECT a.ref,
       ar.stable_key,
       a.name,
       a.purpose,
       ar.core_prompt_revision,
       ar.owner_instructions,
       ar.runtime_state,
       ar.runtime_revision,
       ar.desired_runtime_revision,
       ar.system_session_ref,
       ar.resource_limits,
       ar.last_heartbeat_at,
       ar.version,
       ar.updated_at,
       instruction.ref,
       instruction.digest,
       instruction.content,
       COALESCE(ar.warm_instance_ref, ''),
       a.runtime_key,
       profile.runtime_revision,
       profile.provider,
       profile.model,
       role_definition.ref,
       provider_account.ref,
       credential.ref,
       credential.revision_number,
       credential.secret_name,
       credential.secret_uid::text,
       credential.secret_resource_version,
       credential.content_sha256,
       runtime_config.ref,
       runtime_config.version_number,
       runtime_config.digest,
       provider_policy.ref,
       provider_policy.version_number,
       provider_policy.digest,
       config_overlay.ref,
       config_overlay.version_number,
       config_overlay.digest,
       config_overlay.content,
       environment_set.ref,
       runtime_environment.version_number,
       runtime_environment.digest,
       environment_binding.ref,
       environment_binding.version,
       environment_binding.digest,
       runtime_environment.non_secret_values,
       runtime_environment.secret_descriptors,
       runtime_environment.selected_tools,
       runtime_environment.core_digest,
       runtime_environment.resource_policy,
       runtime_environment.volume_policy,
       runtime_environment.network_policy,
       runtime_environment.kubernetes_access_profile,
       runtime_environment.resources_digest,
       runtime_environment.volumes_digest,
       runtime_environment.network_digest,
       runtime_environment.rbac_digest
FROM control_plane.assistant_runtime ar
JOIN control_plane.agents a ON a.id = ar.agent_id
JOIN control_plane.sessions session ON session.ref = ar.system_session_ref
JOIN control_plane.provider_accounts provider_account
  ON provider_account.id = session.provider_account_id
 AND provider_account.state = 'AUTHORIZED'
 AND provider_account.enabled
JOIN control_plane.provider_credential_revisions credential
  ON credential.id = provider_account.current_credential_revision_id
JOIN control_plane.role_definitions role_definition ON role_definition.id = a.role_definition_id
JOIN control_plane.instruction_versions instruction ON instruction.ref = ar.core_prompt_ref
JOIN control_plane.agent_runtime_config_versions runtime_config
  ON runtime_config.id = a.current_runtime_config_id
JOIN control_plane.provider_account_policy_versions provider_policy
  ON provider_policy.id = runtime_config.provider_account_policy_id
JOIN control_plane.agent_config_overlay_versions config_overlay
  ON config_overlay.id = a.current_config_overlay_id
 AND config_overlay.state = 'PUBLISHED'
JOIN control_plane.agent_runtime_environment_bindings environment_binding
  ON environment_binding.agent_id = a.id
JOIN control_plane.runtime_environment_sets environment_set
  ON environment_set.id = environment_binding.environment_set_id
 AND environment_set.state = 'ACTIVE'
JOIN control_plane.runtime_environment_versions runtime_environment
  ON runtime_environment.id = environment_set.current_version_id
JOIN control_plane.runtime_profiles profile
  ON profile.stable_key = runtime_config.runtime_profile_key
 AND profile.provider = runtime_config.provider
JOIN control_plane.provider_definitions runtime_provider_definition
  ON runtime_provider_definition.stable_key = runtime_config.provider
 AND runtime_provider_definition.stable_key = provider_account.definition_key
WHERE ar.organization_id = $1::uuid
FOR UPDATE
