-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.integration_connection_tests
    ADD COLUMN purpose text NOT NULL DEFAULT 'OWNER_TEST'
        CHECK (purpose IN ('OWNER_TEST', 'MANAGED_MCP_REFRESH')),
    ADD COLUMN predecessor_ref text REFERENCES control_plane.integration_connection_tests(ref),
    ADD COLUMN retry_after timestamptz,
    ADD CONSTRAINT integration_health_refresh_attempt CHECK
        (purpose <> 'MANAGED_MCP_REFRESH' OR attempt BETWEEN 1 AND 3);

-- Только владелец решает, какие подключения требуют реального фонового probe.
-- Recovery относится лишь к последнему собственному health failure с теми же
-- semantic inputs, не к credential/configuration drift или owner disable.
-- +goose StatementBegin
CREATE FUNCTION control_plane.context7_health_refresh_eligible(p_organization uuid, p_connection uuid)
RETURNS boolean LANGUAGE sql STABLE SET search_path = pg_catalog, control_plane AS $$
SELECT EXISTS (
    SELECT 1 FROM control_plane.integration_connections c
    JOIN control_plane.integration_definitions d ON d.stable_key=c.definition_key
    JOIN control_plane.integration_credential_revisions cr
      ON cr.id=c.credential_revision_id AND cr.organization_id=c.organization_id
    WHERE c.id=$2 AND c.organization_id=$1 AND c.lifecycle_state='ACTIVE'
      AND c.enabled AND c.definition_key='context7' AND c.masked_credentials_state='CONFIGURED'
      AND d.enabled AND d.adapter_owner='integration-gateway' AND d.execution_route='MANAGED_MCP'
      AND d.adapter_readiness='READY' AND d.definition_version=c.definition_version AND d.digest=c.definition_digest
      AND EXISTS (
        SELECT 1 FROM control_plane.integration_grants resolve
        JOIN control_plane.integration_grants query
          ON query.connection_id=resolve.connection_id AND query.organization_id=resolve.organization_id
         AND query.target_kind=resolve.target_kind AND query.target_ref=resolve.target_ref
        JOIN control_plane.agents a ON a.ref=resolve.target_ref AND a.organization_id=c.organization_id AND a.enabled
        WHERE resolve.connection_id=c.id AND resolve.organization_id=c.organization_id
          AND resolve.target_kind='AGENT' AND resolve.enabled AND query.enabled
          AND resolve.capability_key='context7.library.resolve' AND query.capability_key='context7.docs.query'
          AND resolve.risk='READ' AND query.risk='READ'
          AND resolve.approval_policy='NONE' AND query.approval_policy='NONE'
          AND resolve.definition_version=c.definition_version AND query.definition_version=c.definition_version
          AND resolve.definition_digest=c.definition_digest AND query.definition_digest=c.definition_digest
      )
      AND (c.state='CONNECTED' OR (c.state='DEGRADED' AND EXISTS (
        SELECT 1 FROM control_plane.integration_connection_tests failed
        WHERE failed.id=(SELECT latest.id FROM control_plane.integration_connection_tests latest
                        WHERE latest.connection_id=c.id AND latest.completed_at IS NOT NULL
                        ORDER BY latest.completed_at DESC, latest.ref DESC LIMIT 1)
          AND failed.purpose='MANAGED_MCP_REFRESH' AND failed.state='FAILED'
          AND failed.claimed_workload='integration-gateway' AND failed.generation>0
          AND failed.input_snapshot->'configuration'=c.public_configuration
          AND failed.input_snapshot->>'definitionKey'=c.definition_key
          AND failed.input_snapshot->>'definitionVersion'=c.definition_version
          AND failed.input_snapshot->>'definitionDigest'=c.definition_digest
          AND failed.input_snapshot->>'credentialRevisionRef'=cr.ref
          AND (failed.input_snapshot->>'credentialRevision')::bigint=cr.revision
          AND failed.input_snapshot->>'credentialSHA256'=cr.content_sha256
      )))
);
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.context7_health_refresh_eligible(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.context7_health_refresh_eligible(uuid,uuid) TO control_plane_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.managed_mcp_health_origin() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' AND (NEW.purpose IS DISTINCT FROM OLD.purpose OR
                          NEW.predecessor_ref IS DISTINCT FROM OLD.predecessor_ref OR
                          (OLD.purpose='MANAGED_MCP_REFRESH' AND
                           (NEW.created_at IS DISTINCT FROM OLD.created_at OR NEW.attempt IS DISTINCT FROM OLD.attempt))) THEN
        RAISE EXCEPTION 'health task origin is immutable';
    END IF;
    IF TG_OP='INSERT' AND NEW.purpose='MANAGED_MCP_REFRESH' AND
       NOT control_plane.context7_health_refresh_eligible(NEW.organization_id, NEW.connection_id) THEN
        RAISE EXCEPTION 'managed MCP health refresh is not eligible';
    END IF;
    IF TG_OP='INSERT' AND NEW.predecessor_ref IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM control_plane.integration_connection_tests predecessor
        WHERE predecessor.ref=NEW.predecessor_ref
          AND predecessor.organization_id=NEW.organization_id AND predecessor.connection_id=NEW.connection_id
          AND predecessor.state IN ('SUCCEEDED','FAILED','CANCELLED')
          AND (NEW.attempt=1 OR (predecessor.purpose='MANAGED_MCP_REFRESH'
               AND predecessor.state='FAILED' AND NEW.attempt=predecessor.attempt+1))
    ) THEN
        RAISE EXCEPTION 'health predecessor binding is invalid';
    END IF;
    IF TG_OP='INSERT' AND NEW.purpose='MANAGED_MCP_REFRESH' AND NEW.attempt>1 AND NEW.predecessor_ref IS NULL THEN
        RAISE EXCEPTION 'health retry requires predecessor';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER managed_mcp_health_origin BEFORE INSERT OR UPDATE
ON control_plane.integration_connection_tests FOR EACH ROW
EXECUTE FUNCTION control_plane.managed_mcp_health_origin();

RESET ROLE;
-- +goose Down
SELECT 1;
