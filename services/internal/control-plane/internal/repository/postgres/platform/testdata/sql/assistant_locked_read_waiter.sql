-- name: assistant_locked_read_waiter :one
SELECT EXISTS (
  SELECT 1 FROM pg_stat_activity
  WHERE datname = current_database() AND pid <> pg_backend_pid()
    AND wait_event_type = 'Lock'
    AND position('-- name: assistant_search_resolve_lease :one' IN query) = 1
);
