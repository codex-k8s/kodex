-- name: assistant_configuration_catalog__accounts :many
SELECT account.ref,account.name,account.definition_key,account.version
FROM control_plane.provider_accounts account
JOIN control_plane.provider_definitions definition ON definition.stable_key=account.definition_key AND definition.enabled
WHERE account.organization_id=@organization_id::uuid AND account.definition_key=@provider
  AND account.enabled AND account.state='AUTHORIZED' AND account.current_credential_revision_id IS NOT NULL
  AND control_plane.catalog_resource_visible(account.organization_id,@actor_id::uuid,'provider.account.view','PROVIDER_ACCOUNT',account.id,NULL,account.created_by,'{}'::jsonb,transaction_timestamp())
  AND (@query='' OR strpos(lower(account.name),lower(@query))>0 OR strpos(lower(account.ref),lower(@query))>0)
ORDER BY account.name,account.ref LIMIT 11 OFFSET @offset;
