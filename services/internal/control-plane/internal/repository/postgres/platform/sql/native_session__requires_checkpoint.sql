-- name: native_session__requires_checkpoint :one
SELECT revision.safe_snapshot->>'runtimeProvider'='opencode'
FROM control_plane.runtime_revisions revision
JOIN control_plane.runtime_leases lease ON lease.runtime_revision_id=revision.id
 AND lease.organization_id=revision.organization_id
WHERE revision.organization_id=@organization_id::uuid
 AND revision.id=@runtime_revision_id::uuid AND lease.ref=@lease_ref;
