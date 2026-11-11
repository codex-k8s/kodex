-- name: native_session__claim_writer :exec
INSERT INTO control_plane.session_storage(
 session_id,organization_id,project_id,provider_account_id,runtime_revision_id,
 native_session_id,content_generation,state,source_relative_path,source_sha256,
 source_size_bytes,current_archive_id,idle_since,native_checkpoint,
 native_writer_lease_ref,native_writer_revision_id)
SELECT session.id,session.organization_id,session.project_id,session.provider_account_id,revision.id,
 NULL,0,'INITIALIZING',NULL,NULL,NULL,NULL,clock_timestamp(),NULL,@lease_ref,revision.id
FROM control_plane.sessions session
JOIN control_plane.runtime_revisions revision ON revision.id=@runtime_revision_id::uuid
 AND revision.organization_id=session.organization_id AND revision.session_id=session.id
 AND revision.provider_account_id=session.provider_account_id
JOIN control_plane.runtime_leases lease ON lease.ref=@lease_ref AND lease.runtime_revision_id=revision.id
 AND lease.organization_id=session.organization_id AND lease.state='CLAIMED'
WHERE session.id=@session_id::uuid AND session.organization_id=@organization_id::uuid
ON CONFLICT(session_id) DO UPDATE
SET native_writer_lease_ref=EXCLUDED.native_writer_lease_ref,
    native_writer_revision_id=EXCLUDED.native_writer_revision_id,
    updated_at=clock_timestamp(),version=control_plane.session_storage.version+1
WHERE control_plane.session_storage.native_writer_lease_ref IS NULL
 AND control_plane.session_storage.state='LIVE'
 AND control_plane.session_storage.organization_id=EXCLUDED.organization_id
 AND control_plane.session_storage.provider_account_id=EXCLUDED.provider_account_id;
