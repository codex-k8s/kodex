-- name: runtime_completeexecution_update_run_usage :exec
WITH current AS (
    SELECT COALESCE(usage->'turns','{}'::jsonb) || jsonb_build_object($2::text,$3::jsonb) AS turns
    FROM control_plane.runs
    WHERE id=$1::uuid
), total AS (
    SELECT COALESCE(sum((item.value->>'total_tokens')::bigint),0)::bigint AS total_tokens,
           COALESCE(sum((item.value->>'input_tokens')::bigint),0)::bigint AS input_tokens,
           COALESCE(sum((item.value->>'cached_input_tokens')::bigint),0)::bigint AS cached_input_tokens,
           COALESCE(sum((item.value->>'cache_write_input_tokens')::bigint),0)::bigint AS cache_write_input_tokens,
           COALESCE(sum((item.value->>'output_tokens')::bigint),0)::bigint AS output_tokens,
           COALESCE(sum((item.value->>'reasoning_output_tokens')::bigint),0)::bigint AS reasoning_output_tokens,
           COALESCE(max((item.value->>'model_context_window')::bigint),0)::bigint AS model_context_window,
           current.turns
    FROM current
    LEFT JOIN LATERAL jsonb_each(current.turns) item ON true
    GROUP BY current.turns
)
UPDATE control_plane.runs run
SET usage=jsonb_build_object(
        'total_tokens',total.total_tokens,
        'input_tokens',total.input_tokens,
        'cached_input_tokens',total.cached_input_tokens,
        'cache_write_input_tokens',total.cache_write_input_tokens,
        'output_tokens',total.output_tokens,
        'reasoning_output_tokens',total.reasoning_output_tokens,
        'model_context_window',total.model_context_window,
        'turns',total.turns
    )
FROM total
WHERE run.id=$1::uuid
