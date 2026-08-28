-- name: commands_changeinstructions_update_current_draft :exec
UPDATE control_plane.instruction_versions
SET state = 'DRAFT',
    content = $2,
    digest = $3,
    validation_problems = '[]'::jsonb,
    created_by = $4::uuid
WHERE agent_id = $1::uuid
  AND state IN ('DRAFT', 'VALID', 'INVALID')
