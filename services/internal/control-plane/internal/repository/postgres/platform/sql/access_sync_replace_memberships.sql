-- name: access_sync_replace_memberships :exec
DELETE FROM control_plane.oidc_group_memberships
WHERE organization_id = @organization_id::uuid AND subject_id = @subject_id::uuid
