-- name: queries_getrungraph_select_run_edges_organization_id_ref :many
SELECT e.ref,root.ref,s.ref,t.ref,e.type,e.label FROM control_plane.run_edges e JOIN control_plane.runs root ON root.id=e.root_run_id JOIN control_plane.run_nodes s ON s.id=e.source_node_id JOIN control_plane.run_nodes t ON t.id=e.target_node_id WHERE e.organization_id=$1::uuid AND root.ref=$2 ORDER BY e.created_at
