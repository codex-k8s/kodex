SELECT $2::integer = ANY(pg_blocking_pids($1::integer));
