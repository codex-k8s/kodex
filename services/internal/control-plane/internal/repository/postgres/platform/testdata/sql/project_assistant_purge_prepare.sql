UPDATE control_plane.project_purge_receipts receipt
SET state='OBJECTS_CLEARED',objects_digest=control_plane.project_purge_inventory_digest(receipt.organization_id,receipt.project_id),
    objects_cleared_at=statement_timestamp()
FROM control_plane.projects project
WHERE project.id=receipt.project_id AND project.ref=$1 AND receipt.state='PENDING'
RETURNING receipt.objects_digest;
