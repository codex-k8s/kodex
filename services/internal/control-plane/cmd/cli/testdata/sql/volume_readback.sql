SELECT count(*) FROM pg_class r JOIN pg_namespace n ON n.oid=r.relnamespace
WHERE n.nspname='control_plane' AND r.relkind='r' AND r.relname IN
('session_workspace_volumes','session_workspace_volume_tasks','session_workspace_volume_attempts');
