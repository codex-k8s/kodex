SELECT account.ref
FROM control_plane.runs run
JOIN control_plane.sessions session ON session.id = run.session_id AND session.organization_id = run.organization_id
JOIN control_plane.provider_accounts account ON account.id = session.provider_account_id AND account.organization_id = session.organization_id
WHERE run.ref = $1;
