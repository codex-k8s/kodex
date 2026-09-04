-- name: managed_configuration_get_stt :one
SELECT configuration.ref, revision.ref, revision.revision, revision.digest,
       revision.content::jsonb -> 'stt' ->> 'providerAccountRef',
       revision.content::jsonb -> 'stt' ->> 'model',
       revision.content::jsonb -> 'stt' ->> 'language',
       revision.content::jsonb -> 'stt' ->> 'permissionKey',
       account.enabled AND account.state = 'AUTHORIZED' AND account.current_credential_revision_id IS NOT NULL,
       COALESCE(definition.enabled, false), COALESCE(definition.capabilities, '{}'::jsonb)
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
WHERE configuration.organization_id = @organization_id::uuid
  AND binding.configuration_kind = 'SYSTEM_STT'
  AND binding.consumer_kind = 'STT_SERVICE'
  AND binding.consumer_ref = 'stt-tts-service'
  AND revision.state IN ('PUBLISHED', 'SUPERSEDED')
LIMIT 1;
