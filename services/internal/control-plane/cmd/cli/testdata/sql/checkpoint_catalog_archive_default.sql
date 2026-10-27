-- Только откатываемая схема отдельной одноразовой базы.
ALTER TABLE control_plane.session_archives ALTER COLUMN created_at SET DEFAULT '2000-01-01T00:00:00Z'::timestamptz;
