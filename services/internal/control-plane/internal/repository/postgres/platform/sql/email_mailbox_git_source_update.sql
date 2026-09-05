-- name: email_mailbox_git_source_update :exec
UPDATE control_plane.managed_configuration_sets SET source_revision=$3
WHERE id=$1::uuid AND managed_by='GIT' AND source=$2;
