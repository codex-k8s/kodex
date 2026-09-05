-- name: configuration_writeback__base :one
SELECT source.accepted_raw_content
FROM control_plane.managed_configuration_git_sources source
WHERE source.organization_id=$1::uuid AND source.id=$2::uuid AND source.state='READY'
  AND source.version=$3 AND source.accepted_commit_sha=$4 AND source.accepted_content_sha256=$5;
