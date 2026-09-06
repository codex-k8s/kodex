-- name: configuration_source__refresh_due :many
SELECT configuration.ref,subject.ref,organization.ref
FROM control_plane.managed_configuration_git_sources source
JOIN control_plane.managed_configuration_sets configuration ON configuration.id=source.configuration_set_id AND configuration.organization_id=source.organization_id
JOIN control_plane.subjects subject ON subject.id=source.root_actor_id AND subject.organization_id=source.organization_id
JOIN control_plane.organizations organization ON organization.id=source.organization_id
WHERE source.organization_id=$1::uuid AND source.state='READY' AND source.next_refresh_at<=clock_timestamp()
 AND configuration.managed_by='GIT' AND configuration.source=source.ref
 AND NOT EXISTS(SELECT 1 FROM control_plane.managed_configuration_writebacks proposal WHERE proposal.configuration_set_id=configuration.id AND proposal.state IN ('WAITING_APPROVAL','QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME'))
 AND NOT EXISTS(SELECT 1 FROM control_plane.managed_configuration_source_work work WHERE work.source_id=source.id AND work.state IN ('QUEUED','CLAIMED'))
ORDER BY source.next_refresh_at,source.id LIMIT $2 FOR UPDATE OF configuration,source SKIP LOCKED;
