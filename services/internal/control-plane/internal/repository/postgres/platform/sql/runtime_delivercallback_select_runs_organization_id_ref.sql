-- name: runtime_delivercallback_select_runs_organization_id_ref :one
SELECT child.id::text,
       child.root_run_id::text,
       child.project_id::text,
       project.ref,
       child.parent_run_id::text,
       child.result_summary,
       child.state,
       edge.id::text,
       edge.target_node_id::text,
       target.ref
FROM control_plane.runs child
JOIN control_plane.projects project ON project.id = child.project_id
JOIN control_plane.run_edges edge ON edge.root_run_id = child.root_run_id AND edge.ref = $3 AND edge.type = 'CALLBACK_TO'
JOIN control_plane.run_nodes target ON target.id = edge.target_node_id
WHERE child.organization_id = $1::uuid
  AND child.ref = $2
FOR UPDATE OF child, edge
