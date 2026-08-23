-- name: workers_changeoccurrence_select_schedule_occurrences_organization_id_ref_lease_ref :one
SELECT o.id::text,
       o.schedule_id::text,
       s.project_id::text,
       p.ref,
       o.state,
       o.fence_digest,
       o.generation,
       o.lease_expires_at,
       o.target_type,
       o.target_ref,
       o.run_name,
       o.input,
       o.schedule_version,
       o.input_digest
FROM control_plane.schedule_occurrences o
JOIN control_plane.schedules s ON s.id = o.schedule_id
JOIN control_plane.projects p ON p.id = s.project_id
WHERE o.organization_id = $1::uuid
  AND o.ref = $2
  AND o.lease_ref = $3
FOR UPDATE
