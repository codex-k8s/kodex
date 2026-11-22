SELECT jsonb_build_object(
 'head',COALESCE(max(version_id) FILTER(WHERE is_applied),0),
 'serverVersionNum',current_setting('server_version_num'),
 'history',COALESCE(jsonb_agg(jsonb_build_array(id,version_id,is_applied) ORDER BY id),'[]'::jsonb)
) FROM public.goose_db_version;
