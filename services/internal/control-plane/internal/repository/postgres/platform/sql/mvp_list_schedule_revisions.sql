-- name: mvp_list_schedule_revisions :many
SELECT revision.ref, revision.revision, revision.digest, revision.name, revision.target_type,
       revision.target_ref, revision.preset, revision.cron_expression, revision.timezone,
       revision.input, revision.session_policy, revision.notification_policy, revision.created_at
FROM control_plane.schedule_revisions revision
JOIN control_plane.schedules schedule ON schedule.id = revision.schedule_id
WHERE revision.organization_id = @organization_id::uuid
  AND schedule.ref = @schedule_ref
  AND (@before_revision = 0 OR revision.revision < @before_revision)
ORDER BY revision.revision DESC
LIMIT @page_size;
