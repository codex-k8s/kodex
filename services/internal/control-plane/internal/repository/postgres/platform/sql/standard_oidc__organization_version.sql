-- name: standard_oidc__organization_version :one
SELECT version FROM control_plane.organizations WHERE id = @organization_id::uuid;
