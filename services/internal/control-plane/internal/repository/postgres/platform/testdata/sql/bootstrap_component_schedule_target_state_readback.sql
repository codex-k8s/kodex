SELECT schedule.enabled,
       occurrence.state,
       occurrence.lease_ref IS NULL
         AND occurrence.fence_digest IS NULL
         AND occurrence.workload_instance IS NULL
         AND occurrence.lease_expires_at IS NULL
FROM control_plane.schedules schedule
JOIN control_plane.schedule_occurrences occurrence ON occurrence.schedule_id = schedule.id
WHERE schedule.ref = $1
  AND occurrence.ref = $2
