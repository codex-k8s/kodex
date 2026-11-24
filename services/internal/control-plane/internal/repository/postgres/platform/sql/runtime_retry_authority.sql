-- name: runtime_retry_authority :one
SELECT run.target_type,run.target_ref,project.ref
FROM control_plane.runs run
JOIN control_plane.projects project ON project.id=run.project_id AND project.organization_id=run.organization_id
WHERE run.organization_id=@organization_id::uuid AND run.ref=@run_ref
  AND NOT EXISTS (
    SELECT 1 FROM control_plane.sessions session
    JOIN control_plane.provider_accounts account
      ON account.id = session.provider_account_id AND account.organization_id = session.organization_id
    WHERE session.id = run.session_id AND account.personal_owner_subject_id IS NOT NULL
      AND account.personal_owner_subject_id <> @actor_id::uuid
  );
