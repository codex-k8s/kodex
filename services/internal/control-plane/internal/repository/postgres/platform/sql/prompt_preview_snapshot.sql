-- name: prompt_preview_snapshot :one
SELECT revision.safe_snapshot -> 'promptSnapshot', run.ref
FROM control_plane.runtime_revisions revision
JOIN control_plane.runs run ON run.id = revision.run_id
JOIN control_plane.sessions session ON session.id = revision.session_id
WHERE revision.organization_id = @organization_id::uuid
  AND revision.safe_snapshot ? 'promptSnapshot'
  AND (
      (@target_kind = 'RUN' AND run.ref = @target_ref)
      OR (@target_kind = 'SESSION' AND session.ref = @target_ref)
  )
ORDER BY revision.created_at DESC, revision.ref DESC
LIMIT 1;
