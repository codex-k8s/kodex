-- name: platform__commands_resolvegate_select_owner_gates_organization_id_ref_state :one
SELECT g.id::text,
       g.node_id::text,
       g.root_run_id::text,
       g.project_id::text,
       project.ref,
       g.version,
       g.allowed_decisions,
       gate_node.ref,
       predecessor.id::text,
       predecessor.run_id::text,
       run.session_id::text
FROM control_plane.owner_gates g
JOIN control_plane.projects project ON project.id=g.project_id
JOIN control_plane.run_nodes gate_node ON gate_node.id=g.node_id
JOIN control_plane.run_nodes predecessor ON predecessor.id=gate_node.parent_node_id
JOIN control_plane.runs run ON run.id=predecessor.run_id
WHERE g.organization_id=$1::uuid
  AND g.ref=$2
  AND g.state='OPEN'
FOR UPDATE OF g
