-- name: platform__runtime_lease_for_update_select_runtime_leases_organization_id_ref :one
SELECT l.id::text,l.run_id::text,l.node_id::text,r.root_run_id::text,COALESCE(r.project_id::text,''),COALESCE(p.ref,''),r.ref,n.ref,l.fence_digest,l.generation,l.state,l.expires_at
FROM control_plane.runtime_leases l
JOIN control_plane.runs r ON r.id=l.run_id
LEFT JOIN control_plane.projects p ON p.id=r.project_id
JOIN control_plane.run_nodes n ON n.id=l.node_id
WHERE l.organization_id=$1::uuid AND l.ref=$2
FOR UPDATE
