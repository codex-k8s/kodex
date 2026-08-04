-- +goose Up
CREATE TABLE interaction_gateway_download_grants (
    id uuid PRIMARY KEY,
    generation bigint NOT NULL CHECK (generation > 0),
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    actor_id uuid NOT NULL,
    mattermost_user_id text NOT NULL CHECK (length(mattermost_user_id) BETWEEN 1 AND 64),
    team_id text NOT NULL CHECK (length(team_id) BETWEEN 1 AND 64),
    channel_id text NOT NULL CHECK (length(channel_id) BETWEEN 1 AND 64),
    session_id uuid NOT NULL,
    turn_id uuid NOT NULL,
    artifact jsonb NOT NULL,
    issued_payload_sha256 text NOT NULL CHECK (issued_payload_sha256 ~ '^[0-9a-f]{64}$'),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    revoked_at timestamptz,
    authenticated_user_id text NOT NULL DEFAULT '' CHECK (length(authenticated_user_id) <= 64),
    authenticated_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK ((consumed_at IS NULL) = (authenticated_user_id = '')),
    CHECK ((consumed_at IS NULL) = (authenticated_at IS NULL))
);
ALTER TABLE interaction_gateway_deliveries
    ADD COLUMN owner_delivery_fence bigint,
    ADD COLUMN owner_delivery_token_ciphertext bytea,
    ADD COLUMN owner_delivery_expires_at timestamptz,
    ADD COLUMN owner_turn_version bigint,
    ADD COLUMN owner_runtime_revision_id uuid,
    ADD COLUMN owner_runtime_revision_version bigint,
    ADD COLUMN owner_delivery_recorded_at timestamptz;
ALTER TABLE interaction_gateway_download_grants ENABLE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_download_grants FORCE ROW LEVEL SECURITY;
CREATE POLICY interaction_gateway_download_grant_runtime_scope ON interaction_gateway_download_grants
USING ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()))
WITH CHECK ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()));
GRANT SELECT, INSERT, UPDATE ON interaction_gateway_download_grants TO interaction_gateway_runtime;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION interaction_gateway_download_grant_scope(requested_grant_id uuid)
RETURNS TABLE (organization_id uuid, project_id uuid)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $function$
BEGIN
    IF NOT pg_has_role(session_user, 'interaction_gateway_runtime', 'member') THEN
        RAISE EXCEPTION 'download grant scope lookup is forbidden' USING ERRCODE = '28000';
    END IF;
    RETURN QUERY
    SELECT grant_row.organization_id, grant_row.project_id
      FROM interaction_gateway_download_grants AS grant_row
     WHERE grant_row.id = requested_grant_id;
END
$function$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION interaction_gateway_download_grant_scope(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION interaction_gateway_download_grant_scope(uuid) TO interaction_gateway_runtime;

-- +goose Down
-- Forward-only: download audit/consumption и credential fences не удаляются.
SELECT 1;
