UPDATE control_plane.provider_model_catalog_tasks
SET fence = 'mcf_forgedreplacement', expires_at = clock_timestamp() + interval '15 seconds'
WHERE ref = $1;
