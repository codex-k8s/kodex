-- name: artifacts_uploadartifact_insert_artifacts_ref_project_id_file_name :one
INSERT INTO control_plane.artifacts(ref,organization_id,project_id,run_id,file_name,media_type,size_bytes,digest,source,scan_state,object_receipt_ref,preview_state,revision,created_by)
SELECT $1,$2::uuid,$3::uuid,$4::uuid,$5,$6,$7,$8,'CONTROL_CENTER',$9,$10,$11,COALESCE(MAX(previous.revision),0)+1,$12::uuid
FROM control_plane.artifacts previous
WHERE previous.project_id=$3::uuid AND previous.file_name=$5
RETURNING ref,file_name,media_type,size_bytes,digest,scan_state,preview_state,revision,version,created_at
