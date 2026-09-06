-- name: provider_usage__accounts :many
SELECT account.ref,account.version,account.definition_key,account.state,account.enabled,
 definition.enabled,COALESCE(credential.id::text,''),
 credential.observed_at,account.max_concurrent_executions,
 control_plane.provider_account_active_executions(account.organization_id,account.id),
 COALESCE(latest.id::text,''),latest.observed_at,latest.expires_at,
 CASE WHEN latest.id IS NULL OR latest.account_version<>account.version
  OR latest.provider_credential_revision_id IS DISTINCT FROM account.current_credential_revision_id THEN 'PENDING'
  WHEN latest.failure<>'NONE' THEN 'FAILED'
  WHEN latest.expires_at<=transaction_timestamp() THEN 'EXPIRED' ELSE 'READY' END,
 COALESCE(latest.source,''),COALESCE(latest.failure,''),COALESCE(success.content_digest,''),
 CASE WHEN @include_models::boolean THEN COALESCE(success.models,'[]'::jsonb) ELSE '[]'::jsonb END,
 transaction_timestamp(),jsonb_build_object('definition',definition.stable_key,
 'organization',account.organization_id,'account',account.ref,'content',COALESCE(success.content_digest,''))::text
FROM control_plane.provider_accounts account
JOIN control_plane.provider_definitions definition ON definition.stable_key=account.definition_key
LEFT JOIN control_plane.provider_credential_revisions credential ON credential.id=account.current_credential_revision_id
 AND credential.organization_id=account.organization_id AND credential.provider_account_id=account.id
LEFT JOIN LATERAL (SELECT observation.id,observation.account_version,observation.provider_credential_revision_id,
 observation.observed_at,observation.expires_at,observation.source,observation.failure
 FROM control_plane.provider_model_catalog_observations observation
 WHERE observation.provider_account_id=account.id AND observation.organization_id=account.organization_id
 ORDER BY observation.created_at DESC,observation.id DESC LIMIT 1) latest ON true
LEFT JOIN LATERAL (SELECT observation.content_digest,observation.models
 FROM control_plane.provider_model_catalog_observations observation
 WHERE observation.provider_account_id=account.id AND observation.organization_id=account.organization_id AND observation.failure='NONE'
 ORDER BY observation.created_at DESC,observation.id DESC LIMIT 1) success ON true
WHERE account.organization_id=@organization_id::uuid
 AND (cardinality(@account_refs::text[])=0 OR account.ref=ANY(@account_refs::text[]))
ORDER BY account.ref LIMIT 4097;
