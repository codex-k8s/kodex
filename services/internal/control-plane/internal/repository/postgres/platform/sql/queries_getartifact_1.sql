-- name: platform__queries_getartifact_1 :one
SELECT ar.ref,p.ref,COALESCE(r.ref,''),COALESCE(n.ref,''),ar.file_name,ar.media_type,ar.digest,ar.scan_state,ar.preview_state,ar.size_bytes,ar.version,ar.created_at,COALESCE((SELECT array_agg(b.target_kind||':'||b.target_ref ORDER BY b.created_at) FROM control_plane.artifact_bindings b WHERE b.artifact_id=ar.id),'{}')
FROM control_plane.artifacts ar
JOIN control_plane.projects p ON p.id=ar.project_id
LEFT JOIN control_plane.runs r ON r.id=ar.run_id
LEFT JOIN control_plane.run_nodes n ON n.id=ar.node_id
WHERE ar.organization_id=$1::uuid
  AND ar.ref=$2
  AND ($3 IN ('OWNER','ADMINISTRATOR') OR EXISTS(
    SELECT 1 FROM control_plane.memberships m
    WHERE m.project_id=ar.project_id AND m.subject_id=$4::uuid AND m.active AND 'VIEW'=ANY(m.permissions)
  ))
