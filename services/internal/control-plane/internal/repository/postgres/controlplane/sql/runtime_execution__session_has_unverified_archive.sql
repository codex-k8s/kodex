-- name: RuntimeExecutionSessionHasUnverifiedArchive :one
SELECT EXISTS (
    SELECT 1
    FROM control_plane.runtime_executions
    WHERE organization_id = @organization_id::uuid
      AND project_id = @project_id::uuid
      AND session_id = @session_id::uuid
      AND state IN ('SUCCEEDED', 'FAILED', 'CANCELLED', 'EXPIRED', 'RETRIED', 'SUSPENDED')
      AND (archive_reference IS NULL OR archive_sha256 IS NULL
           OR restore_proof_reference IS NULL OR restore_proof_sha256 IS NULL)
);
