-- name: workers_schedule_attempt_finish :exec
UPDATE control_plane.schedule_occurrence_attempts
SET state = $2,
    safe_error_code = $3,
    completed_at = clock_timestamp(),
    run_id = CASE WHEN $2 = 'MATERIALIZED' THEN
        (SELECT run_id FROM control_plane.schedule_occurrences WHERE id = $1::uuid) END,
    session_id = CASE WHEN $2 = 'MATERIALIZED' THEN
        (SELECT run.session_id FROM control_plane.runs run JOIN control_plane.schedule_occurrences occurrence
         ON occurrence.run_id = run.id WHERE occurrence.id = $1::uuid) END,
    turn_id = CASE WHEN $2 = 'MATERIALIZED' THEN
        (SELECT turn.id FROM control_plane.session_turns turn JOIN control_plane.schedule_occurrences occurrence
         ON occurrence.run_id = turn.run_id WHERE occurrence.id = $1::uuid ORDER BY turn.turn_number LIMIT 1) END
WHERE occurrence_id = $1::uuid
  AND attempt = $4
  AND generation = $5
  AND state = 'CLAIMED'
