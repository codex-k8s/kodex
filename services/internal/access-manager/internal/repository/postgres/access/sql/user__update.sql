-- name: user__update :exec
UPDATE access_users
SET
    primary_email = @primary_email,
    display_name = @display_name,
    avatar_asset_ref = @avatar_asset_ref,
    status = @status,
    locale = @locale,
    version = @version,
    updated_at = @updated_at
WHERE id = @id AND version = @previous_version;
