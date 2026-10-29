-- name: workflow_evidence__plan_set :one
SELECT pg_catalog.set_config('plan_cache_mode',@mode,true);
