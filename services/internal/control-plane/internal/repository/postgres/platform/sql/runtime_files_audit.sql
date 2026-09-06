-- name: runtime_files_audit :exec
INSERT INTO control_plane.audit_events(ref,organization_id,project_id,actor_id,action,resource_kind,resource_ref,outcome,safe_summary,correlation_ref)
SELECT @audit_ref,catalog.organization_id,catalog.project_id,catalog.actor_id,@action,'RUNTIME_REVISION',
    catalog.runtime_revision_ref,'SUCCEEDED',@summary,@correlation
FROM control_plane.runtime_file_catalogs catalog
JOIN control_plane.runtime_revisions revision ON revision.ref=catalog.runtime_revision_ref
JOIN control_plane.runtime_leases lease ON lease.runtime_revision_id=revision.id
WHERE catalog.id=@catalog_id::uuid AND lease.state='CLAIMED' AND lease.expires_at>clock_timestamp();
