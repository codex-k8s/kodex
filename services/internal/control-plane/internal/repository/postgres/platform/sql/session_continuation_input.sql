-- name: session_continuation_input :one
SELECT run.input
FROM control_plane.runs run
JOIN control_plane.sessions session ON session.id = run.session_id
WHERE run.organization_id = @organization_id::uuid
  AND session.ref = @session_ref AND session.state = 'ACTIVE'
  AND run.state = 'SUCCEEDED'
  AND (@run_ref = '' OR run.ref = @run_ref)
  AND run.target_type = session.target_type AND run.target_ref = session.target_ref
  AND (@authority_project_id = '' OR session.project_id = NULLIF(@authority_project_id, '')::uuid)
  AND EXISTS (SELECT 1 FROM control_plane.catalog_access_targets target
      WHERE target.organization_id = run.organization_id AND target.kind = 'RUN' AND target.id = run.id
        AND control_plane.catalog_resource_visible(run.organization_id, @actor_id::uuid, 'run.view',
            target.kind, target.id, target.project_id, target.owner_id, target.related_ids, transaction_timestamp()))
ORDER BY run.created_at DESC, run.ref DESC
LIMIT 1;
