-- name: platform__commands_changeworkflow_update_workflows_draft_spec_state_version :exec
UPDATE control_plane.workflows
SET name=$2,
    purpose=$3,
    coordinator_agent_id=(SELECT a.id FROM control_plane.agents a WHERE a.organization_id=workflows.organization_id AND a.project_id=workflows.project_id AND a.ref=$4 AND a.enabled AND a.state='READY'),
    draft_spec=$5,
    state='DRAFT',
    version=version+1,
    updated_at=clock_timestamp()
WHERE id=$1::uuid
  AND EXISTS (
      SELECT 1
      FROM control_plane.agents candidate
      WHERE candidate.organization_id=workflows.organization_id
        AND candidate.project_id=workflows.project_id
        AND candidate.ref=$4
        AND candidate.enabled
        AND candidate.state='READY'
  )
