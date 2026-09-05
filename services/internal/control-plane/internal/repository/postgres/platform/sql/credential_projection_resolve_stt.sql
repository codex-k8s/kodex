-- name: credential_projection_resolve_stt :one
SELECT account.ref,
       credential.ref,
       credential.revision_number,
       credential.secret_name,
       credential.secret_uid::text,
       credential.secret_resource_version,
       credential.content_sha256,
       revision.content::jsonb -> 'stt' ->> 'model',
       COALESCE(revision.content::jsonb -> 'stt' ->> 'language',''),
       definition.capabilities
FROM control_plane.managed_configuration_bindings binding
JOIN control_plane.managed_configuration_sets configuration
  ON configuration.id = binding.configuration_set_id
 AND configuration.organization_id = binding.organization_id
 AND configuration.kind = 'SYSTEM_STT'
JOIN control_plane.managed_configuration_revisions revision
  ON revision.id = binding.configuration_revision_id
 AND revision.configuration_set_id = configuration.id
JOIN control_plane.provider_accounts account
  ON account.organization_id = configuration.organization_id
 AND account.ref = revision.content::jsonb -> 'stt' ->> 'providerAccountRef'
JOIN control_plane.provider_definitions definition
  ON definition.stable_key = account.definition_key
 AND definition.stable_key = 'openai-codex'
 AND definition.enabled
JOIN control_plane.provider_credential_revisions credential
  ON credential.id = account.current_credential_revision_id
WHERE configuration.organization_id = @organization_id::uuid
  AND binding.configuration_kind = 'SYSTEM_STT'
  AND binding.consumer_kind = 'STT_SERVICE'
  AND binding.consumer_ref = 'stt-tts-service'
  AND revision.state IN ('PUBLISHED', 'SUPERSEDED')
  AND COALESCE((revision.content::jsonb -> 'stt' ->> 'enabled')::boolean, false)
  AND revision.revision = @config_revision
  AND revision.digest = @config_digest
  AND account.ref = @account_ref
  AND account.enabled
  AND account.state = 'AUTHORIZED'
  AND credential.revision_number = @credential_generation
  AND revision.content::jsonb -> 'stt' ->> 'permissionKey' = 'platform.stt.use'
  AND COALESCE((
      SELECT attempt.method = 'API_KEY'
      FROM control_plane.provider_authorization_attempts attempt
      WHERE attempt.organization_id = configuration.organization_id
        AND attempt.provider_account_id = account.id
        AND attempt.state = 'AUTHORIZED'
      ORDER BY attempt.updated_at DESC, attempt.ref DESC
      LIMIT 1
  ), false);
