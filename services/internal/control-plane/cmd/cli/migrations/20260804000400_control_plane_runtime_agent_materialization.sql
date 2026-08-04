-- +goose Up
CREATE UNIQUE INDEX runtime_agent_session_owner_unique
	ON control_plane.resources (
		organization_id,
		project_id,
		(spec ->> 'agentSessionKey'),
		((spec ->> 'agentSessionId')::bigint)
	)
	WHERE kind = 'SESSION'
	  AND state = 'ACTIVE'
	  AND length(spec ->> 'agentSessionKey') > 0
	  AND (spec ->> 'agentSessionId') ~ '^[1-9][0-9]*$';

CREATE UNIQUE INDEX runtime_agent_turn_source_unique
	ON control_plane.resources (
		organization_id,
		project_id,
		owner_actor_id,
		(spec ->> 'sourceRef')
	)
	WHERE kind = 'TURN';

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
	RAISE EXCEPTION 'migration 20260804000400 is forward-only: authoritative runtime agent mappings cannot be removed safely';
END
$$;
-- +goose StatementEnd
