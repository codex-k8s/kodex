-- name: platform__queries_attachinstructions_select_instruction_versions_organization_id_agent_id_ref :many
SELECT ref,version_number,state,content,digest,core,COALESCE(parent_ref,''),validation_problems,created_at,published_at
		FROM control_plane.instruction_versions WHERE organization_id=$1::uuid AND agent_id=(SELECT id FROM control_plane.agents WHERE ref=$2)
		ORDER BY version_number DESC
