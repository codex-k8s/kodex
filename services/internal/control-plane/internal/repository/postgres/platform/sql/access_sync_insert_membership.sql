-- name: access_sync_insert_membership :exec
INSERT INTO control_plane.oidc_group_memberships
    (organization_id, group_id, subject_id, subject_session_revision, synchronized_at)
VALUES (@organization_id::uuid, @group_id::uuid, @subject_id::uuid, @session_revision, @observed_at)
