-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.assistant_conversations
    ADD COLUMN deleted_at timestamptz,
    ADD COLUMN purge_after timestamptz;

UPDATE control_plane.assistant_conversations
SET deleted_at = updated_at,
    purge_after = updated_at + interval '30 days'
WHERE state = 'ARCHIVED';

ALTER TABLE control_plane.assistant_conversations
    ADD CONSTRAINT assistant_conversations_trash_lifecycle_check CHECK (
        (state = 'ARCHIVED' AND deleted_at IS NOT NULL AND purge_after IS NOT NULL)
        OR (state <> 'ARCHIVED' AND deleted_at IS NULL AND purge_after IS NULL)
    );

CREATE INDEX assistant_conversation_trash_due
    ON control_plane.assistant_conversations(purge_after, id)
    WHERE state = 'ARCHIVED';

-- Квитанция переживает удаление диалога и не сохраняет его название,
-- сообщения, планы или содержимое вложений.
CREATE TABLE control_plane.assistant_conversation_purge_receipts (
    conversation_ref text PRIMARY KEY,
    organization_id uuid NOT NULL,
    created_by uuid NOT NULL,
    purged_by uuid NOT NULL,
    reason text NOT NULL CHECK (reason IN ('OWNER_REQUEST', 'RETENTION')),
    purged_at timestamptz NOT NULL DEFAULT statement_timestamp(),
    redacted_turns integer NOT NULL CHECK (redacted_turns >= 0),
    deleted_plans integer NOT NULL CHECK (deleted_plans >= 0)
);

GRANT SELECT, INSERT ON control_plane.assistant_conversation_purge_receipts
    TO control_plane_runtime;

CREATE TABLE control_plane.assistant_conversation_purge_context (
    transaction_id xid8 PRIMARY KEY,
    backend_pid integer NOT NULL,
    conversation_ref text NOT NULL
);
REVOKE ALL ON control_plane.assistant_conversation_purge_context FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION control_plane.assistant_conversation_purge_authorized()
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog
AS $$
    SELECT EXISTS (
        SELECT 1
        FROM control_plane.assistant_conversation_purge_context context
        WHERE context.transaction_id = pg_current_xact_id_if_assigned()
          AND context.backend_pid = pg_backend_pid()
    );
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION control_plane.assistant_conversation_purge_authorized() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.assistant_conversation_purge_authorized()
    TO control_plane_runtime, control_plane_migrator;

DROP TRIGGER protect_assistant_plan_revisions ON control_plane.assistant_plan_revisions;
CREATE TRIGGER protect_assistant_plan_revisions
BEFORE UPDATE OR DELETE ON control_plane.assistant_plan_revisions
FOR EACH ROW WHEN (NOT control_plane.project_purge_authorized()
                   AND NOT control_plane.assistant_conversation_purge_authorized())
EXECUTE FUNCTION control_plane.reject_immutable_assistant_record();

DROP TRIGGER protect_assistant_plan_receipts ON control_plane.assistant_plan_receipts;
CREATE TRIGGER protect_assistant_plan_receipts
BEFORE UPDATE OR DELETE ON control_plane.assistant_plan_receipts
FOR EACH ROW WHEN (NOT control_plane.project_purge_authorized()
                   AND NOT control_plane.assistant_conversation_purge_authorized())
EXECUTE FUNCTION control_plane.reject_immutable_assistant_record();

-- Диалог удаляется отдельно от Session: run/audit-каркас остаётся доступен,
-- но пользовательский текст turns и все варианты планов удаляются атомарно.
-- +goose StatementBegin
CREATE FUNCTION control_plane.purge_assistant_conversation(
    p_organization_id uuid,
    p_created_by uuid,
    p_conversation_ref text,
    p_expected_version bigint,
    p_purged_by uuid,
    p_reason text
) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog
AS $$
DECLARE
    v_conversation control_plane.assistant_conversations%ROWTYPE;
    v_transaction_id xid8;
    v_redacted_turns integer;
    v_deleted_plans integer;
BEGIN
    IF p_reason NOT IN ('OWNER_REQUEST', 'RETENTION') THEN
        RAISE EXCEPTION 'assistant conversation purge reason is invalid';
    END IF;
    SELECT * INTO v_conversation
    FROM control_plane.assistant_conversations
    WHERE organization_id = p_organization_id
      AND created_by = p_created_by
      AND ref = p_conversation_ref
    FOR UPDATE;
    IF NOT FOUND THEN
        RETURN false;
    END IF;
    IF v_conversation.state <> 'ARCHIVED' OR
       v_conversation.version <> p_expected_version THEN
        RETURN false;
    END IF;

    v_transaction_id := pg_current_xact_id();
    INSERT INTO control_plane.assistant_conversation_purge_context(
        transaction_id, backend_pid, conversation_ref
    ) VALUES (v_transaction_id, pg_backend_pid(), p_conversation_ref);

    UPDATE control_plane.session_turns
    SET content = '[deleted]', actor_ref = 'purged', attachment_set_id = NULL
    WHERE organization_id = p_organization_id
      AND session_id = v_conversation.session_id;
    GET DIAGNOSTICS v_redacted_turns = ROW_COUNT;

    UPDATE control_plane.assistant_conversations
    SET latest_plan_id = NULL
    WHERE id = v_conversation.id;

    DELETE FROM control_plane.assistant_plan_receipts receipt
    USING control_plane.assistant_plans plan
    WHERE receipt.plan_id = plan.id
      AND plan.organization_id = p_organization_id
      AND plan.conversation_ref = p_conversation_ref;
    DELETE FROM control_plane.assistant_plan_revisions revision
    USING control_plane.assistant_plans plan
    WHERE revision.plan_id = plan.id
      AND plan.organization_id = p_organization_id
      AND plan.conversation_ref = p_conversation_ref;
    DELETE FROM control_plane.assistant_plans
    WHERE organization_id = p_organization_id
      AND conversation_ref = p_conversation_ref;
    GET DIAGNOSTICS v_deleted_plans = ROW_COUNT;

    DELETE FROM control_plane.assistant_conversations
    WHERE id = v_conversation.id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'assistant conversation purge target disappeared';
    END IF;

    INSERT INTO control_plane.assistant_conversation_purge_receipts(
        conversation_ref, organization_id, created_by, purged_by, reason,
        redacted_turns, deleted_plans
    ) VALUES (
        p_conversation_ref, p_organization_id, p_created_by, p_purged_by,
        p_reason, v_redacted_turns, v_deleted_plans
    ) ON CONFLICT (conversation_ref) DO NOTHING;

    DELETE FROM control_plane.assistant_conversation_purge_context
    WHERE transaction_id = v_transaction_id;
    RETURN true;
END;
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION control_plane.purge_assistant_conversation(
    uuid, uuid, text, bigint, uuid, text
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.purge_assistant_conversation(
    uuid, uuid, text, bigint, uuid, text
) TO control_plane_runtime;

RESET ROLE;

-- +goose Down
SET ROLE control_plane_owner;

DROP FUNCTION control_plane.purge_assistant_conversation(uuid, uuid, text, bigint, uuid, text);

DROP TRIGGER protect_assistant_plan_receipts ON control_plane.assistant_plan_receipts;
CREATE TRIGGER protect_assistant_plan_receipts
BEFORE UPDATE OR DELETE ON control_plane.assistant_plan_receipts
FOR EACH ROW WHEN (NOT control_plane.project_purge_authorized())
EXECUTE FUNCTION control_plane.reject_immutable_assistant_record();

DROP TRIGGER protect_assistant_plan_revisions ON control_plane.assistant_plan_revisions;
CREATE TRIGGER protect_assistant_plan_revisions
BEFORE UPDATE OR DELETE ON control_plane.assistant_plan_revisions
FOR EACH ROW WHEN (NOT control_plane.project_purge_authorized())
EXECUTE FUNCTION control_plane.reject_immutable_assistant_record();

DROP FUNCTION control_plane.assistant_conversation_purge_authorized();
DROP TABLE control_plane.assistant_conversation_purge_context;
DROP TABLE control_plane.assistant_conversation_purge_receipts;
DROP INDEX control_plane.assistant_conversation_trash_due;
ALTER TABLE control_plane.assistant_conversations
    DROP CONSTRAINT assistant_conversations_trash_lifecycle_check,
    DROP COLUMN purge_after,
    DROP COLUMN deleted_at;

RESET ROLE;
