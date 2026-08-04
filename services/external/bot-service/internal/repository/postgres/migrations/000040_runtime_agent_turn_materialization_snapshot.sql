-- +goose Up
ALTER TABLE matter_codex_runtime_agent_binding_discoveries
	ADD COLUMN agent_session_id bigint,
	ADD COLUMN agent_session_key text,
	ADD COLUMN agent_session_version bigint,
	ADD COLUMN agent_session_turn_version bigint,
	ADD COLUMN role_stable_key text,
	ADD COLUMN external_channel_ref text,
	ADD COLUMN prompt_text text;

UPDATE matter_codex_runtime_agent_binding_discoveries AS discovery
SET agent_session_id = session_row.id,
	agent_session_key = session_row.session_key,
	agent_session_version = session_row.binding_version,
	agent_session_turn_version = turn_row.binding_version,
	role_stable_key = role_row.name,
	external_channel_ref = chat_row.mattermost_channel_id,
	prompt_text = turn_row.message
FROM matter_codex_agent_session_turns turn_row
JOIN matter_codex_agent_sessions session_row ON session_row.id = turn_row.session_id
JOIN matter_codex_agent_roles role_row ON role_row.id = session_row.role_id
JOIN matter_codex_chats chat_row ON chat_row.id = session_row.chat_id
WHERE discovery.agent_session_turn_id = turn_row.id
	AND discovery.agent_run_id = turn_row.run_id
	AND discovery.source_ref = turn_row.mattermost_post_id;

ALTER TABLE matter_codex_runtime_agent_binding_discoveries
	ALTER COLUMN agent_session_id SET NOT NULL,
	ALTER COLUMN agent_session_key SET NOT NULL,
	ALTER COLUMN agent_session_version SET NOT NULL,
	ALTER COLUMN agent_session_turn_version SET NOT NULL,
	ALTER COLUMN role_stable_key SET NOT NULL,
	ALTER COLUMN external_channel_ref SET NOT NULL,
	ALTER COLUMN prompt_text SET NOT NULL,
	ADD CONSTRAINT runtime_agent_discovery_session_id_valid CHECK (agent_session_id > 0),
	ADD CONSTRAINT runtime_agent_discovery_session_key_valid CHECK (length(agent_session_key) BETWEEN 1 AND 256),
	ADD CONSTRAINT runtime_agent_discovery_session_version_valid CHECK (agent_session_version > 0),
	ADD CONSTRAINT runtime_agent_discovery_turn_version_valid CHECK (agent_session_turn_version > 0),
	ADD CONSTRAINT runtime_agent_discovery_role_valid CHECK (length(role_stable_key) BETWEEN 1 AND 128),
	ADD CONSTRAINT runtime_agent_discovery_channel_valid CHECK (length(external_channel_ref) BETWEEN 1 AND 256),
	ADD CONSTRAINT runtime_agent_discovery_prompt_valid CHECK (length(prompt_text) BETWEEN 1 AND 1048576);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION matter_codex_enqueue_runtime_agent_binding_discovery()
RETURNS trigger
LANGUAGE plpgsql
SECURITY INVOKER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
	IF length(btrim(NEW.mattermost_post_id)) > 0 THEN
		INSERT INTO public.matter_codex_runtime_agent_binding_discoveries(
			agent_session_turn_id, agent_run_id, source_ref,
			agent_session_id, agent_session_key, agent_session_version,
			agent_session_turn_version, role_stable_key,
			external_channel_ref, prompt_text
		)
		SELECT NEW.id, NEW.run_id, NEW.mattermost_post_id,
			session_row.id, session_row.session_key, session_row.binding_version,
			NEW.binding_version, role_row.name,
			chat_row.mattermost_channel_id, NEW.message
		FROM public.matter_codex_agent_sessions session_row
		JOIN public.matter_codex_agent_roles role_row ON role_row.id = session_row.role_id
		JOIN public.matter_codex_chats chat_row ON chat_row.id = session_row.chat_id
		JOIN public.matter_codex_chat_participants participant
			ON participant.chat_id = chat_row.id AND participant.role_id = role_row.id
		WHERE session_row.id = NEW.session_id
			AND role_row.project_id = session_row.project_id
			AND chat_row.project_id = session_row.project_id
			AND role_row.enabled AND participant.enabled AND chat_row.status = 'active'
		ON CONFLICT (agent_session_turn_id) DO NOTHING;
		IF NOT FOUND THEN
			RAISE EXCEPTION 'runtime agent discovery owner snapshot is unavailable' USING ERRCODE = '23514';
		END IF;
	END IF;
	RETURN NEW;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
	RAISE EXCEPTION 'migration 000040 is forward-only: runtime materialization snapshots cannot be removed safely';
END
$$;
-- +goose StatementEnd
