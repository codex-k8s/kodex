-- name: runtime_claimexecution_count_active_provider_leases :one
SELECT count(*)
FROM control_plane.runtime_leases lease
JOIN control_plane.runtime_revisions revision ON revision.id = lease.runtime_revision_id
WHERE revision.provider_account_id = $1::uuid
  AND revision.organization_id = $2::uuid
  AND lease.organization_id = $2::uuid
  AND lease.state = 'CLAIMED'
  AND lease.expires_at > clock_timestamp();
