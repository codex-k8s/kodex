-- name: credential_projection_resolve_runtime :one
SELECT revision.provider_account_ref,
       revision.provider_credential_revision_ref,
       revision.provider_credential_revision_number,
       revision.provider_secret_name,
       revision.provider_secret_uid::text,
       revision.provider_secret_resource_version,
       revision.provider_credential_sha256,
       revision.safe_snapshot -> 'secretProjections',
       lease.expires_at
FROM control_plane.runtime_leases lease
JOIN control_plane.runtime_revisions revision ON revision.id = lease.runtime_revision_id
JOIN control_plane.runs root_run ON root_run.id = revision.root_run_id
JOIN control_plane.sessions session ON session.id = revision.session_id
LEFT JOIN control_plane.session_turns turn ON turn.id = revision.turn_id
JOIN control_plane.provider_accounts account ON account.id = revision.provider_account_id
WHERE lease.organization_id = @organization_id::uuid
  AND revision.organization_id = @organization_id::uuid
  AND root_run.initiated_by = @actor_id::uuid
  AND revision.project_id = @project_id::uuid
  AND lease.ref = @lease_ref
  AND lease.workload_instance = @workload_instance
  AND lease.generation = @generation
  AND lease.state = 'CLAIMED'
  AND lease.expires_at > clock_timestamp()
  AND (@fence = '' OR lease.fence_digest = encode(digest(convert_to(@fence, 'UTF8'), 'sha256'), 'hex'))
  AND revision.ref = @runtime_revision_ref
  AND revision.revision_digest = @runtime_revision_digest
  AND revision.generation = @generation
  AND revision.attempt = @attempt
  AND revision.input_digest = @input_digest
  AND session.ref = @session_ref
  AND COALESCE(turn.ref, '') = @turn_ref
  AND account.enabled
  AND account.state = 'AUTHORIZED'
  AND account.current_credential_revision_id = revision.provider_credential_revision_id;
