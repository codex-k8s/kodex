-- name: interaction_approval_root :exec
UPDATE control_plane.runs SET root_run_id=id WHERE id=$1::uuid;
