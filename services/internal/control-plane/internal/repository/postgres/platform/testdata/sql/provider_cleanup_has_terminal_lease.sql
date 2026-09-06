SELECT EXISTS (
 SELECT 1 FROM control_plane.runtime_leases lease
 JOIN control_plane.runtime_revisions revision ON revision.id=lease.runtime_revision_id
 WHERE revision.provider_account_id=$1::uuid AND lease.state='COMPLETED');
