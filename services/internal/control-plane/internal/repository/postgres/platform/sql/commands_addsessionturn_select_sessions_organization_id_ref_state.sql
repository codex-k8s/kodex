-- name: commands_addsessionturn_select_sessions_organization_id_ref_state :one
SELECT session.project_id::text, project.ref, session.target_type, session.target_ref
FROM control_plane.sessions session
JOIN control_plane.projects project ON project.id = session.project_id
JOIN control_plane.provider_accounts account
  ON account.id = session.provider_account_id AND account.organization_id = session.organization_id
WHERE session.organization_id = $1::uuid AND session.ref = $2 AND session.state = 'ACTIVE'
  AND (account.personal_owner_subject_id IS NULL OR account.personal_owner_subject_id = $3::uuid)
FOR UPDATE OF session, project;
