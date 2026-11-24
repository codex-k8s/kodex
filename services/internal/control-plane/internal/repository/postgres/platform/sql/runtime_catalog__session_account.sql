-- name: runtime_catalog__session_account :one
SELECT account.ref, account.personal_owner_subject_id IS NOT NULL
FROM control_plane.sessions session
JOIN control_plane.provider_accounts account ON account.id = session.provider_account_id AND account.organization_id = session.organization_id
WHERE session.id = $1::uuid AND session.organization_id = $2::uuid
FOR SHARE OF session, account;
