-- name: native_session__source :one
SELECT storage.state, storage.native_checkpoint,
       storage.native_writer_lease_ref IS NOT NULL,
       COALESCE(account.ref,''), revision.safe_snapshot
FROM control_plane.session_storage storage
JOIN control_plane.sessions session
  ON session.id=storage.session_id AND session.organization_id=storage.organization_id
LEFT JOIN control_plane.provider_accounts account
  ON account.id=storage.provider_account_id AND account.organization_id=storage.organization_id
LEFT JOIN control_plane.runtime_revisions revision
  ON revision.id=storage.runtime_revision_id AND revision.organization_id=storage.organization_id
 AND revision.session_id=storage.session_id AND revision.provider_account_id=storage.provider_account_id
WHERE storage.organization_id=@organization_id::uuid AND session.ref=@session_ref
FOR UPDATE OF storage;
