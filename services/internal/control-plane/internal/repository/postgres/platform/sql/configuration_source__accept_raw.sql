-- name: configuration_source__accept_raw :exec
UPDATE control_plane.managed_configuration_git_sources SET accepted_raw_content=$4
WHERE id=$1::uuid AND accepted_commit_sha=$2 AND accepted_content_sha256=$3;
