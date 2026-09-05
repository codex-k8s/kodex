-- name: configuration_source__accept :exec
UPDATE control_plane.managed_configuration_git_sources SET accepted_commit_sha=$2,accepted_content_sha256=$3,
 accepted_revision_id=$4::uuid,synced_at=clock_timestamp() WHERE id=$1::uuid;
