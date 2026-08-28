-- name: runtime_completeexecution_update_root_usage :exec
WITH total AS (
    SELECT COALESCE(sum((turn.value->>'total_tokens')::bigint),0)::bigint AS total_tokens,
           COALESCE(sum((turn.value->>'input_tokens')::bigint),0)::bigint AS input_tokens,
           COALESCE(sum((turn.value->>'cached_input_tokens')::bigint),0)::bigint AS cached_input_tokens,
           COALESCE(sum((turn.value->>'cache_write_input_tokens')::bigint),0)::bigint AS cache_write_input_tokens,
           COALESCE(sum((turn.value->>'output_tokens')::bigint),0)::bigint AS output_tokens,
           COALESCE(sum((turn.value->>'reasoning_output_tokens')::bigint),0)::bigint AS reasoning_output_tokens,
           COALESCE(max((turn.value->>'model_context_window')::bigint),0)::bigint AS model_context_window
    FROM control_plane.runs run
    JOIN LATERAL jsonb_each(COALESCE(run.usage->'turns','{}'::jsonb)) turn ON true
    WHERE run.root_run_id=$1::uuid
)
UPDATE control_plane.runs root
SET usage=jsonb_build_object(
        'total_tokens',total.total_tokens,
        'input_tokens',total.input_tokens,
        'cached_input_tokens',total.cached_input_tokens,
        'cache_write_input_tokens',total.cache_write_input_tokens,
        'output_tokens',total.output_tokens,
        'reasoning_output_tokens',total.reasoning_output_tokens,
        'model_context_window',total.model_context_window,
        'turns',COALESCE(root.usage->'turns','{}'::jsonb)
    )
FROM total
WHERE root.id=$1::uuid
