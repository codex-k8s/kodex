-- Ошибка только новой изолированной disposable-базы: runtime insert уже выполнен,
-- но финальная публикация overlay должна откатить всю транзакцию owner-команды.
CREATE FUNCTION control_plane.prepared_agent_runtime_overlay_fault() RETURNS trigger
LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic overlay publication fault' USING ERRCODE = 'XX000'; END $$;
CREATE TRIGGER prepared_agent_runtime_overlay_fault BEFORE INSERT ON control_plane.agent_config_overlay_versions
FOR EACH ROW WHEN (NEW.state = 'PUBLISHED') EXECUTE FUNCTION control_plane.prepared_agent_runtime_overlay_fault();
