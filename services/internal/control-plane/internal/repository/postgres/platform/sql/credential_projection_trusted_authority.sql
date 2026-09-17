-- name: credential_projection_trusted_authority :one
-- Только owner назначает scope; полная eligibility проверяется следующим
-- credential_projection_resolve_runtime в той же repeatable-read транзакции.
SELECT root_run.initiated_by::text, revision.organization_id::text,
       COALESCE(revision.project_id::text, ''), lease.expires_at
FROM control_plane.runtime_leases lease
JOIN control_plane.runtime_revisions revision ON revision.id = lease.runtime_revision_id
JOIN control_plane.runs root_run ON root_run.id = revision.root_run_id
WHERE lease.organization_id = @organization_id::uuid
  AND revision.organization_id = lease.organization_id
  AND root_run.organization_id = revision.organization_id
  AND lease.ref = @lease_ref
  AND lease.workload_instance = @workload_instance
  AND lease.generation = @generation
  AND lease.state = 'CLAIMED'
  AND lease.expires_at > clock_timestamp()
  AND revision.ref = @runtime_revision_ref
  AND revision.revision_digest = @runtime_revision_digest;
