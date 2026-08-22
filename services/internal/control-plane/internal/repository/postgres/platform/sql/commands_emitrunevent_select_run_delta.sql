-- name: platform__commands_emitrunevent_select_run_delta :one
SELECT root.ref,
       root.state,
       root.result_summary,
       root.safe_error_code,
       root.safe_error_message,
       root.graph_revision,
       root.event_sequence,
       root.version,
       COALESCE((
           SELECT array_agg(artifact.ref ORDER BY artifact.created_at)
           FROM control_plane.artifacts artifact
           JOIN control_plane.runs artifact_run ON artifact_run.id = artifact.run_id
           WHERE artifact_run.root_run_id = root.id
       ), '{}'::text[]),
       COALESCE((
           SELECT array_agg(gate.ref ORDER BY gate.created_at)
           FROM control_plane.owner_gates gate
           WHERE gate.root_run_id = root.id
       ), '{}'::text[]),
       root.started_at,
       root.finished_at
FROM control_plane.runs root
WHERE root.organization_id = $1::uuid
  AND root.id = $2::uuid
