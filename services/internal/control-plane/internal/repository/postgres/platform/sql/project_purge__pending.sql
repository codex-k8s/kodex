-- name: project_purge__pending :many
SELECT receipt.organization_id::text, organization.ref,
       receipt.project_id::text, receipt.project_ref
FROM control_plane.project_purge_receipts receipt
JOIN control_plane.organizations organization ON organization.id=receipt.organization_id
JOIN control_plane.projects project ON project.id=receipt.project_id
  AND project.organization_id=receipt.organization_id AND project.ref=receipt.project_ref
WHERE receipt.state='PENDING' AND project.lifecycle='PURGE_PENDING'
ORDER BY receipt.requested_at,receipt.project_id
LIMIT @limit
