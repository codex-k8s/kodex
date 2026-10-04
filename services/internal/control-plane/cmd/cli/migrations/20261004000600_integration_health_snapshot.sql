-- +goose Up
SET ROLE control_plane_owner;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM control_plane.integration_connection_tests
               WHERE state IN ('DUE', 'CLAIMED')) THEN
        RAISE EXCEPTION 'health snapshot activation requires no active tests';
    END IF;
END;
$$;
-- +goose StatementEnd

-- Новая проверка фиксирует input при создании; terminal history не становится
-- доказательством readiness задним числом.
ALTER TABLE control_plane.integration_connection_tests
    ADD COLUMN input_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb
    CHECK (jsonb_typeof(input_snapshot) = 'object' AND octet_length(input_snapshot::text) <= 524288);

-- +goose StatementBegin
CREATE FUNCTION control_plane.capture_integration_health_input() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE snapshot jsonb;
BEGIN
    SELECT jsonb_build_object(
        'connectionRef', c.ref, 'connectionVersion', c.version,
        'configuration', c.public_configuration,
        'definitionKey', c.definition_key, 'definitionVersion', c.definition_version,
        'definitionDigest', c.definition_digest,
        'credentialRevisionRef', COALESCE(cr.ref, ''),
        'credentialRevision', COALESCE(cr.revision, 0),
        'credentialSHA256', COALESCE(cr.content_sha256, '')
    ) INTO snapshot
    FROM control_plane.integration_connections c
    LEFT JOIN control_plane.integration_credential_revisions cr
      ON cr.id = c.credential_revision_id AND cr.organization_id = c.organization_id
    WHERE c.id = NEW.connection_id AND c.organization_id = NEW.organization_id;
    IF TG_OP = 'INSERT' THEN
        IF NEW.input_snapshot <> '{}'::jsonb OR snapshot IS NULL THEN
            RAISE EXCEPTION 'health snapshot must be assigned by owner';
        END IF;
        NEW.input_snapshot := snapshot;
    ELSE
        IF NEW.input_snapshot IS DISTINCT FROM OLD.input_snapshot THEN
            RAISE EXCEPTION 'health input snapshot is immutable';
        END IF;
        IF NEW.state IN ('CLAIMED', 'SUCCEEDED') AND
           NEW.input_snapshot IS DISTINCT FROM snapshot THEN
            RAISE EXCEPTION 'health input snapshot is stale';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER integration_health_input
BEFORE INSERT OR UPDATE ON control_plane.integration_connection_tests
FOR EACH ROW EXECUTE FUNCTION control_plane.capture_integration_health_input();

RESET ROLE;

-- +goose Down
SELECT 1;
