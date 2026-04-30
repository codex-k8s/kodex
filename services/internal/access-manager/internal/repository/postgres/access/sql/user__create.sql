-- name: user__create :exec
INSERT INTO access_users (
    id, primary_email, display_name, avatar_asset_ref, status, locale, version, created_at, updated_at
) VALUES (
    @id, @primary_email, @display_name, @avatar_asset_ref, @status, @locale, @version, @created_at, @updated_at
);
