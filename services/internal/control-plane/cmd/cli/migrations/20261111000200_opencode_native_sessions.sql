-- +goose Up
SET ROLE control_plane_owner;
SET LOCAL lock_timeout='5s';
LOCK TABLE control_plane.sessions,control_plane.session_storage,control_plane.session_archives IN ACCESS EXCLUSIVE MODE;

-- Переход без legacy decoder выполняется только после отдельной операторской
-- очистки старых сессий. Миграция сама не удаляет пользовательские данные.
-- +goose StatementBegin
DO $native_cutover$
BEGIN
 IF EXISTS (SELECT 1 FROM control_plane.sessions)
    OR EXISTS (SELECT 1 FROM control_plane.session_storage)
    OR EXISTS (SELECT 1 FROM control_plane.session_archives) THEN
   RAISE EXCEPTION 'OpenCode-only cutover requires an explicitly authorized session-data purge first';
 END IF;
END $native_cutover$;
-- +goose StatementEnd

-- Закрытая metadata-проекция повторяет native contract. Transcript хранится
-- в официальном export, а не в JSON-поле этой таблицы.
-- +goose StatementBegin
CREATE FUNCTION control_plane.native_checkpoint_shape(p_value jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE SET search_path=pg_catalog AS $$
 SELECT CASE WHEN jsonb_typeof(p_value)='object' THEN COALESCE(
   NOT EXISTS (SELECT 1 FROM jsonb_object_keys(p_value) key WHERE key NOT IN
     ('session_id','version','generation','relative_path','sha256','size_bytes','workspace_ref','workspace_generation'))
   AND jsonb_typeof(p_value->'session_id')='string' AND p_value->>'session_id' ~ '^ses_[A-Za-z0-9]{1,100}$'
   AND jsonb_typeof(p_value->'version')='string' AND p_value->>'version' ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
   AND jsonb_typeof(p_value->'generation')='number'
   AND (p_value->>'generation')::bigint BETWEEN 1 AND 9007199254740990
   AND jsonb_typeof(p_value->'relative_path')='string' AND p_value->>'relative_path'='.kodex/state/opencode/checkpoints/g'||(p_value->>'generation')||'/'||(p_value->>'session_id')||'.json'
   AND jsonb_typeof(p_value->'sha256')='string' AND p_value->>'sha256' ~ '^[a-f0-9]{64}$'
   AND jsonb_typeof(p_value->'size_bytes')='number'
   AND (p_value->>'size_bytes')::bigint BETWEEN 1 AND 67108864
   AND ((NOT p_value ? 'workspace_ref' AND NOT p_value ? 'workspace_generation')
     OR (jsonb_typeof(p_value->'workspace_ref')='string' AND p_value->>'workspace_ref' ~ '^wks_[A-Za-z0-9_-]{8,89}$'
       AND jsonb_typeof(p_value->'workspace_generation')='number'
       AND (p_value->>'workspace_generation')::bigint>0)),false)
 ELSE false END;
$$;
-- +goose StatementEnd

ALTER TABLE control_plane.session_storage RENAME COLUMN codex_session_id TO native_session_id;
ALTER TABLE control_plane.session_storage
 ALTER COLUMN native_session_id TYPE text USING native_session_id::text,
 ALTER COLUMN native_session_id DROP NOT NULL,
 ALTER COLUMN source_relative_path DROP NOT NULL,
 ALTER COLUMN source_sha256 DROP NOT NULL,
 ALTER COLUMN source_size_bytes DROP NOT NULL,
 DROP CONSTRAINT session_storage_source_relative_path_check,
 DROP CONSTRAINT session_storage_content_generation_check,
 DROP CONSTRAINT session_storage_state_check,
 ADD COLUMN native_checkpoint jsonb,
 ADD COLUMN native_writer_lease_ref text REFERENCES control_plane.runtime_leases(ref) DEFERRABLE INITIALLY DEFERRED,
 ADD COLUMN native_writer_revision_id uuid REFERENCES control_plane.runtime_revisions(id) DEFERRABLE INITIALLY DEFERRED,
 ADD CONSTRAINT session_storage_content_generation_check CHECK(content_generation>=0),
 ADD CONSTRAINT session_storage_state_check CHECK(state IN('INITIALIZING','LIVE','SNAPSHOT_READY','SNAPSHOTTING',
   'DELETE_PVC_READY','ARCHIVED','RESTORE_READY','RESTORING','ERROR','PURGED')),
 ADD CONSTRAINT session_storage_native_writer CHECK((native_writer_lease_ref IS NULL)=(native_writer_revision_id IS NULL)),
 ADD CONSTRAINT session_storage_native_writer_state CHECK(native_writer_lease_ref IS NULL OR state IN('INITIALIZING','LIVE','ERROR')),
 ADD CONSTRAINT session_storage_native_shape CHECK(
   (state='INITIALIZING' AND content_generation=0 AND native_session_id IS NULL AND native_checkpoint IS NULL
     AND source_relative_path IS NULL AND source_sha256 IS NULL AND source_size_bytes IS NULL
     AND native_writer_lease_ref IS NOT NULL AND current_archive_id IS NULL)
   OR (content_generation>0 AND state<>'INITIALIZING' AND native_session_id IS NOT NULL
     AND source_relative_path IS NOT NULL AND source_sha256 IS NOT NULL AND source_size_bytes IS NOT NULL
     AND native_checkpoint IS NOT NULL AND control_plane.native_checkpoint_shape(native_checkpoint)
     AND native_checkpoint->>'session_id'=native_session_id
     AND (native_checkpoint->>'generation')::bigint=content_generation
     AND native_checkpoint->>'relative_path'=source_relative_path
     AND native_checkpoint->>'sha256'=source_sha256
     AND (native_checkpoint->>'size_bytes')::bigint=source_size_bytes));
CREATE UNIQUE INDEX session_storage_native_identity ON control_plane.session_storage(native_session_id)
 WHERE native_session_id IS NOT NULL;

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_native_session_identity() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF OLD.native_session_id IS NOT NULL AND NEW.native_session_id IS DISTINCT FROM OLD.native_session_id THEN
   RAISE EXCEPTION 'native session identity is immutable; create an explicit new session';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER guard_native_session_identity BEFORE UPDATE ON control_plane.session_storage
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_native_session_identity();

ALTER TABLE control_plane.session_archives RENAME COLUMN codex_session_id TO native_session_id;
ALTER TABLE control_plane.session_archives
 ALTER COLUMN native_session_id TYPE text USING native_session_id::text,
 DROP CONSTRAINT session_archives_source_relative_path_check,
 DROP CONSTRAINT session_archives_format_shape,
 ADD COLUMN native_checkpoint jsonb,
 ADD CONSTRAINT session_archives_format_shape CHECK(
   (format_version=3 AND native_session_id IS NOT NULL AND source_relative_path IS NOT NULL
     AND source_size_bytes BETWEEN 1 AND 67108864 AND workspace_id IS NULL AND workspace_generation IS NULL
     AND workspace_checkpoint_ref IS NULL AND workspace_entries IS NULL AND workspace_quiescence IS NULL
     AND native_checkpoint IS NOT NULL AND control_plane.native_checkpoint_shape(native_checkpoint)
     AND native_checkpoint->>'session_id'=native_session_id
     AND (native_checkpoint->>'generation')::bigint=content_generation
     AND native_checkpoint->>'relative_path'=source_relative_path
     AND native_checkpoint->>'sha256'=source_sha256
     AND (native_checkpoint->>'size_bytes')::bigint=source_size_bytes)
   OR (format_version=2 AND native_session_id IS NULL AND native_checkpoint IS NULL AND source_relative_path IS NULL AND project_id IS NOT NULL
     AND source_size_bytes BETWEEN 0 AND 1073741824 AND workspace_id IS NOT NULL AND workspace_generation IS NOT NULL AND workspace_generation>0
     AND workspace_checkpoint_ref IS NOT NULL AND workspace_checkpoint_ref ~ '^wcp_[A-Za-z0-9_-]{8,89}$'
     AND workspace_entries IS NOT NULL AND workspace_entries BETWEEN 0 AND 100000
     AND (source_size_bytes=0 OR workspace_entries>0) AND workspace_quiescence IS NOT NULL AND octet_length(workspace_quiescence) BETWEEN 1 AND 2048
     AND object_size_bytes<=65536 AND char_length(object_version)>0));

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.protect_session_archive_receipt()
RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'session archive receipt is immutable'; END IF;
 IF ROW(NEW.id,NEW.ref,NEW.organization_id,NEW.session_id,NEW.provider_account_id,
        NEW.runtime_revision_id,NEW.native_session_id,NEW.native_checkpoint,NEW.content_generation,
        NEW.format_version,NEW.source_relative_path,NEW.source_sha256,NEW.source_size_bytes,
        NEW.object_key,NEW.object_version,NEW.object_etag,NEW.object_digest,NEW.object_size_bytes,NEW.created_at)
    IS DISTINCT FROM
    ROW(OLD.id,OLD.ref,OLD.organization_id,OLD.session_id,OLD.provider_account_id,
        OLD.runtime_revision_id,OLD.native_session_id,OLD.native_checkpoint,OLD.content_generation,
        OLD.format_version,OLD.source_relative_path,OLD.source_sha256,OLD.source_size_bytes,
        OLD.object_key,OLD.object_version,OLD.object_etag,OLD.object_digest,OLD.object_size_bytes,OLD.created_at)
    OR (NEW.project_id IS DISTINCT FROM OLD.project_id AND NOT (
      OLD.project_id IS NULL AND NEW.project_id IS NOT NULL AND EXISTS (
        SELECT 1 FROM control_plane.sessions session WHERE session.id=NEW.session_id
         AND session.organization_id=NEW.organization_id AND session.project_id=NEW.project_id))) THEN
   RAISE EXCEPTION 'session archive receipt identity is immutable';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION control_plane.native_checkpoint_shape(jsonb),control_plane.guard_native_session_identity() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.native_checkpoint_shape(jsonb),control_plane.guard_native_session_identity() TO control_plane_runtime;

RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $native_forward_only$
BEGIN
 RAISE EXCEPTION 'OpenCode-only session cutover is forward-only';
END $native_forward_only$;
-- +goose StatementEnd
