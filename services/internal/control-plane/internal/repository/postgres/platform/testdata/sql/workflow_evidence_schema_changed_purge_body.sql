-- Только fault injection в отдельной disposable schema; production данные не изменяются.
DO $$ DECLARE source text; BEGIN SELECT pg_get_functiondef('control_plane.purge_project_database(uuid,uuid,text)'::regprocedure) INTO source; EXECUTE replace(source,'BEGIN','BEGIN /* drift */'); END $$;
