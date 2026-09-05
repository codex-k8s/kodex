-- name: email_mailbox_git_receipt :one
SELECT source_revision,source_digest,COALESCE(publication_ref,'') FROM control_plane.email_mailbox_git_imports
WHERE source=$1 ORDER BY source_revision DESC LIMIT 1;
