-- name: configuration_assistant_schedule_snapshot :one
SELECT schedule.version,
       schedule.lifecycle_state,
       revision.name,
       revision.target_type,
       revision.target_ref,
       revision.preset,
       revision.cron_expression,
       revision.timezone,
       revision.input,
       revision.automation_text,
       revision.session_policy,
       revision.notification_policy,
       revision.dst_gap_policy,
       revision.dst_fold_policy,
       revision.misfire_policy,
       revision.overlap_policy,
       revision.prompt_inputs
FROM control_plane.schedules schedule
JOIN control_plane.projects project ON project.id=schedule.project_id
  AND project.organization_id=schedule.organization_id AND project.lifecycle='ACTIVE'
JOIN control_plane.schedule_revisions revision ON revision.id=schedule.current_revision_id
  AND revision.schedule_id=schedule.id AND revision.organization_id=schedule.organization_id
WHERE schedule.organization_id=@organization_id::uuid
  AND schedule.ref=@schedule_ref
  AND project.ref=@project_ref
  AND schedule.lifecycle_state<>'DELETED';
