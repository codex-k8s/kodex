-- name: commands_addsessionturn_select_runs_organization_id_ref :one
SELECT r.root_run_id::text FROM control_plane.runs r JOIN control_plane.sessions s ON s.id=r.session_id WHERE r.organization_id=$1::uuid AND r.ref=$2 AND s.ref=$3
