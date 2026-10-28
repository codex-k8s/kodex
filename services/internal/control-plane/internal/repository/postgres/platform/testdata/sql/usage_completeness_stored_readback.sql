SELECT run.ref, run.usage
FROM control_plane.runs run
JOIN control_plane.runs root ON root.id = run.root_run_id
WHERE root.ref = $1
ORDER BY run.ref;
