-- name: repository_bundle__lock :one
SELECT publication.id::text,publication.ref,publication.input_snapshot,publication.snapshot_sha256,
       publication.manifest_metadata,publication.version,publication.state,publication.attempt,publication.claim_generation,
       publication.claimant,publication.fence,publication.lease_expires_at,publication.approved_at,
       publication.deadline,publication.candidate_commit_sha,publication.candidate_tree_sha,
       publication.effect_started_at,publication.confirmed_at,publication.failure_code,publication.receipts,
       publication.completed_at,publication.created_at,publication.expires_at,
       publication.integration_invocation_id::text,publication.owner_gate_id::text,
       publication.project_id::text,publication.root_actor_id::text,publication.root_run_id::text,
       publication.origin_run_id::text,publication.connection_id::text,publication.grant_id::text,
       publication.credential_revision_id::text,COALESCE(publication.approver_id::text,''),publication.invocation_lease_ref,publication.archive_revision_ref,publication.manifest_revision_ref,publication.repository_ref,
       publication.continuation_state,publication.continuation_code,publication.continuation_node_ref
FROM control_plane.repository_bundle_publications publication
WHERE publication.organization_id=@organization_id::uuid AND publication.ref=@proposal_ref
FOR UPDATE OF publication;
