-- name: runtime_completeexecution_insert_artifacts_ref_project_id_node_id :one
INSERT INTO control_plane.artifacts(ref,organization_id,project_id,run_id,node_id,file_name,media_type,size_bytes,digest,source,scan_state,object_receipt_ref,preview_state,revision,created_by)
SELECT $1,$2::uuid,NULLIF($3,'')::uuid,$4::uuid,$5::uuid,$6,$7,$8,$9,
       'AGENT_RESULT',$10,$11,$12,COALESCE(MAX(previous.revision),0)+1,run.initiated_by
FROM control_plane.runs run
LEFT JOIN control_plane.artifacts previous
  ON previous.organization_id=$2::uuid
 AND previous.project_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid
 AND previous.file_name=$6
 AND (NULLIF($3,'') IS NOT NULL OR previous.created_by=run.initiated_by)
WHERE run.id=$4::uuid
  AND run.organization_id=$2::uuid
GROUP BY run.initiated_by
RETURNING id::text
