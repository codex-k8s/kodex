-- name: provider_credential__get_current_for_reconcile :one
SELECT runtime.organization_id::text,
       account.id::text,
       credential.id::text,
       credential.revision_number,
       credential.secret_name,
       credential.secret_uid::text,
       credential.secret_resource_version,
       credential.content_sha256
FROM control_plane.assistant_runtime runtime
JOIN control_plane.sessions session
  ON session.ref = runtime.system_session_ref
JOIN control_plane.provider_accounts account
  ON account.id = session.provider_account_id
JOIN control_plane.provider_credential_revisions credential
  ON credential.id = account.current_credential_revision_id
WHERE runtime.stable_key = 'system-assistant'
  AND account.stable_key = 'default-openai-codex'
  AND account.state = 'AUTHORIZED'
  AND account.enabled
FOR UPDATE OF account;
