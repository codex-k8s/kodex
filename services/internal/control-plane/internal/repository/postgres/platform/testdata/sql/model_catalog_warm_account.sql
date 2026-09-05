SELECT account.ref, account.version
FROM control_plane.assistant_runtime runtime
JOIN control_plane.sessions session ON session.ref = runtime.system_session_ref
JOIN control_plane.provider_accounts account ON account.id = session.provider_account_id
WHERE runtime.organization_id = $1::uuid;
