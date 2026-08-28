-- name: runtime_configuration__validate_accounts :one
SELECT profile.provider,
       profile.model,
       profile.runtime_revision,
       count(DISTINCT account.ref)::integer
FROM control_plane.runtime_profiles profile
LEFT JOIN control_plane.provider_definitions definition ON definition.stable_key = profile.provider
LEFT JOIN control_plane.provider_accounts account
  ON account.provider_definition_id = definition.id
 AND account.organization_id = $1::uuid
 AND account.ref = ANY($3::text[])
 AND account.state = 'AUTHORIZED'
 AND account.enabled
 AND account.current_credential_revision_id IS NOT NULL
WHERE profile.stable_key = $2 AND profile.enabled
GROUP BY profile.provider, profile.model, profile.runtime_revision;
