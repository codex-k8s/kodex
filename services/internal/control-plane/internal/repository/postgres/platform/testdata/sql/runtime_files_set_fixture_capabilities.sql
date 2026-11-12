-- name: runtime_files_set_fixture_capabilities :exec
UPDATE control_plane.agents SET capabilities=$2 WHERE id=$1::uuid;
