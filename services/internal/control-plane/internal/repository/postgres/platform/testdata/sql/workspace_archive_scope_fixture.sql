-- name: workspace_archive_scope_fixture :one
SELECT s.organization_id::text,s.project_id::text,s.id::text,a.id::text,r.id::text
		FROM control_plane.sessions s JOIN control_plane.agents a ON a.ref=s.target_ref
		AND a.organization_id=s.organization_id AND a.project_id=s.project_id
        JOIN LATERAL (SELECT id FROM control_plane.runs WHERE session_id=s.id AND organization_id=s.organization_id
          AND project_id=s.project_id ORDER BY created_at DESC,id DESC LIMIT 1) r ON true WHERE s.ref=$1;
