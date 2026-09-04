-- name: access_sync_list_memberships :many
SELECT oidc_group.display_name,
       membership.subject_session_revision,
       oidc_group.last_seen_at >= clock_timestamp() - interval '24 hours'
FROM control_plane.oidc_group_memberships membership
JOIN control_plane.oidc_groups oidc_group ON oidc_group.id = membership.group_id
WHERE membership.organization_id = @organization_id::uuid
  AND membership.subject_id = @subject_id::uuid
ORDER BY oidc_group.display_name
