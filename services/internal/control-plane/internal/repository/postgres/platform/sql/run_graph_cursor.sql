-- name: run_graph_cursor :one
SELECT root.ref, root.graph_revision, root.event_sequence
FROM control_plane.runs child
JOIN control_plane.runs root ON root.id = child.root_run_id
WHERE child.organization_id = @organization_id::uuid
  AND child.ref = @run_ref
  AND root.organization_id = child.organization_id
  AND root.project_id IS NOT DISTINCT FROM child.project_id
  AND root.ref = @root_run_ref
  AND root.root_run_id = root.id;
