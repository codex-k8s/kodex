-- name: workspace_archive__lock :one
SELECT w.id::text,w.ref,w.generation,w.version,w.state,w.writer_released,
 COALESCE(w.quiescence_record,''),o.ref,p.ref,s.ref,a.ref,
 r.id::text,r.ref,r.generation,r.revision_digest,r.provider_account_id::text,
 r.safe_snapshot->'workspacePolicy',w.organization_id::text,w.project_id::text
FROM control_plane.session_workspace_storage w
JOIN control_plane.sessions s ON s.id=w.session_id AND s.organization_id=w.organization_id AND s.project_id=w.project_id AND s.target_type='AGENT'
JOIN control_plane.agents a ON a.id=w.agent_id AND a.organization_id=w.organization_id AND a.project_id=w.project_id AND a.ref=s.target_ref
JOIN control_plane.organizations o ON o.id=w.organization_id
JOIN control_plane.projects p ON p.id=w.project_id AND p.organization_id=w.organization_id
JOIN control_plane.runtime_leases l ON l.id=w.writer_lease_id AND l.organization_id=w.organization_id
JOIN control_plane.runtime_revisions r ON r.id=l.runtime_revision_id AND r.organization_id=w.organization_id AND r.session_id=w.session_id
 AND r.project_id=w.project_id AND r.agent_id=w.agent_id
WHERE w.organization_id=@organization_id::uuid AND w.session_id=@session_id::uuid
 AND r.safe_snapshot->'roleRuntimeContractRevision'='4'::jsonb
 AND r.safe_snapshot #>> '{workspaceStorage,profile}'='SESSION_WORKSPACE_V2'
 AND r.safe_snapshot #>> '{workspaceStorage,workspace_ref}'=w.ref
 AND r.safe_snapshot #> '{workspaceStorage,generation}'=to_jsonb(w.generation)
 AND p.lifecycle IN ('ACTIVE','TRASHED')
FOR UPDATE OF w;
