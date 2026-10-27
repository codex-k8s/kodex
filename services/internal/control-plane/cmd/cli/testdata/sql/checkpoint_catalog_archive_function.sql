-- Только откатываемая схема отдельной одноразовой базы.
CREATE OR REPLACE FUNCTION control_plane.protect_session_archive_receipt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NULL; END; $$;
