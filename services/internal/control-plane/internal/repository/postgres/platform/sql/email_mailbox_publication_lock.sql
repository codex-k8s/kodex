-- name: email_mailbox_publication_lock :exec
SELECT pg_advisory_xact_lock(hashtextextended('control-plane/email-mailbox-publication',0));
