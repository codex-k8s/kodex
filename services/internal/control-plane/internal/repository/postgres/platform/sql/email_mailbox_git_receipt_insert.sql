-- name: email_mailbox_git_receipt_insert :exec
INSERT INTO control_plane.email_mailbox_git_imports(source,source_revision,source_digest,publication_ref) VALUES($1,$2,$3,NULLIF($4,''));
