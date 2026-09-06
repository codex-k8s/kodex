-- name: run_attachment_account_dependencies :one
SELECT version, enabled, state, COALESCE(current_credential_revision_id::text, '')
FROM control_plane.provider_accounts
WHERE organization_id=@organization_id::uuid AND ref=@account_ref
