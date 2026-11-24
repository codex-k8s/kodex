-- name: provider_accounts__read_personal_owner :one
SELECT COALESCE(personal_owner_subject_id::text, '')
FROM control_plane.provider_accounts
WHERE organization_id=@organization_id::uuid AND ref=@account_ref;
