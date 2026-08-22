-- name: platform__commands_changeinstructions_insert_draft_version :exec
INSERT INTO control_plane.instruction_versions(ref,organization_id,agent_id,version_number,state,content,digest,created_by) VALUES($1,$2::uuid,$3::uuid,$4,'DRAFT',$5,$6,$7::uuid)
