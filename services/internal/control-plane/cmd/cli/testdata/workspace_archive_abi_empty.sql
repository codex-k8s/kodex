SELECT NOT EXISTS(SELECT 1 FROM public.goose_db_version WHERE is_applied AND version_id>0)
 AND to_regclass('control_plane.session_archive_tasks') IS NULL;
