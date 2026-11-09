-- name: integration_approval__self_lock :one
SELECT approval.id::text
FROM control_plane.integration_approval_scopes approval
JOIN control_plane.runs root ON root.id=approval.root_run_id AND root.organization_id=approval.organization_id
WHERE approval.organization_id=$1::uuid AND root.initiated_by=$2::uuid
 AND 'ias_'||replace(approval.id::text,'-','')=$3
FOR UPDATE OF approval
