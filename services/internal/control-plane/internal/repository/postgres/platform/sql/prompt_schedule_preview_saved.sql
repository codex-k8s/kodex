-- name: prompt_schedule_preview_saved :one
SELECT project.ref,schedule.version,revision.ref,revision.digest,
       schedule.target_type,schedule.target_ref,COALESCE(session.ref,''),
       author.id::text,author.ref,author.display_name
FROM control_plane.schedules schedule
JOIN control_plane.projects project ON project.id=schedule.project_id AND project.organization_id=schedule.organization_id
JOIN control_plane.schedule_revisions revision ON revision.id=schedule.current_revision_id
  AND revision.schedule_id=schedule.id AND revision.organization_id=schedule.organization_id
JOIN control_plane.subjects author ON author.id=revision.created_by
LEFT JOIN control_plane.sessions session ON session.id=schedule.continue_session_id
  AND session.organization_id=schedule.organization_id AND session.project_id=schedule.project_id
WHERE schedule.organization_id=@organization_id::uuid AND schedule.ref=@schedule_ref
  AND schedule.project_id=@project_id::uuid AND schedule.lifecycle_state<>'DELETED'
  AND (schedule.continue_session_id IS NULL OR session.id IS NOT NULL);
