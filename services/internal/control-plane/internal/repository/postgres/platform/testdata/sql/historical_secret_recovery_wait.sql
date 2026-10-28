-- name: historical_secret_recovery_wait :one
SELECT EXISTS (
    SELECT 1 FROM pg_stat_activity activity
    WHERE activity.datname=current_database() AND activity.pid=$1
      AND activity.wait_event_type='Lock' AND $2::integer=ANY(pg_blocking_pids(activity.pid))
);
