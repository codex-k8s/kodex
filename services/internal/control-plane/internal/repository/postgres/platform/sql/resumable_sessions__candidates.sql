-- name: resumable_sessions__candidates :many
WITH latest AS (
    SELECT DISTINCT ON (session.id)
           run.ref, run.version, run.created_at, session.id AS session_id,
           session.ref AS session_ref, project.id AS project_id, project.ref AS project_ref,
           session.target_type, session.target_ref, account.ref AS account_ref
    FROM control_plane.sessions session
    JOIN control_plane.projects project ON project.id = session.project_id
    JOIN control_plane.provider_accounts account ON account.id = session.provider_account_id
    JOIN control_plane.runs run ON run.session_id = session.id
      AND run.target_type = session.target_type AND run.target_ref = session.target_ref
    WHERE session.organization_id = @organization_id::uuid
      AND session.state = 'ACTIVE' AND run.state = 'SUCCEEDED'
      AND (@target_type = '' OR (session.target_type = @target_type AND session.target_ref = @target_ref))
      AND (@project_ref = '' OR project.ref = @project_ref)
      AND (@authority_project_id = '' OR project.id = NULLIF(@authority_project_id, '')::uuid)
      AND NOT EXISTS (SELECT 1 FROM control_plane.session_turns turn
                      WHERE turn.session_id = session.id AND turn.state IN ('QUEUED', 'RUNNING'))
      AND (@query = '' OR strpos(lower(run.title), lower(@query)) > 0
                      OR strpos(lower(run.task), lower(@query)) > 0)
      AND EXISTS (SELECT 1 FROM control_plane.catalog_access_targets target
          WHERE target.organization_id = run.organization_id AND target.kind = 'RUN' AND target.id = run.id
            AND control_plane.catalog_resource_visible(run.organization_id, @actor_id::uuid, 'run.view',
                target.kind, target.id, target.project_id, target.owner_id, target.related_ids, transaction_timestamp()))
    ORDER BY session.id, run.created_at DESC, run.ref DESC
)
SELECT ref, version, session_id::text, session_ref, project_id::text, project_ref,
       target_type, target_ref, account_ref
FROM latest
WHERE ref > @after_ref
ORDER BY ref
LIMIT @limit;
