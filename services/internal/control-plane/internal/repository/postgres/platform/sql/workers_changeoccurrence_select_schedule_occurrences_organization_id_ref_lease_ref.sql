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
       o.input_digest,
       o.attempt,
       revision.digest,
       o.target_version,
       o.target_digest,
       o.automation_text,
       o.automation_text_digest,
       o.prompt_inputs,
       o.prompt_inputs_digest,
       o.initiated_by::text,
       initiator.ref,
       initiator.display_name,
       revision.session_policy,
       COALESCE((SELECT session.ref FROM control_plane.sessions session
                 WHERE session.id = s.continue_session_id AND session.target_ref = o.target_ref
                   AND session.target_type = o.target_type), '')
FROM control_plane.schedule_occurrences o
JOIN control_plane.schedules s ON s.id = o.schedule_id
JOIN control_plane.projects p ON p.id = s.project_id
JOIN control_plane.schedule_revisions revision ON revision.id = o.schedule_revision_id
JOIN control_plane.subjects initiator ON initiator.id = o.initiated_by
WHERE o.organization_id = $1::uuid
  AND o.ref = $2
  AND o.lease_ref = $3
  AND EXISTS (SELECT 1 FROM control_plane.schedule_occurrence_attempts attempt
              WHERE attempt.occurrence_id = o.id AND attempt.generation = o.generation
                AND attempt.credential_generation = $4 AND attempt.state = 'CLAIMED')
FOR UPDATE
