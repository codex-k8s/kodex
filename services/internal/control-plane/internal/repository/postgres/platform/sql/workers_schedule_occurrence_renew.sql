-- name: workers_schedule_occurrence_renew :one
WITH renewed_occurrence AS (
    UPDATE control_plane.schedule_occurrences
    SET lease_expires_at = clock_timestamp() + interval '30 seconds',
        version = version + 1,
        updated_at = clock_timestamp()
    WHERE organization_id = $1::uuid
      AND ref = $2
      AND lease_ref = $3
      AND fence_digest = $4
      AND generation = $5
      AND state = 'CLAIMED'
      AND lease_expires_at > clock_timestamp()
      AND EXISTS (SELECT 1 FROM control_plane.schedule_occurrence_attempts attempt
                  WHERE attempt.occurrence_id = schedule_occurrences.id
                    AND attempt.generation = $5 AND attempt.credential_generation = $6
                    AND attempt.state = 'CLAIMED')
    RETURNING id, attempt, generation, lease_ref, lease_expires_at
), renewed_attempt AS (
    UPDATE control_plane.schedule_occurrence_attempts attempt
    SET expires_at = occurrence.lease_expires_at
    FROM renewed_occurrence occurrence
    WHERE attempt.occurrence_id = occurrence.id
      AND attempt.attempt = occurrence.attempt
      AND attempt.generation = occurrence.generation
      AND attempt.state = 'CLAIMED'
    RETURNING attempt.occurrence_id
)
SELECT occurrence.lease_ref, occurrence.generation, occurrence.lease_expires_at
FROM renewed_occurrence occurrence
JOIN renewed_attempt attempt ON attempt.occurrence_id = occurrence.id
