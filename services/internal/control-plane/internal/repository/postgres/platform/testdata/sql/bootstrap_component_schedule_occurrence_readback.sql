SELECT occurrence.state,
       occurrence.lease_ref IS NULL
         AND occurrence.fence_digest IS NULL
         AND occurrence.workload_instance IS NULL
         AND occurrence.lease_expires_at IS NULL,
       run.source
FROM control_plane.schedule_occurrences occurrence
JOIN control_plane.runs run ON run.id = occurrence.run_id
WHERE occurrence.ref = $1
