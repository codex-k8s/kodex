-- name: platform__commands_insertagentnode_select_agents_organization_id_ref_state :one
SELECT id::text,role_description FROM control_plane.agents WHERE organization_id=$1::uuid AND ref=$2 AND enabled AND state='READY'
