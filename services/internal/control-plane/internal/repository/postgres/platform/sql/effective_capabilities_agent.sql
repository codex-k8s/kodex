-- name: effective_capabilities_agent :one
SELECT a.version, COALESCE(p.ref,''), a.enabled AND a.state='READY', a.capabilities
FROM control_plane.agents a
LEFT JOIN control_plane.projects p ON p.id=a.project_id
WHERE a.organization_id=$1::uuid AND a.ref=$2 AND a.state<>'ARCHIVED'
  AND (a.project_id IS NULL OR p.lifecycle='ACTIVE');
