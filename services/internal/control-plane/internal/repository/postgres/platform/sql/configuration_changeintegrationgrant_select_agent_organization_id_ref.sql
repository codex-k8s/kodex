-- name: platform__configuration_changeintegrationgrant_select_agent_organization_id_ref :one
SELECT p.id::text,p.ref,a.name FROM control_plane.agents a JOIN control_plane.projects p ON p.id=a.project_id WHERE a.organization_id=$1::uuid AND a.ref=$2 AND a.enabled AND a.state='READY' AND p.lifecycle='ACTIVE'
