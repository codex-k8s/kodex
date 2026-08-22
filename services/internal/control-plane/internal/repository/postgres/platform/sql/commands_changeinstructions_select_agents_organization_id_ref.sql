-- name: commands_changeinstructions_select_agents_organization_id_ref :one
SELECT a.id::text,COALESCE(a.project_id::text,''),COALESCE(p.ref,''),COALESCE(a.system_key,''),a.version FROM control_plane.agents a LEFT JOIN control_plane.projects p ON p.id=a.project_id WHERE a.organization_id=$1::uuid AND a.ref=$2 FOR UPDATE
