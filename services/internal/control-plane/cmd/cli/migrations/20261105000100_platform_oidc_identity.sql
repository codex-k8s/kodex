-- +goose Up
SET ROLE control_plane_owner;

-- Состояние standard OIDC принадлежит control-plane. Оно не заполняется
-- из browser payload и не заменяет существующие application bindings.
ALTER TABLE control_plane.subjects
    ADD CONSTRAINT subjects_oidc_owner_key UNIQUE (organization_id, id);

CREATE TABLE control_plane.oidc_identity_policies (
    revision bigint PRIMARY KEY CHECK (revision BETWEEN 1 AND 9007199254740991),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    issuer text NOT NULL CHECK (octet_length(issuer) BETWEEN 1 AND 2048),
    digest text NOT NULL CHECK (digest ~ '^[a-f0-9]{64}$'),
    document jsonb NOT NULL CHECK (jsonb_typeof(document) = 'object' AND octet_length(document::text) <= 16384),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id, revision, digest, issuer)
);

CREATE TABLE control_plane.oidc_identity_policy_current (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    revision bigint NOT NULL REFERENCES control_plane.oidc_identity_policies(revision)
);

CREATE SEQUENCE control_plane.oidc_identity_session_revisions
    MINVALUE 1 MAXVALUE 9007199254740991 NO CYCLE;

CREATE TABLE control_plane.oidc_identity_sessions (
    revision bigint PRIMARY KEY CHECK (revision BETWEEN 1 AND 9007199254740991),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    subject_id uuid NOT NULL,
    issuer text NOT NULL CHECK (octet_length(issuer) BETWEEN 1 AND 2048),
    external_subject uuid NOT NULL,
    provider_session_id uuid NOT NULL,
    authenticated_at timestamptz NOT NULL,
    absolute_expires_at timestamptz NOT NULL,
    last_issued_at timestamptz NOT NULL,
    policy_revision bigint NOT NULL,
    policy_digest text NOT NULL CHECK (policy_digest ~ '^[a-f0-9]{64}$'),
    state text NOT NULL DEFAULT 'ACTIVE' CHECK (state IN ('ACTIVE', 'REVOKED')),
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY (organization_id, subject_id)
        REFERENCES control_plane.subjects(organization_id, id),
    FOREIGN KEY (organization_id, policy_revision, policy_digest, issuer)
        REFERENCES control_plane.oidc_identity_policies(organization_id, revision, digest, issuer),
    UNIQUE (issuer, external_subject, provider_session_id, authenticated_at),
    CHECK (absolute_expires_at > authenticated_at AND
           absolute_expires_at <= authenticated_at + interval '12 hours'),
    CHECK (last_issued_at >= authenticated_at AND last_issued_at < absolute_expires_at),
    CHECK ((state = 'ACTIVE' AND revoked_at IS NULL) OR
           (state = 'REVOKED' AND revoked_at IS NOT NULL))
);

CREATE INDEX oidc_identity_sessions_owner ON control_plane.oidc_identity_sessions
    (organization_id, subject_id, revision);

CREATE TABLE control_plane.oidc_identity_observations (
    organization_id uuid NOT NULL,
    subject_id uuid NOT NULL,
    last_issued_at timestamptz NOT NULL,
    groups_digest text NOT NULL CHECK (groups_digest ~ '^[a-f0-9]{64}$'),
    session_revision bigint NOT NULL CHECK (session_revision BETWEEN 1 AND 9007199254740991),
    observed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id, subject_id),
    FOREIGN KEY (organization_id, subject_id)
        REFERENCES control_plane.subjects(organization_id, id)
);

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_oidc_identity_policy() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, control_plane AS $$
BEGIN
    IF TG_TABLE_NAME = 'oidc_identity_policy_current' AND TG_OP = 'UPDATE' THEN
        IF NEW.singleton IS DISTINCT FROM OLD.singleton OR NEW.revision <= OLD.revision THEN
            RAISE EXCEPTION 'OIDC policy revision must advance' USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'OIDC policy history cannot be rewritten' USING ERRCODE = '23514';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER oidc_identity_policy_immutable
    BEFORE UPDATE OR DELETE ON control_plane.oidc_identity_policies
    FOR EACH ROW EXECUTE FUNCTION control_plane.guard_oidc_identity_policy();
CREATE TRIGGER oidc_identity_policy_monotonic
    BEFORE UPDATE OR DELETE ON control_plane.oidc_identity_policy_current
    FOR EACH ROW EXECUTE FUNCTION control_plane.guard_oidc_identity_policy();

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_oidc_identity_session() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, control_plane AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'OIDC session tombstone cannot be deleted' USING ERRCODE = '23514';
    END IF;
    IF (NEW.revision, NEW.organization_id, NEW.subject_id, NEW.issuer,
        NEW.external_subject, NEW.provider_session_id, NEW.authenticated_at,
        NEW.absolute_expires_at, NEW.policy_revision, NEW.policy_digest, NEW.created_at)
       IS DISTINCT FROM
       (OLD.revision, OLD.organization_id, OLD.subject_id, OLD.issuer,
        OLD.external_subject, OLD.provider_session_id, OLD.authenticated_at,
        OLD.absolute_expires_at, OLD.policy_revision, OLD.policy_digest, OLD.created_at)
       OR NEW.last_issued_at < OLD.last_issued_at
       OR (OLD.state = 'REVOKED' AND (NEW.state <> 'REVOKED' OR NEW.revoked_at IS DISTINCT FROM OLD.revoked_at)) THEN
        RAISE EXCEPTION 'OIDC session transition is invalid' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER oidc_identity_session_monotonic
    BEFORE UPDATE OR DELETE ON control_plane.oidc_identity_sessions
    FOR EACH ROW EXECUTE FUNCTION control_plane.guard_oidc_identity_session();

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_oidc_identity_observation() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, control_plane AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'OIDC identity high-watermark cannot be deleted' USING ERRCODE = '23514';
    END IF;
    IF (NEW.organization_id, NEW.subject_id) IS DISTINCT FROM (OLD.organization_id, OLD.subject_id)
       OR NEW.last_issued_at < OLD.last_issued_at OR NEW.session_revision < OLD.session_revision
       OR (NEW.last_issued_at = OLD.last_issued_at AND NEW.groups_digest <> OLD.groups_digest) THEN
        RAISE EXCEPTION 'OIDC identity observation cannot move backwards' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER oidc_identity_observation_monotonic
    BEFORE UPDATE OR DELETE ON control_plane.oidc_identity_observations
    FOR EACH ROW EXECUTE FUNCTION control_plane.guard_oidc_identity_observation();

REVOKE UPDATE, DELETE ON control_plane.oidc_identity_policies FROM control_plane_runtime;
REVOKE DELETE ON control_plane.oidc_identity_policy_current,
    control_plane.oidc_identity_sessions, control_plane.oidc_identity_observations
    FROM control_plane_runtime;
REVOKE UPDATE ON SEQUENCE control_plane.oidc_identity_session_revisions FROM control_plane_runtime;

RESET ROLE;

-- Forward-only: policy history, session revisions, revocations и high-watermarks
-- не удаляются и не понижаются при возврате application image.
-- +goose Down
SELECT 1;
