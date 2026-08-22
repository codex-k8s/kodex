-- name: platform__commands_resolvegate_select_owner_gates_organization_id_ref_state :one
SELECT g.id::text,g.node_id::text,g.root_run_id::text,g.project_id::text,p.ref,g.version,g.allowed_decisions FROM control_plane.owner_gates g JOIN control_plane.projects p ON p.id=g.project_id WHERE g.organization_id=$1::uuid AND g.ref=$2 AND g.state='OPEN' FOR UPDATE
