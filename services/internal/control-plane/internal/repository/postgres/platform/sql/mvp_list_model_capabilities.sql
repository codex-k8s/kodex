-- name: mvp_list_model_capabilities :many
SELECT definition.stable_key, account.ref,
       COALESCE(success.models, '[]'::jsonb),
       CASE
         WHEN NOT definition.enabled THEN 'PROVIDER_DISABLED'
         WHEN NOT account.enabled OR account.state = 'DISABLED' THEN 'PROVIDER_ACCOUNT_DISABLED'
         WHEN account.state = 'REAUTHORIZATION_REQUIRED' THEN 'PROVIDER_ACCOUNT_REAUTHORIZATION_REQUIRED'
         WHEN account.state = 'REVOKED' THEN 'PROVIDER_ACCOUNT_REVOKED'
         WHEN account.state <> 'AUTHORIZED' THEN 'PROVIDER_ACCOUNT_AUTHORIZATION_PENDING'
         WHEN current_credential.id IS NULL THEN 'PROVIDER_ACCOUNT_CREDENTIAL_MISSING'
         WHEN latest.id IS NULL OR latest.account_version <> account.version
           OR latest.provider_credential_revision_id IS DISTINCT FROM account.current_credential_revision_id THEN 'MODEL_CATALOG_PENDING'
         WHEN latest.failure <> 'NONE' THEN 'MODEL_CATALOG_' || latest.failure
         WHEN latest.expires_at <= clock_timestamp() THEN 'MODEL_CATALOG_EXPIRED'
         ELSE '' END,
       CASE WHEN latest.id IS NULL OR latest.account_version <> account.version
           OR latest.provider_credential_revision_id IS DISTINCT FROM account.current_credential_revision_id THEN 'PENDING'
         WHEN latest.failure <> 'NONE' THEN 'FAILED'
         WHEN latest.expires_at <= clock_timestamp() THEN 'EXPIRED'
         ELSE 'READY' END,
       latest.observed_at, latest.expires_at, COALESCE(latest.source, ''), COALESCE(latest.failure, ''),
       jsonb_build_object('definition', definition.stable_key,
         'organization', account.organization_id, 'account', account.ref,
         'content', COALESCE(success.content_digest, ''))::text
FROM control_plane.provider_definitions definition
JOIN control_plane.provider_accounts account ON account.definition_key = definition.stable_key
  AND account.organization_id = @organization_id::uuid
LEFT JOIN control_plane.provider_credential_revisions current_credential ON current_credential.id = account.current_credential_revision_id
  AND current_credential.provider_account_id = account.id AND current_credential.organization_id = account.organization_id
LEFT JOIN LATERAL (
  SELECT observation.* FROM control_plane.provider_model_catalog_observations observation
  WHERE observation.provider_account_id = account.id AND observation.organization_id = account.organization_id
  ORDER BY observation.created_at DESC, observation.id DESC LIMIT 1
) latest ON true
LEFT JOIN LATERAL (
  SELECT observation.models, observation.content_digest FROM control_plane.provider_model_catalog_observations observation
  WHERE observation.provider_account_id = account.id AND observation.organization_id = account.organization_id AND observation.failure = 'NONE'
  ORDER BY observation.created_at DESC, observation.id DESC LIMIT 1
) success ON true
WHERE (@provider_definition_key = '' OR definition.stable_key = @provider_definition_key)
  AND (@provider_account_ref = '' OR account.ref = @provider_account_ref)
ORDER BY definition.stable_key, account.ref
LIMIT 4097;
