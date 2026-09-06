-- name: prompt_continuation_session :one
SELECT session.id::text, COALESCE(project.id::text, ''), COALESCE(project.ref, ''), session.target_type,
       session.target_ref, account.ref, latest.run_ref, latest.revision_ref
FROM control_plane.sessions session
LEFT JOIN control_plane.projects project ON project.id=session.project_id
JOIN control_plane.provider_accounts account ON account.id=session.provider_account_id
JOIN LATERAL (
    SELECT run.ref AS run_ref, revision.ref AS revision_ref
    FROM control_plane.runtime_revisions revision
    JOIN control_plane.runs run ON run.id=revision.run_id
    WHERE revision.session_id=session.id AND revision.organization_id=session.organization_id
    ORDER BY revision.created_at DESC, revision.ref DESC LIMIT 1
) latest ON true
WHERE session.organization_id=@organization_id::uuid AND session.ref=@session_ref
  AND session.state='ACTIVE';
