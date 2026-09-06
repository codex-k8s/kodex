-- name: configuration_source__cancel_work :exec
UPDATE control_plane.managed_configuration_source_work work SET state='CANCELLED',
 claimant='',fence='',lease_expires_at=NULL,updated_at=clock_timestamp()
FROM control_plane.managed_configuration_git_sources source
WHERE work.source_id=source.id AND source.configuration_set_id=$1::uuid
 AND work.organization_id=source.organization_id AND work.state IN ('QUEUED','CLAIMED');
