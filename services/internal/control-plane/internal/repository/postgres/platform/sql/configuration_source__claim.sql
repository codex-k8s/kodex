-- name: configuration_source__claim :exec
UPDATE control_plane.managed_configuration_source_work SET state='CLAIMED',attempt=$2,
 claim_generation=claim_generation+1,claimant=$3,fence=$4,lease_expires_at=$5,updated_at=clock_timestamp()
WHERE id=$1::uuid AND state IN ('QUEUED','CLAIMED');
