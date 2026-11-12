SELECT EXISTS(SELECT 1 FROM pg_stat_activity
 WHERE datname=current_database() AND $1::integer=ANY(pg_blocking_pids(pid)));
