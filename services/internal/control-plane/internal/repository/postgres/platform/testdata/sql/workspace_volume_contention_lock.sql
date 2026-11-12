SELECT root.id::text FROM control_plane.runs run
JOIN control_plane.runs root ON root.id=run.root_run_id AND root.organization_id=run.organization_id
WHERE run.ref=$1 FOR UPDATE OF root;
