-- name: runtime_secret_maximum_revision :one
SELECT GREATEST(secret.current_revision, COALESCE(MAX(operation.target_revision), 0),
    COALESCE((SELECT max(draft_operation.target_revision)
      FROM control_plane.runtime_secret_draft_operations draft_operation
      JOIN control_plane.runtime_secret_drafts draft ON draft.id=draft_operation.draft_id
      WHERE draft.secret_id=@secret_id::uuid),0))
FROM control_plane.runtime_secrets secret
LEFT JOIN control_plane.runtime_secret_operations operation
  ON operation.secret_id = secret.id
 AND operation.kind IN ('CREATE', 'ROTATE')
WHERE secret.id = @secret_id::uuid
GROUP BY secret.current_revision;
