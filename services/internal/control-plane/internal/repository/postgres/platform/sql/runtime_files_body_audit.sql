-- name: runtime_files_body_audit :exec
INSERT INTO control_plane.audit_events(ref,organization_id,project_id,actor_id,action,resource_kind,resource_ref,outcome,safe_summary,correlation_ref)
SELECT @audit_ref,lease.organization_id,run.project_id,root.initiated_by,'runtime.execution.artifact.read','ARTIFACT',
    @artifact_ref,'SUCCEEDED','i18n:RUNTIME_ARTIFACT_READ',@correlation
FROM control_plane.runtime_leases lease
JOIN control_plane.runs run ON run.id=lease.run_id
JOIN control_plane.runs root ON root.id=run.root_run_id AND root.organization_id=lease.organization_id
WHERE lease.organization_id=@organization_id::uuid AND lease.ref=@lease_ref AND lease.fence_digest=@fence_digest
  AND lease.generation=@generation AND lease.state='CLAIMED' AND lease.expires_at>clock_timestamp();
