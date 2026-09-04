-- name: managed_configuration_get_stt :one
SELECT configuration.ref, revision.ref, revision.revision, revision.digest,
       revision.content::jsonb -> 'stt' ->> 'providerAccountRef',
       revision.content::jsonb -> 'stt' ->> 'model',
       revision.content::jsonb -> 'stt' ->> 'language',
       revision.content::jsonb -> 'stt' ->> 'permissionKey',
       account.enabled AND account.state = 'AUTHORIZED' AND account.current_credential_revision_id IS NOT NULL,
       COALESCE(definition.enabled AND definition.stable_key = 'openai-codex', false), COALESCE(definition.capabilities, '{}'::jsonb),
       COALESCE(credential.revision_number, 0),
       COALESCE((
           SELECT attempt.method = 'API_KEY'
           FROM control_plane.provider_authorization_attempts attempt
           WHERE attempt.organization_id = configuration.organization_id
             AND attempt.provider_account_id = account.id
             AND attempt.state = 'AUTHORIZED'
           ORDER BY attempt.updated_at DESC, attempt.ref DESC
           LIMIT 1
       ), false)
FROM control_plane.managed_configuration_bindings binding
JOIN control_plane.managed_configuration_sets configuration
  ON configuration.id = binding.configuration_set_id
 AND configuration.organization_id = binding.organization_id
 AND configuration.kind = binding.configuration_kind
JOIN control_plane.managed_configuration_revisions revision
  ON revision.id = binding.configuration_revision_id
 AND revision.configuration_set_id = configuration.id
LEFT JOIN control_plane.provider_accounts account
  ON account.organization_id = configuration.organization_id
 AND account.ref = revision.content::jsonb -> 'stt' ->> 'providerAccountRef'
LEFT JOIN control_plane.provider_definitions definition ON definition.stable_key = account.definition_key
LEFT JOIN control_plane.provider_credential_revisions credential ON credential.id = account.current_credential_revision_id
WHERE configuration.organization_id = @organization_id::uuid
  AND binding.configuration_kind = 'SYSTEM_STT'
  AND binding.consumer_kind = 'STT_SERVICE'
  AND binding.consumer_ref = 'stt-tts-service'
  AND revision.state IN ('PUBLISHED', 'SUPERSEDED')
LIMIT 1;
