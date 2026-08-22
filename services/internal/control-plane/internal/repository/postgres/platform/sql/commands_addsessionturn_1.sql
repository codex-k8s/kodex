-- name: platform__commands_addsessionturn_1 :one
SELECT s.project_id::text,p.ref,s.target_type,s.target_ref FROM control_plane.sessions s JOIN control_plane.projects p ON p.id=s.project_id WHERE s.organization_id=$1::uuid AND s.ref=$2 AND s.state='ACTIVE' FOR UPDATE
