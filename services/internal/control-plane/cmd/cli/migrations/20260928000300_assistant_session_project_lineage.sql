-- +goose Up
SET ROLE control_plane_owner;

-- RuntimeRevision остаётся неизменяемым снимком. Единственное разрешённое
-- изменение — одноразовая привязка исходно общего снимка к Проекту той же
-- сессии при явном переносе диалога. Все остальные поля остаются защищены.
-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_runtime_revision_project_promotion()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' OR
       (to_jsonb(NEW) - 'project_id') IS DISTINCT FROM (to_jsonb(OLD) - 'project_id') OR
       OLD.project_id IS NOT NULL OR NEW.project_id IS NULL OR
       NOT EXISTS (
           SELECT 1 FROM control_plane.sessions session
           WHERE session.id = NEW.session_id
             AND session.organization_id = NEW.organization_id
             AND session.project_id = NEW.project_id
       ) THEN
        RAISE EXCEPTION 'runtime revision is immutable';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER protect_runtime_revision ON control_plane.runtime_revisions;
CREATE TRIGGER protect_runtime_revision
BEFORE UPDATE OR DELETE ON control_plane.runtime_revisions
FOR EACH ROW WHEN (NOT control_plane.project_purge_authorized())
EXECUTE FUNCTION control_plane.protect_runtime_revision_project_promotion();

-- Binding сохраняет неизменяемую связь с exact turn/run/gate. Разрешена
-- только такая же одноразовая project-привязка по уже перенесённой сессии.
-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_attachment_binding_project_promotion()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' OR
       (to_jsonb(NEW) - 'project_id') IS DISTINCT FROM (to_jsonb(OLD) - 'project_id') OR
       OLD.project_id IS NOT NULL OR NEW.project_id IS NULL OR
       NOT (
           EXISTS (
               SELECT 1 FROM control_plane.runs run
               JOIN control_plane.sessions session ON session.id = run.session_id
               WHERE run.id = NEW.run_id
                 AND session.organization_id = NEW.organization_id
                 AND session.project_id = NEW.project_id
           ) OR EXISTS (
               SELECT 1 FROM control_plane.session_turns turn
               JOIN control_plane.sessions session ON session.id = turn.session_id
               WHERE turn.id IN (NEW.assistant_turn_id, NEW.session_turn_id)
                 AND session.organization_id = NEW.organization_id
                 AND session.project_id = NEW.project_id
           ) OR EXISTS (
               SELECT 1 FROM control_plane.owner_gates gate
               JOIN control_plane.runs run ON run.id = gate.root_run_id
               JOIN control_plane.sessions session ON session.id = run.session_id
               WHERE gate.id = NEW.owner_gate_id
                 AND session.organization_id = NEW.organization_id
                 AND session.project_id = NEW.project_id
           )
       ) THEN
        RAISE EXCEPTION 'attachment binding is immutable';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER protect_attachment_binding ON control_plane.attachment_bindings;
CREATE TRIGGER protect_attachment_binding
BEFORE UPDATE OR DELETE ON control_plane.attachment_bindings
FOR EACH ROW WHEN (NOT control_plane.project_purge_authorized())
EXECUTE FUNCTION control_plane.protect_attachment_binding_project_promotion();

-- Архивная квитанция допускает прежние lifecycle-переходы и одноразовую
-- project-привязку, но её содержимое, объект и исходная сессия неизменяемы.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.protect_session_archive_receipt()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'session archive receipt is immutable';
    END IF;
    IF ROW(NEW.id, NEW.ref, NEW.organization_id, NEW.session_id,
           NEW.provider_account_id, NEW.runtime_revision_id, NEW.codex_session_id,
           NEW.content_generation, NEW.format_version, NEW.source_relative_path,
           NEW.source_sha256, NEW.source_size_bytes, NEW.object_key, NEW.object_version,
           NEW.object_etag, NEW.object_digest, NEW.object_size_bytes, NEW.created_at)
       IS DISTINCT FROM
       ROW(OLD.id, OLD.ref, OLD.organization_id, OLD.session_id,
           OLD.provider_account_id, OLD.runtime_revision_id, OLD.codex_session_id,
           OLD.content_generation, OLD.format_version, OLD.source_relative_path,
           OLD.source_sha256, OLD.source_size_bytes, OLD.object_key, OLD.object_version,
           OLD.object_etag, OLD.object_digest, OLD.object_size_bytes, OLD.created_at) OR
       (NEW.project_id IS DISTINCT FROM OLD.project_id AND NOT (
           OLD.project_id IS NULL AND NEW.project_id IS NOT NULL AND EXISTS (
               SELECT 1 FROM control_plane.sessions session
               WHERE session.id = NEW.session_id
                 AND session.organization_id = NEW.organization_id
                 AND session.project_id = NEW.project_id
           )
       )) THEN
        RAISE EXCEPTION 'session archive receipt identity is immutable';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- Любой уже существующий конфликт с другим Проектом закрыто останавливает
-- backfill: переносить чужие строки или частично исправлять граф запрещено.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM control_plane.runs run
        JOIN control_plane.sessions session ON session.id = run.session_id
        WHERE session.project_id IS NOT NULL
          AND run.project_id IS NOT NULL AND run.project_id <> session.project_id
    ) OR EXISTS (
        SELECT 1 FROM control_plane.run_events event
        JOIN control_plane.runs run ON run.id = event.root_run_id
        JOIN control_plane.sessions session ON session.id = run.session_id
        WHERE session.project_id IS NOT NULL
          AND event.project_id IS NOT NULL AND event.project_id <> session.project_id
    ) OR EXISTS (
        SELECT 1 FROM control_plane.runtime_revisions revision
        JOIN control_plane.sessions session ON session.id = revision.session_id
        WHERE session.project_id IS NOT NULL
          AND revision.project_id IS NOT NULL AND revision.project_id <> session.project_id
    ) OR EXISTS (
        SELECT 1 FROM control_plane.artifacts artifact
        JOIN control_plane.runs run ON run.id = artifact.run_id
        JOIN control_plane.sessions session ON session.id = run.session_id
        WHERE session.project_id IS NOT NULL
          AND artifact.project_id IS NOT NULL AND artifact.project_id <> session.project_id
    ) OR EXISTS (
        SELECT 1 FROM control_plane.interaction_message_receipts receipt
        JOIN control_plane.runs run ON run.id = receipt.root_run_id
        JOIN control_plane.sessions session ON session.id = run.session_id
        WHERE session.project_id IS NOT NULL
          AND receipt.project_id IS NOT NULL AND receipt.project_id <> session.project_id
    ) OR EXISTS (
        SELECT 1 FROM control_plane.session_archives archive
        JOIN control_plane.sessions session ON session.id = archive.session_id
        WHERE session.project_id IS NOT NULL
          AND archive.project_id IS NOT NULL AND archive.project_id <> session.project_id
    ) OR EXISTS (
        SELECT 1 FROM control_plane.session_archive_tasks task
        JOIN control_plane.sessions session ON session.id = task.session_id
        WHERE session.project_id IS NOT NULL
          AND task.project_id IS NOT NULL AND task.project_id <> session.project_id
    ) OR EXISTS (
        SELECT 1 FROM control_plane.session_storage storage
        JOIN control_plane.sessions session ON session.id = storage.session_id
        WHERE session.project_id IS NOT NULL
          AND storage.project_id IS NOT NULL AND storage.project_id <> session.project_id
    ) THEN
        RAISE EXCEPTION 'assistant session project lineage conflicts with another project';
    END IF;
END;
$$;
-- +goose StatementEnd

UPDATE control_plane.runs run
SET project_id = session.project_id
FROM control_plane.sessions session
WHERE run.session_id = session.id
  AND session.project_id IS NOT NULL
  AND run.project_id IS NULL;

UPDATE control_plane.run_events event
SET project_id = session.project_id
FROM control_plane.runs run
JOIN control_plane.sessions session ON session.id = run.session_id
WHERE event.root_run_id = run.id
  AND session.project_id IS NOT NULL
  AND event.project_id IS NULL;

UPDATE control_plane.runtime_revisions revision
SET project_id = session.project_id
FROM control_plane.sessions session
WHERE revision.session_id = session.id
  AND session.project_id IS NOT NULL
  AND revision.project_id IS NULL;

UPDATE control_plane.artifacts artifact
SET project_id = session.project_id
FROM control_plane.runs run
JOIN control_plane.sessions session ON session.id = run.session_id
WHERE artifact.run_id = run.id
  AND session.project_id IS NOT NULL
  AND artifact.project_id IS NULL;

UPDATE control_plane.artifact_download_grants download_grant
SET project_id = artifact.project_id
FROM control_plane.artifacts artifact
WHERE download_grant.artifact_id = artifact.id
  AND artifact.project_id IS NOT NULL
  AND download_grant.project_id IS NULL;

UPDATE control_plane.attachment_bindings binding
SET project_id = session.project_id
FROM control_plane.session_turns turn
JOIN control_plane.sessions session ON session.id = turn.session_id
WHERE session.project_id IS NOT NULL
  AND binding.project_id IS NULL
  AND turn.id IN (binding.assistant_turn_id, binding.session_turn_id);

UPDATE control_plane.attachment_bindings binding
SET project_id = session.project_id
FROM control_plane.runs run
JOIN control_plane.sessions session ON session.id = run.session_id
WHERE session.project_id IS NOT NULL
  AND binding.project_id IS NULL
  AND binding.run_id = run.id;

UPDATE control_plane.interaction_message_receipts receipt
SET project_id = session.project_id
FROM control_plane.runs run
JOIN control_plane.sessions session ON session.id = run.session_id
WHERE receipt.root_run_id = run.id
  AND session.project_id IS NOT NULL
  AND receipt.project_id IS NULL;

UPDATE control_plane.session_archives archive
SET project_id = session.project_id
FROM control_plane.sessions session
WHERE archive.session_id = session.id
  AND session.project_id IS NOT NULL
  AND archive.project_id IS NULL;

UPDATE control_plane.session_archive_tasks task
SET project_id = session.project_id
FROM control_plane.sessions session
WHERE task.session_id = session.id
  AND session.project_id IS NOT NULL
  AND task.project_id IS NULL;

UPDATE control_plane.session_storage storage
SET project_id = session.project_id,
    version = storage.version + 1,
    updated_at = clock_timestamp()
FROM control_plane.sessions session
WHERE storage.session_id = session.id
  AND storage.organization_id = session.organization_id
  AND session.project_id IS NOT NULL
  AND storage.project_id IS NULL;

RESET ROLE;
