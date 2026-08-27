-- name: bootstrap_component_provider_credential_readback :one
SELECT credential.revision_number,
       credential.secret_uid::text,
       credential.secret_resource_version,
       credential.content_sha256,
       account.version,
       (SELECT count(*)
        FROM control_plane.provider_credential_revisions revisions
        WHERE revisions.provider_account_id = account.id)
FROM control_plane.provider_accounts account
JOIN control_plane.provider_credential_revisions credential
  ON credential.id = account.current_credential_revision_id
WHERE account.stable_key = 'default-openai-codex';
