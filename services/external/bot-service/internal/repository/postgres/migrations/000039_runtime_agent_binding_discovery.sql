-- +goose Up
CREATE TABLE matter_codex_runtime_agent_binding_discoveries (
	id bigserial PRIMARY KEY,
	agent_session_turn_id bigint NOT NULL UNIQUE
		REFERENCES matter_codex_agent_session_turns(id) ON DELETE RESTRICT,
	agent_run_id text NOT NULL UNIQUE CHECK (length(agent_run_id) BETWEEN 1 AND 256),
	source_ref text NOT NULL CHECK (length(source_ref) BETWEEN 1 AND 256),
	state text NOT NULL DEFAULT 'PENDING' CHECK (state IN ('PENDING', 'LEASED', 'DELIVERED')),
	lease_token text,
	lease_expires_at timestamptz,
	next_attempt_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
	attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
	last_error_code text,
	delivered_at timestamptz,
	created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
	CHECK ((state = 'LEASED') = (lease_token IS NOT NULL AND lease_expires_at IS NOT NULL)),
	CHECK ((state = 'DELIVERED') = (delivered_at IS NOT NULL))
);
CREATE INDEX matter_codex_runtime_agent_binding_discoveries_claim_idx
	ON matter_codex_runtime_agent_binding_discoveries(next_attempt_at, id)
	WHERE state IN ('PENDING', 'LEASED');

-- Фактически созданный bot turn является producer: discovery фиксируется в
-- той же PostgreSQL transaction и не зависит от ответа caller.
-- +goose StatementBegin
CREATE FUNCTION matter_codex_enqueue_runtime_agent_binding_discovery()
RETURNS trigger
LANGUAGE plpgsql
SECURITY INVOKER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
	IF length(btrim(NEW.mattermost_post_id)) > 0 THEN
		INSERT INTO public.matter_codex_runtime_agent_binding_discoveries(
			agent_session_turn_id, agent_run_id, source_ref
		) VALUES (NEW.id, NEW.run_id, NEW.mattermost_post_id)
		ON CONFLICT (agent_session_turn_id) DO NOTHING;
	END IF;
	RETURN NEW;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER matter_codex_agent_turn_runtime_binding_discovery
AFTER INSERT ON matter_codex_agent_session_turns
FOR EACH ROW EXECUTE FUNCTION matter_codex_enqueue_runtime_agent_binding_discovery();

INSERT INTO matter_codex_runtime_agent_binding_discoveries(
	agent_session_turn_id, agent_run_id, source_ref
)
SELECT id, run_id, mattermost_post_id
FROM matter_codex_agent_session_turns
WHERE length(btrim(mattermost_post_id)) > 0
  AND status IN ('queued', 'running', 'capacity_retry', 'blocked')
ON CONFLICT (agent_session_turn_id) DO NOTHING;

-- +goose StatementBegin
DO $$
DECLARE
	runtime_role_name text := nullif(trim(current_setting('matter_codex.runtime_role', true)), '');
BEGIN
	REVOKE ALL ON matter_codex_runtime_agent_binding_discoveries FROM PUBLIC;
	REVOKE ALL ON SEQUENCE matter_codex_runtime_agent_binding_discoveries_id_seq FROM PUBLIC;
	IF runtime_role_name IS NOT NULL THEN
		EXECUTE format('grant select, insert, update on matter_codex_runtime_agent_binding_discoveries to %I', runtime_role_name);
		EXECUTE format('grant usage, select on sequence matter_codex_runtime_agent_binding_discoveries_id_seq to %I', runtime_role_name);
	END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
	RAISE EXCEPTION 'migration 000039 is forward-only: runtime binding discovery receipts cannot be removed safely';
END
$$;
-- +goose StatementEnd
