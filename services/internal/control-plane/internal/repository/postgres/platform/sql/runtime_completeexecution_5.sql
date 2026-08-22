-- name: platform__runtime_completeexecution_5 :one
INSERT INTO control_plane.artifacts(ref,organization_id,project_id,run_id,node_id,file_name,media_type,size_bytes,digest,scan_state,object_receipt_ref,preview_state,created_by) VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,$7,$8,$9,'CLEAN',$10,'AVAILABLE',$11::uuid) RETURNING id::text
