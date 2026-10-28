-- name: repository_bundle__insert :exec
INSERT INTO control_plane.repository_bundle_publications (
    ref,organization_id,project_id,root_actor_id,root_run_id,origin_run_id,origin_node_id,
    origin_runtime_revision_id,origin_lease_ref,origin_generation,connection_id,grant_id,
    credential_revision_id,owner_gate_id,integration_invocation_id,idempotency_key,input_sha256,
    snapshot_sha256,approval_digest,input_snapshot,manifest_metadata,manifest_sha256,archive_revision_ref,manifest_revision_ref,archive_sha256,
    base_commit_sha,repository_ref,proposal_branch,created_at,expires_at
) VALUES (
    @ref,@organization_id::uuid,@project_id::uuid,@actor_id::uuid,@root_run_id::uuid,@run_id::uuid,@node_id::uuid,
    @runtime_revision_id::uuid,@lease_ref,@generation,@connection_id::uuid,@grant_id::uuid,
    @credential_id::uuid,@gate_id::uuid,@invocation_id::uuid,@idempotency_key,@input_sha256,
    @snapshot_sha256,@approval_digest,@snapshot::jsonb,@manifest_metadata::jsonb,@manifest_sha256,@archive_revision_ref,@manifest_revision_ref,@archive_sha256,
    @base_commit_sha,@repository_ref,@proposal_branch,@created_at,@expires_at
);
