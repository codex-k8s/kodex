-- name: native_session__closed_writer :one
SELECT lease.id::text,run.id::text,node.id::text,root.id::text,
 COALESCE(run.project_id::text,''),COALESCE(project.ref,''),run.ref,
 revision.id::text,COALESCE(turn.ref,''),session.ref,lease.fence_digest,lease.generation
FROM control_plane.runtime_leases lease
JOIN control_plane.runtime_revisions revision ON revision.id=lease.runtime_revision_id AND revision.organization_id=lease.organization_id
JOIN control_plane.runs run ON run.id=lease.run_id AND run.organization_id=lease.organization_id
JOIN control_plane.runs root ON root.id=run.root_run_id AND root.organization_id=run.organization_id
JOIN control_plane.run_nodes node ON node.id=lease.node_id AND node.run_id=run.id
JOIN control_plane.sessions session ON session.id=run.session_id AND session.id=revision.session_id
 AND session.organization_id=run.organization_id
LEFT JOIN control_plane.session_turns turn ON turn.id=node.turn_id AND turn.session_id=session.id
LEFT JOIN control_plane.projects project ON project.id=run.project_id AND project.organization_id=run.organization_id
WHERE lease.organization_id=@organization_id::uuid AND lease.ref=@lease_ref
 AND revision.safe_snapshot->>'runtimeProvider'='opencode'
 AND (root.state IN('SUCCEEDED','FAILED','CANCELLED')
   OR lease.state='EXPIRED' AND root.state IN('PENDING','RUNNING','CANCELLING'))
 AND lease.state IN('COMPLETED','CANCELLED','EXPIRED')
FOR UPDATE OF lease,root;
