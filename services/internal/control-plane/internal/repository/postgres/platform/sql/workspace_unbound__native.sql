-- name: workspace_unbound__native :one
WITH initial AS (
 DELETE FROM control_plane.session_storage native USING control_plane.workspace_unbound_stops receipt,
  control_plane.runtime_leases lease,control_plane.runtime_revisions revision
 WHERE receipt.organization_id=@organization_id::uuid AND receipt.lease_id=@lease_id::uuid
  AND lease.id=receipt.lease_id AND lease.organization_id=receipt.organization_id AND lease.state<>'CLAIMED'
  AND revision.id=lease.runtime_revision_id AND native.session_id=revision.session_id AND native.organization_id=receipt.organization_id
  AND native.native_writer_lease_ref=lease.ref AND native.native_writer_revision_id=revision.id
  AND native.state='INITIALIZING' AND native.native_session_id IS NULL AND native.native_checkpoint IS NULL
  AND native.content_generation=0 AND native.current_archive_id IS NULL
 RETURNING native.session_id
), resumed AS (
 UPDATE control_plane.session_storage native
 SET native_writer_lease_ref=NULL,native_writer_revision_id=NULL,version=native.version+1,updated_at=clock_timestamp()
 FROM control_plane.workspace_unbound_stops receipt,control_plane.runtime_leases lease,control_plane.runtime_revisions revision
 WHERE receipt.organization_id=@organization_id::uuid AND receipt.lease_id=@lease_id::uuid
  AND lease.id=receipt.lease_id AND lease.organization_id=receipt.organization_id AND lease.state<>'CLAIMED'
  AND revision.id=lease.runtime_revision_id AND native.session_id=revision.session_id AND native.organization_id=receipt.organization_id
  AND native.native_writer_lease_ref=lease.ref AND native.native_writer_revision_id=revision.id
  AND native.state='LIVE' AND native.native_checkpoint IS NOT NULL AND native.content_generation>0
 RETURNING native.session_id
)
SELECT (SELECT count(*) FROM initial)+(SELECT count(*) FROM resumed);
