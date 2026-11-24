-- name: provider_accounts_create :one
INSERT INTO control_plane.provider_accounts
    (ref, organization_id, definition_key, stable_key, name, state, enabled, created_by, personal_owner_subject_id)
SELECT @account_ref, @organization_id::uuid, definition.stable_key,
       'account-' || substr(md5(@account_ref), 1, 24), @name,
       'PENDING_AUTHORIZATION', false, @created_by::uuid,
       CASE WHEN @personal::bool THEN @created_by::uuid END
FROM control_plane.provider_definitions definition
WHERE definition.stable_key = @definition_key
  AND definition.enabled
RETURNING ref;
