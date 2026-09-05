-- name: mvp_list_model_capabilities :many
SELECT definition.stable_key,
       definition.enabled,
       definition.capabilities,
       COALESCE(array_agg(account.ref ORDER BY account.ref)
           FILTER (WHERE account.ref IS NOT NULL
             AND account.enabled
             AND account.state = 'AUTHORIZED'
             AND account.current_credential_revision_id IS NOT NULL), '{}'::text[]),
       COALESCE(array_agg(account.ref ORDER BY account.ref)
           FILTER (WHERE account.ref IS NOT NULL), '{}'::text[]),
       COALESCE(array_agg(DISTINCT CASE
           WHEN account.ref IS NULL THEN NULL
           WHEN NOT account.enabled THEN 'PROVIDER_ACCOUNT_DISABLED'
           WHEN account.state = 'REAUTHORIZATION_REQUIRED' THEN 'PROVIDER_ACCOUNT_REAUTHORIZATION_REQUIRED'
           WHEN account.state = 'REVOKED' THEN 'PROVIDER_ACCOUNT_REVOKED'
           WHEN account.state = 'DISABLED' THEN 'PROVIDER_ACCOUNT_DISABLED'
           WHEN account.state <> 'AUTHORIZED' THEN 'PROVIDER_ACCOUNT_AUTHORIZATION_PENDING'
           WHEN account.current_credential_revision_id IS NULL THEN 'PROVIDER_ACCOUNT_CREDENTIAL_MISSING'
           ELSE NULL
       END) FILTER (WHERE account.ref IS NOT NULL
         AND (NOT account.enabled
           OR account.state <> 'AUTHORIZED'
           OR account.current_credential_revision_id IS NULL)), '{}'::text[]),
       jsonb_build_object('definition', definition.stable_key,
         'version', definition.version, 'capabilities', definition.capabilities,
         'accounts', COALESCE(jsonb_agg(jsonb_build_object(
           'ref', account.ref, 'version', account.version,
           'credential_revision', account.current_credential_revision_id)
           ORDER BY account.ref) FILTER (WHERE account.ref IS NOT NULL), '[]'::jsonb))::text
FROM control_plane.provider_definitions definition
LEFT JOIN control_plane.provider_accounts account
  ON account.definition_key = definition.stable_key
 AND account.organization_id = @organization_id::uuid
 AND (@provider_account_ref = '' OR account.ref = @provider_account_ref)
WHERE (@provider_definition_key = '' OR definition.stable_key = @provider_definition_key)
GROUP BY definition.stable_key, definition.enabled, definition.capabilities, definition.version
ORDER BY definition.stable_key
LIMIT 129;
