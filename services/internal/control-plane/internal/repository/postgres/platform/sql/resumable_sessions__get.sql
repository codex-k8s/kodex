-- name: resumable_sessions__get :one
SELECT run.ref, run.version, session.id::text, session.ref, project.id::text, project.ref,
       session.target_type, session.target_ref, account.ref
FROM control_plane.runs run
JOIN control_plane.sessions session ON session.id = run.session_id
JOIN control_plane.projects project ON project.id = session.project_id
JOIN control_plane.provider_accounts account ON account.id = session.provider_account_id
WHERE run.organization_id = @organization_id::uuid AND run.ref = @run_ref
  AND session.state = 'ACTIVE' AND run.state = 'SUCCEEDED'
  AND run.target_type = session.target_type AND run.target_ref = session.target_ref
  AND (@authority_project_id = '' OR project.id = NULLIF(@authority_project_id, '')::uuid)
  AND NOT EXISTS (SELECT 1 FROM control_plane.session_turns turn
                  WHERE turn.session_id = session.id AND turn.state IN ('QUEUED', 'RUNNING'))
  AND EXISTS (SELECT 1 FROM control_plane.catalog_access_targets target
      WHERE target.organization_id = run.organization_id AND target.kind = 'RUN' AND target.id = run.id
        AND control_plane.catalog_resource_visible(run.organization_id, @actor_id::uuid, 'run.view',
            target.kind, target.id, target.project_id, target.owner_id, target.related_ids, transaction_timestamp()));
