-- name: RuntimeExecutionLatestSessionCodexLineage :one
SELECT id::text,
       provider_binding_id::text,
       codex_session_id,
       codex_archive_relative_path,
       codex_archive_sha256,
       codex_archive_provenance,
       terminal_outcome,
       terminal_reference
FROM control_plane.runtime_executions
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND session_id = @session_id
  AND state IN ('SUCCEEDED', 'FAILED', 'SUSPENDED')
  AND terminal_outcome IN ('SUCCEEDED', 'FAILED', 'BLOCKED')
  AND provider_binding_id IS NOT NULL
  AND codex_session_id IS NOT NULL
  AND codex_archive_relative_path IS NOT NULL
  AND codex_archive_sha256 IS NOT NULL
  AND codex_archive_provenance IS NOT NULL
ORDER BY updated_at DESC, id DESC
LIMIT 1;
