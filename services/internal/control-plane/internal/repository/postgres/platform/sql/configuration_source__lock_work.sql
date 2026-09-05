-- name: configuration_source__lock_work :one
SELECT work.id::text,work.state,work.attempt,work.claim_generation,work.claimant,work.fence,work.lease_expires_at,
 work.deadline,work.input_snapshot,work.input_sha256,COALESCE(work.receipt,'{}'::jsonb),configuration.ref,subject.ref,organization.ref
FROM control_plane.managed_configuration_source_work work
JOIN control_plane.managed_configuration_git_sources source ON source.id=work.source_id AND source.organization_id=work.organization_id
JOIN control_plane.managed_configuration_sets configuration ON configuration.id=source.configuration_set_id AND configuration.organization_id=source.organization_id
JOIN control_plane.subjects subject ON subject.id=work.root_actor_id AND subject.organization_id=work.organization_id
JOIN control_plane.organizations organization ON organization.id=work.organization_id
WHERE work.organization_id=$1::uuid AND work.ref=$2
FOR UPDATE OF configuration,source,work;
