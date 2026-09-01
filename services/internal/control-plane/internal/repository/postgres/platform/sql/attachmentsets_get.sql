-- name: attachmentsets_get :one
SELECT attachment_set.id::text, attachment_set.ref, attachment_set.family_ref,
       COALESCE(attachment_set.project_id::text, ''), COALESCE(project.ref, ''), attachment_set.revision,
       attachment_set.version, attachment_set.state, attachment_set.purpose,
       attachment_set.source, COALESCE(attachment_set.manifest_digest, ''),
       attachment_set.item_count, attachment_set.total_size_bytes,
       attachment_set.created_at, attachment_set.finalized_at,
       EXISTS (
           SELECT 1 FROM control_plane.attachment_sets later
           WHERE later.family_ref = attachment_set.family_ref
             AND later.revision > attachment_set.revision
       )
FROM control_plane.attachment_sets attachment_set
LEFT JOIN control_plane.projects project ON project.id = attachment_set.project_id
WHERE attachment_set.organization_id = $1::uuid
  AND attachment_set.ref = $2;
