-- name: AutomationSchedulerProjectNext
WITH cursor_row AS (
    INSERT INTO control_plane.automation_scheduler_cursors (
        organization_id, operation, updated_at
    ) VALUES (@organization_id::uuid, @operation, clock_timestamp())
    ON CONFLICT (organization_id, operation) DO UPDATE
      SET updated_at = control_plane.automation_scheduler_cursors.updated_at
    RETURNING last_project_id
), candidate AS (
    SELECT project.id
    FROM control_plane.resources AS project, cursor_row
    WHERE project.organization_id = @organization_id::uuid
      AND project.kind = 'PROJECT'
      AND project.project_id = project.id
      AND project.state = 'ACTIVE'
    ORDER BY (project.id <= cursor_row.last_project_id), project.id
    LIMIT 1
), updated AS (
    UPDATE control_plane.automation_scheduler_cursors AS cursor
    SET last_project_id = candidate.id, updated_at = clock_timestamp()
    FROM candidate
    WHERE cursor.organization_id = @organization_id::uuid
      AND cursor.operation = @operation
    RETURNING candidate.id
)
SELECT id::text FROM updated
