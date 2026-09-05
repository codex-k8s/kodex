-- name: prompt_continuation_previous :one
SELECT revision.id::text, revision.ref, revision.safe_snapshot
FROM control_plane.runtime_revisions revision
JOIN control_plane.sessions session ON session.id = revision.session_id
WHERE revision.organization_id = @organization_id::uuid AND session.ref = @session_ref
ORDER BY revision.created_at DESC, revision.ref DESC
LIMIT 1;
