-- name: platform__commands_changeworkflow_select_authoritative_readback :one
SELECT w.ref,
       p.ref,
       w.name,
       w.purpose,
       a.ref,
       w.state,
       w.version,
       w.draft_spec,
       w.published_spec,
       w.published_version,
       w.created_at,
       w.updated_at
FROM control_plane.workflows w
JOIN control_plane.projects p ON p.id=w.project_id
JOIN control_plane.agents a ON a.id=w.coordinator_agent_id
WHERE w.organization_id=$1::uuid
  AND w.ref=$2
