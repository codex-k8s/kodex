-- name: provider_financial_guard :one
SELECT lease.created_at,
       CASE WHEN account.financial_credential_revision_id = revision.provider_credential_revision_id THEN account.financial_collector_created_at END,
       CASE WHEN account.financial_credential_revision_id = revision.provider_credential_revision_id THEN COALESCE(account.financial_collector_lease_id::text, '') ELSE '' END,
       CASE WHEN account.financial_credential_revision_id = revision.provider_credential_revision_id THEN COALESCE(account.financial_sequence, 0) ELSE 0 END,
       CASE WHEN account.financial_credential_revision_id = revision.provider_credential_revision_id THEN account.financial_observation END
FROM control_plane.provider_accounts account,
     control_plane.runtime_revisions revision,
     control_plane.runtime_leases lease,
     control_plane.runs root,
     control_plane.run_nodes node,
     control_plane.session_turns turn,
     control_plane.sessions session
WHERE revision.id = @runtime_revision_id::uuid
  AND revision.organization_id = @organization_id::uuid
  AND lease.id = @lease_id::uuid
  AND lease.organization_id = revision.organization_id
  AND lease.runtime_revision_id = revision.id
  AND lease.state = 'CLAIMED' AND lease.expires_at > clock_timestamp()
  AND revision.generation = lease.generation
  AND revision.run_id = lease.run_id
  AND revision.root_run_id = @root_run_id::uuid
  AND node.id = lease.node_id AND node.state = 'RUNNING'
  AND node.organization_id = revision.organization_id
  AND node.id = revision.node_id AND node.attempt = revision.attempt
  AND node.root_run_id = revision.root_run_id
  AND turn.id = revision.turn_id AND turn.id = node.turn_id
  AND turn.organization_id = revision.organization_id AND turn.state = 'RUNNING'
  AND session.id = revision.session_id AND session.id = turn.session_id
  AND session.organization_id = revision.organization_id AND session.state = 'ACTIVE'
  AND NOT EXISTS (SELECT 1 FROM control_plane.runtime_leases newer
    WHERE newer.node_id = lease.node_id AND newer.generation > lease.generation)
  AND root.id = @root_run_id::uuid AND root.state NOT IN ('SUCCEEDED', 'FAILED', 'CANCELLED')
  AND account.id = revision.provider_account_id
  AND account.organization_id = revision.organization_id
  AND account.current_credential_revision_id = revision.provider_credential_revision_id
  AND account.state = 'AUTHORIZED' AND account.enabled
FOR UPDATE OF account;
