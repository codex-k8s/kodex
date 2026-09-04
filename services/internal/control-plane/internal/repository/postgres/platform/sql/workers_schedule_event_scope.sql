-- name: workers_schedule_event_scope :one
SELECT schedule.ref, project.ref, schedule.version
FROM control_plane.schedule_occurrences occurrence
JOIN control_plane.schedules schedule ON schedule.id = occurrence.schedule_id
JOIN control_plane.projects project ON project.id = schedule.project_id
WHERE occurrence.id = $1::uuid AND occurrence.organization_id = $2::uuid
