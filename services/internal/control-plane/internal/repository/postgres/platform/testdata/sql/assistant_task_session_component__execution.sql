-- name: assistant_task_session_component__execution :one
WITH previous AS MATERIALIZED (
 SELECT ref,safe_delta->'Execution' AS execution FROM control_plane.run_events WHERE ref=$1
), changed AS (
 UPDATE control_plane.run_events event SET safe_delta=jsonb_set(event.safe_delta,'{Execution,SessionRef}',to_jsonb($2::text))
 FROM previous WHERE event.ref=previous.ref RETURNING event.ref
)
SELECT previous.execution FROM previous JOIN changed USING(ref);
