-- name: queries_listartifacts_select_artifact_bindings_artifact_id_id_organization_id :many
SELECT ar.ref,COALESCE(p.ref,''),COALESCE(r.ref,''),COALESCE(s.ref,''),COALESCE(n.ref,''),ar.file_name,ar.media_type,ar.digest,ar.scan_state,ar.preview_state,
       ar.source,
       ar.size_bytes,ar.revision,ar.version,ar.lifecycle_state,ar.created_at,ar.deleted_at,ar.purge_after,
       COALESCE((SELECT array_agg(b.target_ref ORDER BY b.created_at) FROM control_plane.artifact_bindings b WHERE b.artifact_id=ar.id AND b.target_kind='KNOWLEDGE'),'{}'),
       ($4 IN ('OWNER','ADMINISTRATOR') OR (ar.project_id IS NULL AND ar.created_by=$5::uuid) OR EXISTS(
         SELECT 1 FROM control_plane.memberships manage_membership
         WHERE manage_membership.project_id=ar.project_id
           AND manage_membership.subject_id=$5::uuid
           AND manage_membership.active
           AND 'MANAGE_ARTIFACTS'=ANY(manage_membership.permissions)
       ))
FROM control_plane.artifacts ar
LEFT JOIN control_plane.projects p ON p.id=ar.project_id
LEFT JOIN control_plane.runs r ON r.id=ar.run_id
LEFT JOIN control_plane.sessions s ON s.id=r.session_id
LEFT JOIN control_plane.run_nodes n ON n.id=ar.node_id
WHERE ar.organization_id=$1::uuid
  AND ($2='' OR p.ref=$2)
  AND ($3='' OR r.ref=$3)
  AND ($4 IN ('OWNER','ADMINISTRATOR') OR (ar.project_id IS NULL AND ar.created_by=$5::uuid) OR EXISTS(
    SELECT 1 FROM control_plane.memberships m
    WHERE m.project_id=ar.project_id AND m.subject_id=$5::uuid AND m.active AND 'VIEW'=ANY(m.permissions)
  ))
  AND ($6='' OR ar.file_name ILIKE '%'||$6||'%')
  AND ar.lifecycle_state=$7
  AND ($8::timestamptz IS NULL OR ar.created_at < $8::timestamptz OR
       (ar.created_at = $8::timestamptz AND ar.ref < $9))
ORDER BY ar.created_at DESC,ar.ref DESC
LIMIT $10
