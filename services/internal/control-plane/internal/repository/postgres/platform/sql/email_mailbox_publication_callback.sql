-- name: email_mailbox_publication_callback :exec
UPDATE control_plane.email_mailbox_publications SET callback_at=COALESCE(callback_at,clock_timestamp())
WHERE revision=$1 AND digest=$2 AND state IN ('PENDING','READY') AND applied_at IS NOT NULL
    AND revision=(SELECT max(revision) FROM control_plane.email_mailbox_publications);
