-- name: configuration_writeback__update :exec
UPDATE control_plane.managed_configuration_writebacks SET version=version+1,state=@state,effect=@effect,
 attempt=@attempt,claim_generation=@claim_generation,claimant=@claimant,fence=@fence,lease_expires_at=@lease,
 candidate_commit_sha=@candidate_commit,candidate_tree_sha=@candidate_tree,candidate_blob_sha=@candidate_blob,
 effect_started_at=@effect_started,branch_confirmed_at=@branch_confirmed,pull_request_confirmed_at=@pr_confirmed,
 pull_request_ref=@pr_ref,pull_request_url=@pr_url,failure_code=@failure,receipts=@receipts::jsonb,
 approved_at=@approved,approver_id=(SELECT id FROM control_plane.subjects WHERE ref=NULLIF(@approver,'')),
 deadline=@deadline,completed_at=@completed,updated_at=clock_timestamp()
WHERE id=@id::uuid AND organization_id=@organization_id::uuid AND version=@version;
