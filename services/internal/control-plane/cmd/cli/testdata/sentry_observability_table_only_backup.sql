-- Историческая table-only qualification не является canonical backup допуском.
GRANT SELECT ON ALL TABLES IN SCHEMA public,control_plane TO kodex_backup_reader;
ALTER DEFAULT PRIVILEGES FOR ROLE control_plane_owner IN SCHEMA control_plane GRANT SELECT ON TABLES TO kodex_backup_reader;
