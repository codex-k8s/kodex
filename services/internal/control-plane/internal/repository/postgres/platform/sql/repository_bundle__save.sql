-- name: repository_bundle__save :exec
UPDATE control_plane.repository_bundle_publications
SET version=version+1,state=@state,approver_id=NULLIF(@approver_id,'')::uuid,approved_at=@approved_at,deadline=@deadline,
    attempt=@attempt,claim_generation=@generation,claimant=@claimant,fence=@fence,lease_expires_at=@lease_expires_at,invocation_lease_ref=@invocation_lease_ref,
    candidate_commit_sha=@commit_sha,candidate_tree_sha=@tree_sha,effect_started_at=@started_at,
    confirmed_at=@confirmed_at,failure_code=@failure_code,receipts=@receipts::jsonb,
    completed_at=@completed_at,updated_at=clock_timestamp(),continuation_state=@continuation_state,continuation_code=@continuation_code,continuation_node_ref=@continuation_node_ref
WHERE organization_id=@organization_id::uuid AND id=@id::uuid AND version=@expected_version;
