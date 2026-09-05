SELECT cardinality(pg_blocking_pids($1::integer)) > 0;
