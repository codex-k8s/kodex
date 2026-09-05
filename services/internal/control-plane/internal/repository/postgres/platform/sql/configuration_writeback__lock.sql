-- name: configuration_writeback__lock :one
SELECT work.id::text,work.input_snapshot,work.input_sha256,work.approval_digest,work.version,work.state,work.effect,
       work.attempt,work.claim_generation,work.claimant,work.fence,work.lease_expires_at,
       work.candidate_commit_sha,work.candidate_tree_sha,work.candidate_blob_sha,work.effect_started_at,
       work.branch_confirmed_at,work.pull_request_confirmed_at,work.pull_request_ref,work.pull_request_url,
       work.failure_code,work.receipts,work.approved_at,work.deadline,work.expires_at,work.completed_at,work.created_at,
       root.ref,COALESCE(approver.ref,''),organization.ref,configuration.ref,source.ref,connection.ref,credential.ref
FROM control_plane.managed_configuration_writebacks work
JOIN control_plane.organizations organization ON organization.id=work.organization_id
JOIN control_plane.subjects root ON root.id=work.root_actor_id
LEFT JOIN control_plane.subjects approver ON approver.id=work.approver_id
JOIN control_plane.managed_configuration_sets configuration ON configuration.id=work.configuration_set_id AND configuration.organization_id=work.organization_id
JOIN control_plane.managed_configuration_git_sources source ON source.id=work.source_id AND source.organization_id=work.organization_id AND source.configuration_set_id=configuration.id
JOIN control_plane.integration_connections connection ON connection.id=work.connection_id AND connection.organization_id=work.organization_id
JOIN control_plane.integration_credential_revisions credential ON credential.id=work.credential_revision_id AND credential.connection_id=connection.id AND credential.organization_id=work.organization_id
WHERE work.organization_id=$1::uuid AND work.ref=$2
FOR UPDATE OF work;
