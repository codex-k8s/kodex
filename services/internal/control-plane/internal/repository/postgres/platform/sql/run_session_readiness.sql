-- name: run_session_readiness :one
-- Вызывается только после canonical GetRun eligibility в той же read-only tx.
SELECT session.ref, COALESCE(storage.state, 'UNTRACKED'),
       EXISTS (
           SELECT 1 FROM control_plane.provider_accounts account
           JOIN control_plane.provider_credential_revisions credential
             ON credential.id = account.current_credential_revision_id
            AND credential.organization_id = run.organization_id
           WHERE account.id = session.provider_account_id
             AND account.organization_id = run.organization_id
             AND account.state = 'AUTHORIZED' AND account.enabled
       ),
       EXISTS (
           SELECT 1 FROM control_plane.run_nodes current_node
           JOIN control_plane.run_nodes earlier ON earlier.organization_id = run.organization_id
           JOIN control_plane.runs earlier_run ON earlier_run.id = earlier.run_id
             AND earlier_run.organization_id = run.organization_id
           WHERE current_node.run_id = run.id AND current_node.organization_id = run.organization_id
             AND current_node.type = 'AGENT_EXECUTION' AND current_node.state = 'QUEUED'
             AND earlier_run.session_id = session.id AND earlier_run.root_run_id <> run.root_run_id
             AND (earlier_run.dispatch_priority > run.dispatch_priority
                  OR (earlier_run.dispatch_priority = run.dispatch_priority AND earlier.created_at < current_node.created_at))
             AND earlier.type = 'AGENT_EXECUTION' AND earlier.state IN ('QUEUED','RUNNING','WAITING')
       ),
       COALESCE(task.ref, ''), COALESCE(task.kind, ''), COALESCE(task.state, ''),
       COALESCE(task.attempt, 0), COALESCE(task.maximum_attempts, 0), COALESCE(task.safe_error_code, '')
FROM control_plane.runs run
JOIN control_plane.sessions session ON session.id = run.session_id AND session.organization_id = run.organization_id
LEFT JOIN control_plane.session_storage storage ON storage.session_id = session.id AND storage.organization_id = run.organization_id
LEFT JOIN LATERAL (
    SELECT candidate.ref, candidate.kind, candidate.state, candidate.attempt,
           candidate.maximum_attempts, candidate.safe_error_code
    FROM control_plane.session_archive_tasks candidate
    WHERE candidate.session_id = session.id AND candidate.organization_id = run.organization_id
      AND candidate.kind IN ('SNAPSHOT','RESTORE','DELETE_PVC')
    ORDER BY candidate.created_at DESC, candidate.ref DESC
    LIMIT 1
) task ON true
WHERE run.organization_id = @organization_id::uuid AND run.ref = @run_ref AND session.ref = @session_ref;

