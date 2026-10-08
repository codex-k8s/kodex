-- name: session_archive_run_wake :one
-- Один server-owned anchor будит org projection; он не ограничивает подписки.
SELECT organization.id::text, organization.ref, root.ref, root.version
FROM control_plane.sessions session
JOIN control_plane.organizations organization ON organization.id = session.organization_id
JOIN LATERAL (
    SELECT root.ref, root.version
    FROM control_plane.runs run
    JOIN control_plane.runs root ON root.id = run.root_run_id
      AND root.organization_id = run.organization_id
    WHERE run.session_id = session.id AND run.organization_id = session.organization_id
    ORDER BY run.created_at DESC, run.ref DESC
    LIMIT 1
) root ON true
WHERE session.id::text = @session_identity OR session.ref = @session_identity;
