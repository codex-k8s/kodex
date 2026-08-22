-- name: platform__queries_listartifacts_select_artifact_bindings_artifact_id_id_organization_id :many
SELECT ar.ref,p.ref,COALESCE(r.ref,''),COALESCE(n.ref,''),ar.file_name,ar.media_type,ar.digest,ar.scan_state,ar.preview_state,ar.size_bytes,ar.version,ar.created_at,COALESCE((SELECT array_agg(b.target_kind||':'||b.target_ref ORDER BY b.created_at) FROM control_plane.artifact_bindings b WHERE b.artifact_id=ar.id),'{}')
FROM control_plane.artifacts ar
JOIN control_plane.projects p ON p.id=ar.project_id
LEFT JOIN control_plane.runs r ON r.id=ar.run_id
LEFT JOIN control_plane.run_nodes n ON n.id=ar.node_id
WHERE ar.organization_id=$1::uuid
  AND ($2='' OR p.ref=$2)
  AND ($3='' OR r.ref=$3)
  AND ($4 IN ('OWNER','ADMINISTRATOR') OR EXISTS(
    SELECT 1 FROM control_plane.memberships m
    WHERE m.project_id=ar.project_id AND m.subject_id=$5::uuid AND m.active AND 'VIEW'=ANY(m.permissions)
  ))
  AND ($6='' OR ar.file_name ILIKE '%'||$6||'%')
ORDER BY ar.created_at DESC
LIMIT $7
