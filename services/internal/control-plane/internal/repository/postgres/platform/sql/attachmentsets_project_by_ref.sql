-- name: attachmentsets_project_by_ref :one
SELECT COALESCE(project.ref, '')
FROM control_plane.attachment_sets attachment_set
LEFT JOIN control_plane.projects project ON project.id = attachment_set.project_id
WHERE attachment_set.organization_id = $1::uuid
  AND attachment_set.ref = $2;
