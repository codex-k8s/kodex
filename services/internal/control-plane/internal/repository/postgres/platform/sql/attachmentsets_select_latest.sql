-- name: attachmentsets_select_latest :one
SELECT current.id::text, current.ref, current.family_ref, COALESCE(current.project_id::text, ''),
       COALESCE(project.ref, ''), current.state, current.purpose, current.source,
       COALESCE(current.manifest_digest, ''), current.revision, current.version,
       current.item_count, current.total_size_bytes
FROM control_plane.attachment_sets requested
JOIN control_plane.attachment_sets current
  ON current.organization_id = requested.organization_id
 AND current.family_ref = requested.family_ref
LEFT JOIN control_plane.projects project ON project.id = current.project_id
WHERE requested.organization_id = $1::uuid
  AND requested.ref = $2
  AND current.revision = (
      SELECT max(candidate.revision)
      FROM control_plane.attachment_sets candidate
      WHERE candidate.organization_id = requested.organization_id
        AND candidate.family_ref = requested.family_ref
  )
  AND current.ref = requested.ref;
