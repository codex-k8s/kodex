-- name: artifacts_purge_select_artifact_content_for_update :one
SELECT artifact.id::text,
       COALESCE(artifact.project_id::text, ''),
       COALESCE(project.ref, ''),
       artifact.version,
       artifact.lifecycle_state,
       COALESCE((SELECT jsonb_agg(jsonb_build_object('revisionId',revision.id::text,
           'objectKey',content.object_key,'objectVersion',content.object_version)
           ORDER BY revision.revision,revision.id)
           FROM control_plane.artifact_revisions revision
           JOIN control_plane.artifact_revision_content content ON content.revision_id=revision.id
           WHERE revision.artifact_id=artifact.id),'[]'::jsonb)
FROM control_plane.artifact_heads artifact
LEFT JOIN control_plane.projects project ON project.id = artifact.project_id
WHERE artifact.organization_id = @organization_id::uuid
  AND artifact.ref = @artifact_ref
FOR UPDATE OF artifact;
