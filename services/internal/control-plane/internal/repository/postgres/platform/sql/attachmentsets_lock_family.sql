-- name: attachmentsets_lock_family :exec
SELECT pg_advisory_xact_lock(hashtextextended($1, 0));
