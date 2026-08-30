-- name: attachmentsets_resolve_finalized :one
SELECT attachment_set.id::text, attachment_set.ref, attachment_set.manifest_digest,
       attachment_set.purpose, attachment_set.item_count, attachment_set.total_size_bytes
FROM control_plane.attachment_sets attachment_set
WHERE attachment_set.organization_id = @organization_id::uuid
  AND attachment_set.project_id IS NOT DISTINCT FROM NULLIF(@project_id, '')::uuid
  AND attachment_set.ref = @attachment_set_ref
  AND attachment_set.state = 'FINALIZED'
  AND attachment_set.purpose = @purpose
  AND NOT EXISTS (
      SELECT 1 FROM control_plane.attachment_sets later
      WHERE later.family_ref = attachment_set.family_ref
        AND later.revision > attachment_set.revision
  );
