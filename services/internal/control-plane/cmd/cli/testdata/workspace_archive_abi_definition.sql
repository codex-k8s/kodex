SELECT pg_get_functiondef(p.oid), (to_jsonb(p)-'prosrc')::text, md5(p.prosrc)
FROM pg_proc p WHERE p.oid='control_plane.guard_workspace_archive_task()'::regprocedure;
