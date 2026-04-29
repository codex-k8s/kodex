-- name: user__get_by_email :one
SELECT id, primary_email, display_name, avatar_asset_ref, status, locale, version, created_at, updated_at
FROM access_users
WHERE primary_email = @primary_email;
