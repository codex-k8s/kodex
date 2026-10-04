-- +goose Up
SET ROLE control_plane_owner;

-- Immutable исторические terminal rows не переписываются. Активация требует
-- штатного завершения/reconciliation всех прежних invocation, без ручного cancel.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM control_plane.integration_invocations
               WHERE state IN ('WAITING_APPROVAL','READY','RUNNING','UNKNOWN_OUTCOME')) THEN
        RAISE EXCEPTION 'integration approval policy activation requires no active invocations';
    END IF;
END;
$$;
-- +goose StatementEnd

-- Parser PostgreSQL строит точный expected predecessor, не regexp-приближение.
CREATE TEMPORARY TABLE kodex_expected_approval_constraints (
    risk text, approval_policy text, state text, mailbox_gate_required boolean,
    resource_kind text, grant_version bigint, approval_scope_paths text[], adapter text,
    CONSTRAINT expected_adapter CHECK (adapter IN (
        'SYNTHETIC_HTTP', 'GITHUB', 'GITLAB', 'JIRA', 'CONFLUENCE',
        'EMAIL_HTTPS', 'MATTERMOST_INTERACTION', 'HTTPS_JSON_READ', 'OPENAPI_MCP'
    )),
    CONSTRAINT expected_approval CHECK (
        (risk='READ' AND approval_policy='NONE' AND (state<>'WAITING_APPROVAL' OR mailbox_gate_required)) OR
        (risk IN ('WRITE','SENSITIVE','DESTRUCTIVE') AND approval_policy IN ('HUMAN_EACH_EFFECT','HUMAN_SCOPED')) OR
        (risk IN ('WRITE','SENSITIVE','DESTRUCTIVE') AND approval_policy='NONE' AND resource_kind='EMAIL_SENDER'
            AND (state<>'WAITING_APPROVAL' OR mailbox_gate_required))
    ),
    CONSTRAINT expected_pins CHECK (
        (approval_policy <> 'HUMAN_SCOPED' AND grant_version = 0
            AND cardinality(approval_scope_paths) = 0) OR
        (approval_policy = 'HUMAN_SCOPED' AND grant_version > 0
            AND cardinality(approval_scope_paths) BETWEEN 1 AND 16)
    )
) ON COMMIT DROP;

-- +goose StatementBegin
DO $$
DECLARE old_definition text; expected_definition text;
BEGIN
    SELECT pg_get_constraintdef(oid) INTO old_definition FROM pg_constraint
      WHERE conrelid='control_plane.integration_definitions'::regclass AND conname='integration_definitions_adapter_check';
    SELECT pg_get_constraintdef(oid) INTO expected_definition FROM pg_constraint
      WHERE conrelid='kodex_expected_approval_constraints'::regclass AND conname='expected_adapter';
    IF old_definition IS DISTINCT FROM expected_definition THEN
        RAISE EXCEPTION 'integration adapter check predecessor mismatch';
    END IF;
    SELECT pg_get_constraintdef(oid) INTO old_definition FROM pg_constraint
      WHERE conrelid='control_plane.integration_invocations'::regclass AND conname='integration_invocations_approval_check';
    SELECT pg_get_constraintdef(oid) INTO expected_definition FROM pg_constraint
      WHERE conrelid='kodex_expected_approval_constraints'::regclass AND conname='expected_approval';
    IF old_definition IS DISTINCT FROM expected_definition THEN
        RAISE EXCEPTION 'integration approval check predecessor mismatch';
    END IF;
    SELECT pg_get_constraintdef(oid) INTO old_definition FROM pg_constraint
      WHERE conrelid='control_plane.integration_invocations'::regclass AND conname='integration_invocations_approval_pins_check';
    SELECT pg_get_constraintdef(oid) INTO expected_definition FROM pg_constraint
      WHERE conrelid='kodex_expected_approval_constraints'::regclass AND conname='expected_pins';
    IF old_definition IS DISTINCT FROM expected_definition THEN
        RAISE EXCEPTION 'integration approval pins predecessor mismatch';
    END IF;
    SELECT btrim(prosrc) INTO old_definition FROM pg_proc
      WHERE oid='control_plane.protect_email_mailbox_gate()'::regprocedure;
    IF old_definition IS DISTINCT FROM btrim($previous$
BEGIN
    IF TG_OP='UPDATE' AND (NEW.mailbox_gate_required IS DISTINCT FROM OLD.mailbox_gate_required
        OR NEW.approval_policy IS DISTINCT FROM OLD.approval_policy) THEN
        RAISE EXCEPTION 'mailbox approval policy is immutable';
    END IF;
    IF (NEW.mailbox_gate_required OR (NEW.risk<>'READ' AND NEW.approval_policy='NONE')) AND NOT EXISTS (
        SELECT 1 FROM control_plane.integration_connections c
        WHERE c.id=NEW.connection_id AND c.organization_id=NEW.organization_id AND c.definition_key='email'
    ) THEN RAISE EXCEPTION 'mailbox approval requires email owner connection'; END IF;
    RETURN NEW;
END;
$previous$) THEN
        RAISE EXCEPTION 'integration approval owner guard predecessor mismatch';
    END IF;
END;
$$;
-- +goose StatementEnd

ALTER TABLE control_plane.integration_definitions
    DROP CONSTRAINT integration_definitions_adapter_check,
    ADD CONSTRAINT integration_definitions_adapter_check CHECK (adapter IN (
        'SYNTHETIC_HTTP', 'GITHUB', 'GITLAB', 'JIRA', 'CONFLUENCE',
        'EMAIL_HTTPS', 'MATTERMOST_INTERACTION', 'HTTPS_JSON_READ', 'OPENAPI_MCP', 'CONTEXT7'
    ));

ALTER TABLE control_plane.integration_invocations
    DROP CONSTRAINT integration_invocations_approval_check,
    DROP CONSTRAINT integration_invocations_approval_pins_check,
    ADD CONSTRAINT integration_invocations_approval_check CHECK (
        (risk='READ' AND approval_policy='NONE' AND (state<>'WAITING_APPROVAL' OR mailbox_gate_required)) OR
        (risk IN ('WRITE','SENSITIVE','DESTRUCTIVE') AND approval_policy IN ('HUMAN_EACH_EFFECT','HUMAN_SCOPED')) OR
        (risk IN ('WRITE','SENSITIVE','DESTRUCTIVE') AND approval_policy='NONE' AND resource_kind='EMAIL_SENDER'
            AND (state<>'WAITING_APPROVAL' OR mailbox_gate_required)) OR
        (risk='WRITE' AND approval_policy='NONE' AND resource_kind='GITHUB_REPOSITORY' AND operation=capability_key
         AND operation IN ('github.issue.comment.create','github.issue.comment.update','github.pull_request.create',
                           'github.pull_request.update','github.pull_request.review.create')
         AND NOT mailbox_gate_required AND state<>'WAITING_APPROVAL')
    ),
    ADD CONSTRAINT integration_invocations_approval_pins_check CHECK (
        (grant_version>0 AND ((approval_policy<>'HUMAN_SCOPED' AND cardinality(approval_scope_paths)=0) OR
                            (approval_policy='HUMAN_SCOPED' AND cardinality(approval_scope_paths) BETWEEN 1 AND 16))) OR
        (grant_version=0 AND approval_policy<>'HUMAN_SCOPED' AND cardinality(approval_scope_paths)=0
         AND state IN ('SUCCEEDED','FAILED','REJECTED','CANCELLED'))
    );

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.protect_email_mailbox_gate() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' AND (NEW.mailbox_gate_required IS DISTINCT FROM OLD.mailbox_gate_required
        OR NEW.approval_policy IS DISTINCT FROM OLD.approval_policy
        OR NEW.grant_version IS DISTINCT FROM OLD.grant_version
        OR NEW.approval_scope_paths IS DISTINCT FROM OLD.approval_scope_paths) THEN
        RAISE EXCEPTION 'integration approval binding is immutable';
    END IF;
    IF NEW.mailbox_gate_required OR (NEW.risk<>'READ' AND NEW.approval_policy='NONE' AND NEW.resource_kind='EMAIL_SENDER') THEN
        IF NOT EXISTS (SELECT 1 FROM control_plane.integration_connections c
                       WHERE c.id=NEW.connection_id AND c.organization_id=NEW.organization_id AND c.definition_key='email') THEN
            RAISE EXCEPTION 'mailbox approval requires email owner connection';
        END IF;
    ELSIF NEW.risk<>'READ' AND NEW.approval_policy='NONE' THEN
        IF NEW.risk<>'WRITE' OR NEW.resource_kind<>'GITHUB_REPOSITORY' OR NEW.operation<>NEW.capability_key OR
           NEW.operation NOT IN ('github.issue.comment.create','github.issue.comment.update','github.pull_request.create',
                                 'github.pull_request.update','github.pull_request.review.create') OR
           NOT EXISTS (SELECT 1 FROM control_plane.integration_connections c
                       WHERE c.id=NEW.connection_id AND c.organization_id=NEW.organization_id AND c.definition_key='github') OR
           (NEW.operation='github.pull_request.review.create' AND NEW.bounded_input->>'event' IS DISTINCT FROM 'COMMENT') THEN
            RAISE EXCEPTION 'autonomous integration write is not allowed';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'integration approval policy migration is forward-only'; END $$;
-- +goose StatementEnd
