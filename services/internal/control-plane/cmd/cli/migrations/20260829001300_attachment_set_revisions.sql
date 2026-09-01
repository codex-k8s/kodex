-- +goose Up
SET ROLE control_plane_owner;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM control_plane.attachment_sets LIMIT 1) THEN
        RAISE EXCEPTION 'attachment set v1 rows must not exist before prototype schema replacement';
    END IF;
END;
$$;
-- +goose StatementEnd

ALTER TABLE control_plane.owner_gates DROP COLUMN resolution_attachment_set_id;
ALTER TABLE control_plane.session_turns DROP COLUMN attachment_set_id;
ALTER TABLE control_plane.runs DROP COLUMN input_attachment_set_id;

DROP TRIGGER protect_attachment_binding ON control_plane.attachment_bindings;
DROP TRIGGER protect_attachment_set_item ON control_plane.attachment_set_items;
DROP TRIGGER protect_attachment_set ON control_plane.attachment_sets;
DROP TABLE control_plane.attachment_bindings;
DROP TABLE control_plane.attachment_set_items;
DROP TABLE control_plane.attachment_sets;

ALTER TABLE control_plane.runs DROP COLUMN input_artifact_refs;
ALTER TABLE control_plane.session_turns DROP COLUMN artifact_refs;

CREATE TABLE control_plane.attachment_sets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^aset_[A-Za-z0-9_-]{8,89}$'),
    family_ref text NOT NULL CHECK (family_ref ~ '^asetf_[A-Za-z0-9_-]{8,88}$'),
    revision bigint NOT NULL CHECK (revision > 0),
    version bigint NOT NULL CHECK (version = revision),
    previous_revision_id uuid REFERENCES control_plane.attachment_sets(id),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid REFERENCES control_plane.projects(id),
    state text NOT NULL CHECK (state IN ('DRAFT', 'FINALIZED')),
    source text NOT NULL CHECK (source IN ('CONTROL_CENTER', 'SYSTEM_ASSISTANT', 'RUNTIME')),
    purpose text NOT NULL CHECK (purpose IN (
        'ASSISTANT_MESSAGE', 'SESSION_TURN', 'RUN_INPUT', 'WORKFLOW_INPUT',
        'OWNER_GATE_MESSAGE'
    )),
    manifest jsonb,
    manifest_digest text,
    item_count bigint NOT NULL CHECK (item_count >= 0),
    total_size_bytes bigint NOT NULL CHECK (total_size_bytes BETWEEN 0 AND 536870912),
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    finalized_at timestamptz,
    UNIQUE (family_ref, revision),
    CHECK (
        (state = 'DRAFT' AND manifest IS NULL AND manifest_digest IS NULL AND finalized_at IS NULL) OR
        (state = 'FINALIZED' AND item_count > 0 AND jsonb_typeof(manifest) = 'object' AND
         octet_length(manifest::text) <= 1048576 AND manifest_digest ~ '^[a-f0-9]{64}$' AND
         finalized_at IS NOT NULL)
    )
);

CREATE INDEX attachment_sets_project_recent
    ON control_plane.attachment_sets (project_id, created_at DESC, ref);
CREATE INDEX attachment_sets_family_latest
    ON control_plane.attachment_sets (family_ref, revision DESC);

CREATE TABLE control_plane.attachment_set_items (
    attachment_set_id uuid NOT NULL REFERENCES control_plane.attachment_sets(id),
    position bigint NOT NULL CHECK (position > 0),
    artifact_id uuid NOT NULL REFERENCES control_plane.artifacts(id),
    artifact_ref text NOT NULL CHECK (artifact_ref ~ '^art_[A-Za-z0-9_-]{8,89}$'),
    artifact_revision bigint NOT NULL CHECK (artifact_revision > 0),
    artifact_version bigint NOT NULL CHECK (artifact_version > 0),
    file_name text NOT NULL CHECK (char_length(file_name) BETWEEN 1 AND 255),
    media_type text NOT NULL CHECK (char_length(media_type) BETWEEN 1 AND 255),
    size_bytes bigint NOT NULL CHECK (size_bytes BETWEEN 0 AND 536870912),
    digest text NOT NULL CHECK (digest ~ '^sha256:[a-f0-9]{64}$'),
    source text NOT NULL CHECK (source IN (
        'CONTROL_CENTER', 'AGENT_RESULT', 'INTEGRATION_RESULT',
        'KNOWLEDGE_SOURCE', 'INTERACTION_ATTACHMENT'
    )),
    PRIMARY KEY (attachment_set_id, position),
    UNIQUE (attachment_set_id, artifact_id)
);

CREATE INDEX attachment_set_items_artifact_version
    ON control_plane.attachment_set_items (artifact_id, artifact_version, attachment_set_id);

CREATE TABLE control_plane.attachment_bindings (
    ref text PRIMARY KEY CHECK (ref ~ '^abnd_[A-Za-z0-9_-]{8,89}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid REFERENCES control_plane.projects(id),
    attachment_set_id uuid NOT NULL REFERENCES control_plane.attachment_sets(id),
    kind text NOT NULL CHECK (kind IN (
        'ASSISTANT_MESSAGE', 'SESSION_TURN', 'RUN_INPUT', 'WORKFLOW_INPUT',
        'OWNER_GATE_MESSAGE'
    )),
    assistant_turn_id uuid REFERENCES control_plane.session_turns(id),
    session_turn_id uuid REFERENCES control_plane.session_turns(id),
    run_id uuid REFERENCES control_plane.runs(id),
    owner_gate_id uuid REFERENCES control_plane.owner_gates(id),
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK (num_nonnulls(assistant_turn_id, session_turn_id, run_id, owner_gate_id) = 1),
    CHECK (
        (kind = 'ASSISTANT_MESSAGE' AND assistant_turn_id IS NOT NULL) OR
        (kind = 'SESSION_TURN' AND session_turn_id IS NOT NULL) OR
        (kind IN ('RUN_INPUT', 'WORKFLOW_INPUT') AND run_id IS NOT NULL) OR
        (kind = 'OWNER_GATE_MESSAGE' AND owner_gate_id IS NOT NULL)
    )
);

CREATE UNIQUE INDEX attachment_bindings_assistant_turn
    ON control_plane.attachment_bindings (assistant_turn_id) WHERE assistant_turn_id IS NOT NULL;
CREATE UNIQUE INDEX attachment_bindings_session_turn
    ON control_plane.attachment_bindings (session_turn_id) WHERE session_turn_id IS NOT NULL;
CREATE UNIQUE INDEX attachment_bindings_run
    ON control_plane.attachment_bindings (run_id) WHERE run_id IS NOT NULL;
CREATE UNIQUE INDEX attachment_bindings_owner_gate
    ON control_plane.attachment_bindings (owner_gate_id) WHERE owner_gate_id IS NOT NULL;

ALTER TABLE control_plane.runs
    ADD COLUMN input_attachment_set_id uuid REFERENCES control_plane.attachment_sets(id);
ALTER TABLE control_plane.session_turns
    ADD COLUMN attachment_set_id uuid REFERENCES control_plane.attachment_sets(id);
ALTER TABLE control_plane.owner_gates
    ADD COLUMN resolution_attachment_set_id uuid REFERENCES control_plane.attachment_sets(id);

CREATE TRIGGER protect_attachment_set
BEFORE UPDATE OR DELETE ON control_plane.attachment_sets
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_immutable_row();

CREATE TRIGGER protect_attachment_set_item
BEFORE UPDATE OR DELETE ON control_plane.attachment_set_items
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_immutable_row();

CREATE TRIGGER protect_attachment_binding
BEFORE UPDATE OR DELETE ON control_plane.attachment_bindings
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_immutable_row();

RESET ROLE;

-- +goose Down
SET ROLE control_plane_owner;

DROP TRIGGER protect_attachment_binding ON control_plane.attachment_bindings;
DROP TRIGGER protect_attachment_set_item ON control_plane.attachment_set_items;
DROP TRIGGER protect_attachment_set ON control_plane.attachment_sets;
ALTER TABLE control_plane.owner_gates DROP COLUMN resolution_attachment_set_id;
ALTER TABLE control_plane.session_turns DROP COLUMN attachment_set_id;
ALTER TABLE control_plane.runs DROP COLUMN input_attachment_set_id;
DROP TABLE control_plane.attachment_bindings;
DROP TABLE control_plane.attachment_set_items;
DROP TABLE control_plane.attachment_sets;

ALTER TABLE control_plane.runs ADD COLUMN input_artifact_refs text[] NOT NULL DEFAULT '{}';
ALTER TABLE control_plane.session_turns ADD COLUMN artifact_refs text[] NOT NULL DEFAULT '{}';

CREATE TABLE control_plane.attachment_sets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^aset_[A-Za-z0-9_-]{8,89}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid NOT NULL REFERENCES control_plane.projects(id),
    context_kind text NOT NULL CHECK (context_kind IN (
        'ASSISTANT_MESSAGE', 'SESSION_TURN', 'RUN_INPUT', 'WORKFLOW_INPUT', 'OWNER_GATE_MESSAGE'
    )),
    manifest jsonb NOT NULL CHECK (jsonb_typeof(manifest) = 'object' AND octet_length(manifest::text) <= 1048576),
    manifest_digest text NOT NULL CHECK (manifest_digest ~ '^[a-f0-9]{64}$'),
    item_count bigint NOT NULL CHECK (item_count > 0),
    total_size_bytes bigint NOT NULL CHECK (total_size_bytes BETWEEN 0 AND 536870912),
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX attachment_sets_project_recent ON control_plane.attachment_sets (project_id, created_at DESC, id);
CREATE TABLE control_plane.attachment_set_items (
    attachment_set_id uuid NOT NULL REFERENCES control_plane.attachment_sets(id),
    position bigint NOT NULL CHECK (position > 0),
    artifact_id uuid NOT NULL REFERENCES control_plane.artifacts(id),
    artifact_ref text NOT NULL CHECK (artifact_ref ~ '^art_[A-Za-z0-9_-]{8,89}$'),
    artifact_revision bigint NOT NULL CHECK (artifact_revision > 0),
    artifact_version bigint NOT NULL CHECK (artifact_version > 0),
    file_name text NOT NULL CHECK (char_length(file_name) BETWEEN 1 AND 255),
    media_type text NOT NULL CHECK (char_length(media_type) BETWEEN 1 AND 255),
    size_bytes bigint NOT NULL CHECK (size_bytes BETWEEN 0 AND 536870912),
    digest text NOT NULL CHECK (digest ~ '^sha256:[a-f0-9]{64}$'),
    PRIMARY KEY (attachment_set_id, position),
    UNIQUE (attachment_set_id, artifact_id)
);
CREATE INDEX attachment_set_items_artifact_version ON control_plane.attachment_set_items (artifact_id, artifact_version, attachment_set_id);
CREATE TABLE control_plane.attachment_bindings (
    ref text PRIMARY KEY CHECK (ref ~ '^abnd_[A-Za-z0-9_-]{8,89}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid NOT NULL REFERENCES control_plane.projects(id),
    attachment_set_id uuid NOT NULL REFERENCES control_plane.attachment_sets(id),
    target_kind text NOT NULL CHECK (target_kind IN (
        'ASSISTANT_MESSAGE', 'SESSION_TURN', 'RUN_INPUT', 'WORKFLOW_INPUT', 'OWNER_GATE_MESSAGE'
    )),
    target_ref text NOT NULL CHECK (char_length(target_ref) BETWEEN 8 AND 96),
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (target_kind, target_ref)
);
ALTER TABLE control_plane.runs ADD COLUMN input_attachment_set_id uuid REFERENCES control_plane.attachment_sets(id);
ALTER TABLE control_plane.session_turns ADD COLUMN attachment_set_id uuid REFERENCES control_plane.attachment_sets(id);
ALTER TABLE control_plane.owner_gates ADD COLUMN resolution_attachment_set_id uuid REFERENCES control_plane.attachment_sets(id);
CREATE TRIGGER protect_attachment_set BEFORE UPDATE OR DELETE ON control_plane.attachment_sets
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_immutable_row();
CREATE TRIGGER protect_attachment_set_item BEFORE UPDATE OR DELETE ON control_plane.attachment_set_items
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_immutable_row();
CREATE TRIGGER protect_attachment_binding BEFORE UPDATE OR DELETE ON control_plane.attachment_bindings
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_immutable_row();

RESET ROLE;
