-- name: catalog_artifacts_count :one
SELECT count(*)
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
  AND (@authority_project = '' OR ar.project_id = NULLIF(@authority_project,'')::uuid)
  AND EXISTS (SELECT 1 FROM control_plane.catalog_access_targets target
      WHERE target.organization_id=ar.organization_id AND target.kind='ARTIFACT' AND target.id=ar.id
        AND control_plane.catalog_resource_visible(ar.organization_id, @actor_id::uuid, 'artifact.view', target.kind,
            target.id, target.project_id, target.owner_id, target.related_ids, transaction_timestamp()))
