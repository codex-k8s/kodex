-- name: runtime_configuration__select_provider_account :one
SELECT selected.account_id::text,selected.account_ref,selected.config_ref,selected.config_version,
 selected.config_digest,selected.policy_ref,selected.policy_version,selected.policy_digest
FROM control_plane.provider_account_selection($1::uuid,$2) selected
JOIN control_plane.provider_accounts account ON account.id=selected.account_id
 AND account.organization_id=$1::uuid
 AND account.enabled AND account.state='AUTHORIZED'
 AND account.current_credential_revision_id IS NOT NULL
FOR UPDATE OF account;
