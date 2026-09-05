-- name: configuration_source__retry :exec
UPDATE control_plane.managed_configuration_source_work SET state='QUEUED',attempt=attempt+1,
 receipt=$2::jsonb,failure_code=$3,claimant='',fence='',lease_expires_at=NULL,updated_at=clock_timestamp()
WHERE id=$1::uuid AND state='CLAIMED' AND attempt<3 AND deadline>clock_timestamp();
