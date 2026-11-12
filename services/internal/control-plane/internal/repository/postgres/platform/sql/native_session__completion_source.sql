-- name: native_session__completion_source :one
SELECT revision.safe_snapshot, storage.native_checkpoint
FROM control_plane.session_storage storage
JOIN control_plane.runtime_revisions revision ON revision.id=@runtime_revision_id::uuid
 AND revision.organization_id=storage.organization_id AND revision.session_id=storage.session_id
 AND revision.provider_account_id=storage.provider_account_id
JOIN control_plane.runtime_leases lease ON lease.ref=@lease_ref
 AND lease.organization_id=storage.organization_id AND lease.runtime_revision_id=revision.id
JOIN control_plane.runs run ON run.id=lease.run_id AND run.session_id=storage.session_id
 AND run.organization_id=storage.organization_id
WHERE storage.organization_id=@organization_id::uuid
 AND storage.native_writer_lease_ref=lease.ref AND storage.native_writer_revision_id=revision.id
 AND storage.state IN('INITIALIZING','LIVE','ERROR')
FOR UPDATE OF storage;
