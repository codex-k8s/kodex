UPDATE control_plane.runtime_leases
SET state='CLAIMED',expires_at=clock_timestamp()-interval '1 second'
WHERE ref=$1;
