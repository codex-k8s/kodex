-- name: assistant_configuration_component_revision :one
SELECT revision.safe_snapshot::text
FROM control_plane.runtime_leases lease JOIN control_plane.runtime_revisions revision ON revision.id=lease.runtime_revision_id
WHERE lease.ref=$1;
