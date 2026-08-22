-- name: platform__runtime_completeexecution_insert_artifacts_ref_project_id_node_id :one
INSERT INTO control_plane.artifacts(ref,organization_id,project_id,run_id,node_id,file_name,media_type,size_bytes,digest,source,scan_state,object_receipt_ref,preview_state,revision,created_by)
SELECT $1,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,$7,$8,$9,'AGENT_RESULT',$10,$11,$12,COALESCE(MAX(previous.revision),0)+1,$13::uuid
FROM control_plane.artifacts previous
WHERE previous.project_id=$3::uuid AND previous.file_name=$6
RETURNING id::text
