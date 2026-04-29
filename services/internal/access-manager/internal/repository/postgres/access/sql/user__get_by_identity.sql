-- name: user__get_by_identity :one
SELECT u.id, u.primary_email, u.display_name, u.avatar_asset_ref, u.status, u.locale, u.version, u.created_at, u.updated_at
FROM access_users u
JOIN access_user_identities i ON i.user_id = u.id
WHERE i.provider = @provider AND i.subject = @subject;
