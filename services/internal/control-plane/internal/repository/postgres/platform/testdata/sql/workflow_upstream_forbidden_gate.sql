-- name: workflow_upstream_forbidden_gate :exec
UPDATE control_plane.owner_gates gate SET state='CANCELLED',closure_kind='SUPERSEDED',decision=NULL,
    resolved_at=clock_timestamp(),resolved_by=root.initiated_by,version=gate.version+1
FROM control_plane.runs root JOIN control_plane.projects project ON project.id=root.project_id AND project.organization_id=root.organization_id
WHERE gate.organization_id=root.organization_id AND gate.root_run_id=root.id AND gate.project_id=root.project_id
    AND gate.ref=@gate_ref AND root.ref=@run_ref AND project.ref=@project_ref AND root.id=root.root_run_id;
