-- name: queries_listartifacts_select_artifact_bindings_artifact_id_id_organization_id :many
SELECT ar.ref,COALESCE(p.ref,''),COALESCE(r.ref,''),COALESCE(s.ref,''),COALESCE(n.ref,''),ar.file_name,ar.media_type,ar.digest,ar.scan_state,ar.preview_state,
       ar.source,
       ar.size_bytes,ar.revision,ar.version,ar.lifecycle_state,ar.created_at,ar.deleted_at,ar.purge_after,
       COALESCE((SELECT array_agg(b.target_ref ORDER BY b.created_at) FROM control_plane.artifact_bindings b WHERE b.artifact_id=ar.id AND b.target_kind='KNOWLEDGE'),'{}'),
       (@role IN ('OWNER','ADMINISTRATOR') OR (ar.project_id IS NULL AND ar.created_by=@actor_id::uuid) OR EXISTS(
         SELECT 1 FROM control_plane.memberships manage_membership
         WHERE manage_membership.project_id=ar.project_id
           AND manage_membership.subject_id=@actor_id::uuid
           AND manage_membership.active
           AND 'MANAGE_ARTIFACTS'=ANY(manage_membership.permissions)
       ))
FROM control_plane.artifacts ar
LEFT JOIN control_plane.projects p ON p.id=ar.project_id
LEFT JOIN control_plane.runs r ON r.id=ar.run_id
LEFT JOIN control_plane.sessions s ON s.id=r.session_id
LEFT JOIN control_plane.run_nodes n ON n.id=ar.node_id
WHERE ar.organization_id=@organization_id::uuid
  AND (@project_ref='' OR p.ref=@project_ref)
  AND (@run_ref='' OR r.ref=@run_ref)
  AND (@query='' OR strpos(lower(ar.file_name),lower(@query)) > 0)
  AND ar.lifecycle_state=@lifecycle_state
  AND (@scan_state='' OR ar.scan_state=@scan_state)
  AND (@source_kind='' OR ar.source=@source_kind)
  AND (cardinality(@source_kinds::text[])=0 OR ar.source=ANY(@source_kinds::text[]))
  AND (
    @artifact_type=''
    OR (@artifact_type='IMAGE' AND ar.media_type LIKE 'image/%')
    OR (@artifact_type='DOCUMENT' AND (
      ar.media_type='application/pdf'
      OR ar.media_type LIKE '%officedocument%'
      OR lower(ar.file_name) ~ '\.(doc|docx|odt|ppt|pptx|csv|ods|xls|xlsx)$'
    ))
    OR (@artifact_type='TEXT' AND NOT (
      ar.media_type LIKE 'image/%'
      OR ar.media_type='application/pdf'
      OR ar.media_type LIKE '%officedocument%'
      OR lower(ar.file_name) ~ '\.(doc|docx|odt|ppt|pptx|csv|ods|xls|xlsx)$'
    ))
  )
  AND (@cursor_ref = '' OR ar.ref > @cursor_ref)
  AND (@authority_project = '' OR ar.project_id = NULLIF(@authority_project,'')::uuid)
  AND EXISTS (SELECT 1 FROM control_plane.catalog_access_targets target
      WHERE target.organization_id=ar.organization_id AND target.kind='ARTIFACT' AND target.id=ar.id
        AND control_plane.catalog_resource_visible(ar.organization_id, @actor_id::uuid, 'artifact.view', target.kind,
            target.id, target.project_id, target.owner_id, target.related_ids, transaction_timestamp()))
ORDER BY ar.ref
LIMIT @limit
