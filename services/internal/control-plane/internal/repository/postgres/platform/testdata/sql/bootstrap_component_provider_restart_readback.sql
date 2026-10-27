SELECT account.state, account.current_credential_revision_id::text, account.version,
       count(revision.id)::bigint
FROM control_plane.provider_accounts account
JOIN control_plane.provider_credential_revisions revision
  ON revision.provider_account_id = account.id
WHERE account.id = $1::uuid
GROUP BY account.state, account.current_credential_revision_id, account.version
