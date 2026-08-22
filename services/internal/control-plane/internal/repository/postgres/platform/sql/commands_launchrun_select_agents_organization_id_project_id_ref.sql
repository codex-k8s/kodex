-- name: platform__commands_launchrun_select_agents_organization_id_project_id_ref :one
SELECT name FROM control_plane.agents a WHERE a.organization_id=$1::uuid AND a.project_id=$2::uuid AND a.ref=$3 AND a.enabled AND a.state='READY' AND EXISTS(SELECT 1 FROM control_plane.instruction_versions i WHERE i.agent_id=a.id AND i.state='PUBLISHED')
