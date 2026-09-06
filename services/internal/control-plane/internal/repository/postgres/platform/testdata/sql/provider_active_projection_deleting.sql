UPDATE control_plane.provider_accounts account SET state='DELETING',enabled=false
FROM control_plane.runtime_revisions revision
JOIN control_plane.runtime_leases lease ON lease.runtime_revision_id=revision.id
WHERE lease.ref=$1 AND account.id=revision.provider_account_id;
