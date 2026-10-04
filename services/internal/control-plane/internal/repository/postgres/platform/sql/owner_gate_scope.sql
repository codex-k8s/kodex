-- name: owner_gate_scope :one
SELECT gate.scope_kind,organization.ref,COALESCE(project.ref,'')
FROM control_plane.owner_gates gate
JOIN control_plane.organizations organization ON organization.id=gate.organization_id
LEFT JOIN control_plane.projects project ON project.id=gate.project_id
WHERE gate.organization_id=$1::uuid AND gate.ref=$2
  AND ((gate.scope_kind='PROJECT' AND project.id IS NOT NULL) OR
       (gate.scope_kind='ORGANIZATION' AND gate.project_id IS NULL
        AND control_plane.owned_organization_assistant_run(gate.organization_id,gate.root_run_id)));
