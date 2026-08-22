-- name: platform__commands_changeworkflow_insert_workflow_versions_ref_workflow_id_spec :exec
INSERT INTO control_plane.workflow_versions(ref,organization_id,workflow_id,version_number,spec,digest,created_by) VALUES($1,$2::uuid,$3::uuid,$4,$5,$6,$7::uuid)
