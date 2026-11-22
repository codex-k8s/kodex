-- name: kubernetes_project__object_receipt :one
WITH exact AS MATERIALIZED (
 SELECT DISTINCT receipt.* FROM control_plane.projects project
 CROSS JOIN LATERAL control_plane.kubernetes_project_object_receipts(project.organization_id,project.id) receipt
 WHERE project.organization_id=@organization_id::uuid AND project.id=@project_id::uuid
  AND project.ref=@project_ref AND project.lifecycle='PURGE_PENDING'
  AND receipt.object_key=@object_key AND receipt.object_version=@object_version
)
SELECT min(object_key),min(object_version),min(object_etag),min(digest),min(size_bytes)
FROM exact HAVING count(*)=1;
