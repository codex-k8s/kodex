-- +goose Up
SET ROLE control_plane_owner;

-- ARCH-MC-008: существующий Artifact остаётся aggregate с тем же id/ref.
-- Одноимённые старые Artifact не объединяются. Содержимое имеет одного
-- владельца: immutable revision; current/history являются проекциями.
DROP VIEW control_plane.runtime_file_visible_entries;
DROP TRIGGER protect_skill_artifact_retention ON control_plane.artifacts;
ALTER TABLE control_plane.artifacts RENAME TO artifact_heads;

CREATE TABLE control_plane.artifact_revisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    artifact_id uuid NOT NULL REFERENCES control_plane.artifact_heads(id)
        DEFERRABLE INITIALLY DEFERRED,
    revision bigint NOT NULL CHECK (revision > 0),
    file_name text NOT NULL CHECK (char_length(file_name) BETWEEN 1 AND 255),
    media_type text NOT NULL CHECK (char_length(media_type) BETWEEN 1 AND 255),
    size_bytes bigint NOT NULL CHECK (size_bytes BETWEEN 0 AND 536870912),
    digest text NOT NULL CHECK (digest ~ '^sha256:[a-f0-9]{64}$'),
    source text NOT NULL CHECK (source IN ('CONTROL_CENTER','AGENT_RESULT','INTEGRATION_RESULT','KNOWLEDGE_SOURCE','INTERACTION_ATTACHMENT')),
    scan_state text NOT NULL CHECK (scan_state IN ('PENDING','SCANNING','CLEAN','QUARANTINED','FAILED')),
    object_receipt_ref text NOT NULL,
    preview_state text NOT NULL CHECK (preview_state IN ('AVAILABLE','UNAVAILABLE','BLOCKED')),
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL,
    UNIQUE (artifact_id, revision),
    UNIQUE (artifact_id, id)
);

INSERT INTO control_plane.artifact_revisions
    (id,ref,artifact_id,revision,file_name,media_type,size_bytes,digest,source,
     scan_state,object_receipt_ref,preview_state,created_by,created_at)
SELECT id,'arv_' || replace(id::text,'-',''),id,revision,file_name,media_type,
       size_bytes,digest,source,scan_state,object_receipt_ref,preview_state,created_by,created_at
FROM control_plane.artifact_heads WHERE lifecycle_state<>'PURGED';

ALTER TABLE control_plane.artifact_heads ADD COLUMN current_revision_id uuid;
UPDATE control_plane.artifact_heads SET current_revision_id=id WHERE lifecycle_state<>'PURGED';
ALTER TABLE control_plane.artifact_heads
    ADD CONSTRAINT artifact_heads_current_revision_lifecycle CHECK (
        (lifecycle_state='PURGED' AND current_revision_id IS NULL) OR
        (lifecycle_state<>'PURGED' AND current_revision_id IS NOT NULL)),
    ADD CONSTRAINT artifact_heads_current_revision FOREIGN KEY (id,current_revision_id)
        REFERENCES control_plane.artifact_revisions(artifact_id,id) DEFERRABLE INITIALLY DEFERRED,
    DROP COLUMN file_name,
    DROP COLUMN media_type,
    DROP COLUMN size_bytes,
    DROP COLUMN digest,
    DROP COLUMN source,
    DROP COLUMN scan_state,
    DROP COLUMN object_receipt_ref,
    DROP COLUMN preview_state,
    DROP COLUMN revision;

ALTER TABLE control_plane.artifact_content RENAME TO artifact_revision_content;
ALTER TABLE control_plane.artifact_revision_content
    DROP CONSTRAINT artifact_content_artifact_id_fkey;
ALTER TABLE control_plane.artifact_revision_content RENAME COLUMN artifact_id TO revision_id;
ALTER TABLE control_plane.artifact_revision_content
    ADD CONSTRAINT artifact_revision_content_revision FOREIGN KEY (revision_id)
        REFERENCES control_plane.artifact_revisions(id) DEFERRABLE INITIALLY IMMEDIATE;

CREATE VIEW control_plane.artifact_history WITH (security_invoker=true) AS
SELECT head.id,head.ref,head.organization_id,head.project_id,head.run_id,head.node_id,
       revision.file_name,revision.media_type,revision.size_bytes,revision.digest,
       revision.source,revision.scan_state,revision.object_receipt_ref,revision.preview_state,
       revision.revision,head.version,head.created_by,revision.created_at,
       head.lifecycle_state,head.deleted_at,head.purge_after,head.purged_at,
       head.retention_claim_owner,head.retention_claim_generation,head.retention_claim_expires_at,
       revision.id AS revision_id,revision.ref AS revision_ref
FROM control_plane.artifact_heads head
JOIN control_plane.artifact_revisions revision ON revision.artifact_id=head.id;

CREATE VIEW control_plane.artifacts WITH (security_invoker=true) AS
SELECT head.id,head.ref,head.organization_id,head.project_id,head.run_id,head.node_id,
       CASE WHEN head.lifecycle_state='PURGED' THEN 'purged-' || head.ref ELSE revision.file_name END AS file_name,
       CASE WHEN head.lifecycle_state='PURGED' THEN 'application/octet-stream' ELSE revision.media_type END AS media_type,
       CASE WHEN head.lifecycle_state='PURGED' THEN 0::bigint ELSE revision.size_bytes END AS size_bytes,
       CASE WHEN head.lifecycle_state='PURGED' THEN 'sha256:' || repeat('0',64) ELSE revision.digest END AS digest,
       CASE WHEN head.lifecycle_state='PURGED' THEN 'CONTROL_CENTER' ELSE revision.source END AS source,
       CASE WHEN head.lifecycle_state='PURGED' THEN 'FAILED' ELSE revision.scan_state END AS scan_state,
       CASE WHEN head.lifecycle_state='PURGED' THEN '' ELSE revision.object_receipt_ref END AS object_receipt_ref,
       CASE WHEN head.lifecycle_state='PURGED' THEN 'BLOCKED' ELSE revision.preview_state END AS preview_state,
       CASE WHEN head.lifecycle_state='PURGED' THEN 1::bigint ELSE revision.revision END AS revision,
       head.version,head.created_by,head.created_at,
       head.lifecycle_state,head.deleted_at,head.purge_after,head.purged_at,
       head.retention_claim_owner,head.retention_claim_generation,head.retention_claim_expires_at,
       revision.id AS revision_id,COALESCE(revision.ref,'') AS current_revision_ref
FROM control_plane.artifact_heads head
LEFT JOIN control_plane.artifact_revisions revision ON revision.id=head.current_revision_id AND revision.artifact_id=head.id;

CREATE VIEW control_plane.artifact_content WITH (security_invoker=true) AS
SELECT head.id AS artifact_id,content.object_key,content.object_version,content.object_etag,
       content.digest,content.size_bytes,content.stored_at
FROM control_plane.artifact_heads head
JOIN control_plane.artifact_revision_content content ON content.revision_id=head.current_revision_id;

-- Системные create-producers используют одну canonical insert-проекцию.
-- Эта функция создаёт новый aggregate, но никогда не заменяет старый body.
-- +goose StatementBegin
CREATE FUNCTION control_plane.insert_artifact_projection() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE revision_id uuid := gen_random_uuid();
BEGIN
    NEW.id := coalesce(NEW.id,gen_random_uuid());
    NEW.version := coalesce(NEW.version,1);
    NEW.created_at := coalesce(NEW.created_at,clock_timestamp());
    NEW.lifecycle_state := coalesce(NEW.lifecycle_state,'ACTIVE');
    NEW.retention_claim_generation := coalesce(NEW.retention_claim_generation,0);
    IF NEW.version<>1 OR NEW.lifecycle_state<>'ACTIVE' OR NEW.deleted_at IS NOT NULL
       OR NEW.purge_after IS NOT NULL OR NEW.purged_at IS NOT NULL
       OR NEW.retention_claim_owner IS NOT NULL OR NEW.retention_claim_generation<>0
       OR NEW.retention_claim_expires_at IS NOT NULL OR NEW.current_revision_ref IS NOT NULL
       OR NEW.revision_id IS NOT NULL THEN
        RAISE EXCEPTION 'invalid new artifact projection';
    END IF;
    NEW.current_revision_ref := 'arv_' || replace(revision_id::text,'-','');
    NEW.revision_id := revision_id;
    INSERT INTO control_plane.artifact_heads
        (id,ref,organization_id,project_id,run_id,node_id,version,created_by,created_at,current_revision_id)
    VALUES (NEW.id,NEW.ref,NEW.organization_id,NEW.project_id,NEW.run_id,NEW.node_id,
            NEW.version,NEW.created_by,NEW.created_at,revision_id);
    INSERT INTO control_plane.artifact_revisions
        (id,ref,artifact_id,revision,file_name,media_type,size_bytes,digest,source,
         scan_state,object_receipt_ref,preview_state,created_by,created_at)
    VALUES (revision_id,NEW.current_revision_ref,NEW.id,NEW.revision,NEW.file_name,NEW.media_type,
            NEW.size_bytes,NEW.digest,NEW.source,NEW.scan_state,NEW.object_receipt_ref,
            NEW.preview_state,NEW.created_by,NEW.created_at);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER insert_artifact_projection INSTEAD OF INSERT ON control_plane.artifacts
FOR EACH ROW EXECUTE FUNCTION control_plane.insert_artifact_projection();

-- +goose StatementBegin
CREATE FUNCTION control_plane.insert_artifact_content_projection() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE selected_revision uuid;
BEGIN
    SELECT revision.id INTO STRICT selected_revision
    FROM control_plane.artifact_heads head
    JOIN control_plane.artifact_revisions revision ON revision.id=head.current_revision_id
    WHERE head.id=NEW.artifact_id AND head.lifecycle_state='ACTIVE'
      AND revision.digest=NEW.digest AND revision.size_bytes=NEW.size_bytes
    FOR UPDATE OF head;
    INSERT INTO control_plane.artifact_revision_content
        (revision_id,object_key,object_version,object_etag,digest,size_bytes,stored_at)
    VALUES (selected_revision,NEW.object_key,NEW.object_version,NEW.object_etag,
            NEW.digest,NEW.size_bytes,coalesce(NEW.stored_at,clock_timestamp()));
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER insert_artifact_content_projection INSTEAD OF INSERT ON control_plane.artifact_content
FOR EACH ROW EXECUTE FUNCTION control_plane.insert_artifact_content_projection();

-- Точная ревизия binding/download grant назначается сервером в момент записи.
ALTER TABLE control_plane.artifact_bindings ADD COLUMN revision_id uuid,
    ADD COLUMN artifact_revision_ref text, ADD COLUMN artifact_revision bigint;
UPDATE control_plane.artifact_bindings binding
SET revision_id=revision.id,artifact_revision_ref=revision.ref,artifact_revision=revision.revision
FROM control_plane.artifact_heads head JOIN control_plane.artifact_revisions revision ON revision.id=head.current_revision_id
WHERE head.id=binding.artifact_id;
ALTER TABLE control_plane.artifact_bindings
    ALTER COLUMN artifact_revision_ref SET NOT NULL, ALTER COLUMN artifact_revision SET NOT NULL,
    ADD CONSTRAINT artifact_bindings_revision_owner FOREIGN KEY (artifact_id,revision_id)
        REFERENCES control_plane.artifact_revisions(artifact_id,id) ON DELETE SET NULL (revision_id) DEFERRABLE INITIALLY IMMEDIATE;
ALTER TABLE control_plane.artifact_download_grants ADD COLUMN revision_id uuid,
    ADD COLUMN artifact_revision_ref text, ADD COLUMN artifact_revision bigint;
UPDATE control_plane.artifact_download_grants grant_row
SET revision_id=revision.id,artifact_revision_ref=revision.ref,artifact_revision=revision.revision
FROM control_plane.artifact_heads head JOIN control_plane.artifact_revisions revision ON revision.id=head.current_revision_id
WHERE head.id=grant_row.artifact_id;
ALTER TABLE control_plane.artifact_download_grants
    ALTER COLUMN artifact_revision_ref SET NOT NULL, ALTER COLUMN artifact_revision SET NOT NULL,
    ADD CONSTRAINT artifact_download_grants_revision_owner FOREIGN KEY (artifact_id,revision_id)
        REFERENCES control_plane.artifact_revisions(artifact_id,id) ON DELETE SET NULL (revision_id) DEFERRABLE INITIALLY IMMEDIATE;

-- +goose StatementBegin
CREATE FUNCTION control_plane.pin_artifact_revision() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE selected_revision uuid;
BEGIN
    SELECT current_revision_id INTO STRICT selected_revision
    FROM control_plane.artifact_heads
    WHERE id=NEW.artifact_id AND lifecycle_state='ACTIVE' FOR UPDATE;
    IF NEW.revision_id IS NULL THEN NEW.revision_id:=selected_revision; END IF;
    IF NOT EXISTS (SELECT 1 FROM control_plane.artifact_revisions
                   WHERE id=NEW.revision_id AND artifact_id=NEW.artifact_id) THEN
        RAISE EXCEPTION 'artifact revision does not belong to aggregate';
    END IF;
    SELECT ref,revision INTO NEW.artifact_revision_ref,NEW.artifact_revision
    FROM control_plane.artifact_revisions WHERE id=NEW.revision_id;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER pin_artifact_binding_revision BEFORE INSERT ON control_plane.artifact_bindings
FOR EACH ROW EXECUTE FUNCTION control_plane.pin_artifact_revision();
CREATE TRIGGER pin_artifact_download_revision BEFORE INSERT ON control_plane.artifact_download_grants
FOR EACH ROW EXECUTE FUNCTION control_plane.pin_artifact_revision();

-- FK release очищает только техническую ссылку; origin tuple остаётся.
-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_artifact_revision_pin() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    IF ROW(NEW.artifact_id,NEW.artifact_revision_ref,NEW.artifact_revision) IS DISTINCT FROM
       ROW(OLD.artifact_id,OLD.artifact_revision_ref,OLD.artifact_revision) THEN
        RAISE EXCEPTION 'artifact revision origin is immutable';
    END IF;
    IF NEW.revision_id IS DISTINCT FROM OLD.revision_id AND NOT (
        OLD.revision_id IS NOT NULL AND NEW.revision_id IS NULL AND EXISTS (
            SELECT 1 FROM control_plane.artifact_heads head WHERE head.id=OLD.artifact_id
              AND head.lifecycle_state='PURGED' AND head.current_revision_id IS NULL)
    ) THEN RAISE EXCEPTION 'artifact revision pin is immutable'; END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER protect_artifact_binding_revision BEFORE UPDATE ON control_plane.artifact_bindings
FOR EACH ROW WHEN (NOT control_plane.project_purge_authorized())
EXECUTE FUNCTION control_plane.protect_artifact_revision_pin();
CREATE TRIGGER protect_artifact_download_revision BEFORE UPDATE ON control_plane.artifact_download_grants
FOR EACH ROW WHEN (NOT control_plane.project_purge_authorized())
EXECUTE FUNCTION control_plane.protect_artifact_revision_pin();

-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_artifact_revision() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    IF TG_OP='DELETE' AND control_plane.project_purge_authorized() THEN RETURN OLD; END IF;
    IF TG_OP='DELETE' AND EXISTS (
        SELECT 1 FROM control_plane.artifact_heads head
        WHERE head.id=OLD.artifact_id AND head.lifecycle_state='PURGED'
          AND head.current_revision_id IS NULL
          AND NOT control_plane.artifact_has_retained_revisions(head.id)
          AND NOT EXISTS (SELECT 1 FROM control_plane.artifact_revision_content content
              JOIN control_plane.artifact_revisions revision ON revision.id=content.revision_id
              WHERE revision.artifact_id=head.id)
    ) THEN RETURN OLD; END IF;
    RAISE EXCEPTION 'artifact revision is immutable';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER protect_artifact_revision BEFORE UPDATE OR DELETE ON control_plane.artifact_revisions
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_artifact_revision();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.protect_skill_artifact_retention() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    IF TG_OP='DELETE' OR NEW.lifecycle_state IS DISTINCT FROM OLD.lifecycle_state THEN
        IF EXISTS (SELECT 1 FROM control_plane.artifact_revisions revision
                   WHERE revision.artifact_id=OLD.id AND
                     control_plane.skill_artifact_reference_count(OLD.organization_id,OLD.ref,revision.revision,revision.digest)>0) THEN
            RAISE EXCEPTION 'artifact is retained by skill revision';
        END IF;
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER protect_skill_artifact_retention BEFORE UPDATE OR DELETE ON control_plane.artifact_heads
FOR EACH ROW WHEN (NOT control_plane.project_purge_authorized())
EXECUTE FUNCTION control_plane.protect_skill_artifact_retention();

-- Ни старые entries, ни их digest не изменяются. Алгоритм совпадает с
-- canonical runtime_files_capture_entries.sql, включая исходные имена keys.
-- +goose StatementBegin
CREATE FUNCTION control_plane.runtime_file_entry_digest(entry control_plane.runtime_file_catalog_entries)
RETURNS bytea LANGUAGE sql IMMUTABLE SECURITY INVOKER SET search_path=pg_catalog AS $$
    SELECT public.digest(convert_to(jsonb_build_object(
        'ref',entry.artifact_ref,'revision',entry.artifact_revision,'version',entry.artifact_version,
        'digest',entry.artifact_digest,'file_name',entry.file_name,'media_type',entry.media_type,
        'size_bytes',entry.size_bytes,'purpose',entry.purpose,'source',entry.source,
        'source_ref',entry.source_ref,'source_revision_ref',entry.source_revision_ref,
        'project_ref',entry.project_ref,'run_ref',entry.run_ref)::text,'UTF8'),'sha256');
$$;
-- +goose StatementEnd

-- Forward-only замена ровно доказанных reader joins. Неизвестная версия
-- canonical функции закрыто останавливает migration вместо слепого patch.
-- +goose StatementBegin
DO $$
DECLARE source_definition text; replacement text;
BEGIN
    source_definition:=pg_get_functiondef('control_plane.runtime_file_source_visible(uuid,uuid,uuid,uuid,uuid,text,text,uuid)'::regprocedure);
    IF position('JOIN control_plane.artifacts file ON file.id=file_target.id' IN source_definition)=0
       OR position('JOIN control_plane.artifact_content content ON content.artifact_id=file.id' IN source_definition)=0
       OR position('AND pin->''revision''=to_jsonb(file.revision) AND pin->''version''=to_jsonb(file.version)' IN source_definition)=0 THEN
        RAISE EXCEPTION 'runtime file source reader profile changed';
    END IF;
    replacement:=replace(source_definition,'JOIN control_plane.artifacts file ON file.id=file_target.id',
        'JOIN control_plane.artifact_history file ON file.id=file_target.id');
    replacement:=replace(replacement,'JOIN control_plane.artifact_content content ON content.artifact_id=file.id',
        'JOIN control_plane.artifact_revision_content content ON content.revision_id=file.revision_id');
    replacement:=replace(replacement,'AND pin->''revision''=to_jsonb(file.revision) AND pin->''version''=to_jsonb(file.version)',
        'AND pin->''revision''=to_jsonb(file.revision)');
    EXECUTE replacement;

    source_definition:=pg_get_functiondef('control_plane.protect_runtime_file_catalog()'::regprocedure);
    IF position('JOIN control_plane.artifacts artifact ON artifact.id=NEW.artifact_id' IN source_definition)=0
       OR position('AND artifact.digest=NEW.artifact_digest AND artifact.size_bytes=NEW.size_bytes' IN source_definition)=0 THEN
        RAISE EXCEPTION 'runtime file catalog guard profile changed';
    END IF;
    replacement:=replace(source_definition,'JOIN control_plane.artifacts artifact ON artifact.id=NEW.artifact_id',
        'JOIN control_plane.artifact_history artifact ON artifact.id=NEW.artifact_id');
    replacement:=replace(replacement,'AND artifact.digest=NEW.artifact_digest AND artifact.size_bytes=NEW.size_bytes',
        'AND artifact.digest=NEW.artifact_digest AND artifact.size_bytes=NEW.size_bytes
         AND artifact.media_type=NEW.media_type AND artifact.source=NEW.source
         AND (NEW.purpose=''SKILL'' OR artifact.file_name=NEW.file_name)
         AND NEW.entry_digest=control_plane.runtime_file_entry_digest(NEW)');
    EXECUTE replacement;
END;
$$;
-- +goose StatementEnd

-- ARCH-MC-008: frozen aggregate version не является новым current head OCC.
-- Ничего в immutable entry/snapshot не переписывается; exact body metadata
-- и свежая owner/source eligibility проверяются независимо.
CREATE VIEW control_plane.runtime_file_visible_entries WITH (security_invoker=true) AS
SELECT entry.id,entry.ref,entry.catalog_id,entry.artifact_id,entry.artifact_ref,entry.artifact_revision,
    entry.artifact_version,entry.artifact_digest,entry.file_name,entry.media_type,entry.size_bytes,
    entry.purpose,entry.project_ref,entry.run_ref,entry.source,entry.source_ref,entry.source_revision_ref,entry.entry_digest
FROM control_plane.runtime_file_catalog_entries entry
JOIN control_plane.runtime_file_catalogs catalog ON catalog.id=entry.catalog_id AND catalog.frozen
JOIN control_plane.runtime_revisions revision ON revision.ref=catalog.runtime_revision_ref
JOIN control_plane.artifact_history artifact ON artifact.id=entry.artifact_id AND artifact.ref=entry.artifact_ref
  AND artifact.revision=entry.artifact_revision AND artifact.digest=entry.artifact_digest
  AND artifact.size_bytes=entry.size_bytes AND artifact.media_type=entry.media_type AND artifact.source=entry.source
  AND (entry.purpose='SKILL' OR artifact.file_name=entry.file_name)
  AND artifact.scan_state='CLEAN'
JOIN control_plane.artifact_revision_content content ON content.revision_id=artifact.revision_id
  AND content.digest=entry.artifact_digest AND content.size_bytes=entry.size_bytes
WHERE control_plane.runtime_file_source_visible(catalog.organization_id,catalog.actor_id,catalog.project_id,
    catalog.agent_id,entry.artifact_id,entry.purpose,entry.source_revision_ref,catalog.node_id)
  AND entry.entry_digest=control_plane.runtime_file_entry_digest(entry)
  AND (entry.purpose<>'SKILL' OR EXISTS (
    SELECT 1 FROM jsonb_array_elements(revision.safe_snapshot #> '{contextSnapshot,skills}') skill
    JOIN control_plane.agent_context_bindings binding
      ON binding.organization_id=catalog.organization_id AND binding.agent_id=catalog.agent_id
      AND binding.ref=skill->>'binding_ref' AND to_jsonb(binding.version)=skill->'binding_version' AND binding.enabled
    WHERE skill->>'bundle_ref'=entry.source_ref AND skill->>'revision_ref'=entry.source_revision_ref));

-- Все исторические pins удерживают aggregate независимо от current pointer.
-- Reader возвращает только boolean; scoped rows не раскрываются worker.
-- +goose StatementBegin
CREATE FUNCTION control_plane.artifact_active_binding_count(p_artifact_id uuid)
RETURNS bigint LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
    SELECT count(*) FROM control_plane.artifact_heads head
    JOIN control_plane.artifact_bindings binding ON binding.artifact_id=head.id
    WHERE head.id=p_artifact_id AND (
        (binding.target_kind IN ('KNOWLEDGE','AGENT') AND EXISTS (
            SELECT 1 FROM control_plane.agents agent
            JOIN control_plane.projects project ON project.id=agent.project_id
            WHERE agent.ref=binding.target_ref AND agent.organization_id=head.organization_id
              AND agent.project_id=head.project_id AND project.organization_id=head.organization_id
              AND project.lifecycle='ACTIVE' AND agent.system_key IS NULL AND agent.state<>'ARCHIVED'
              AND (binding.target_kind='KNOWLEDGE' OR
                  (agent.avatar_artifact_id=head.id AND agent.avatar_artifact_revision=binding.artifact_revision))))
        OR (binding.target_kind='RUN_RESULT' AND EXISTS (
            SELECT 1 FROM control_plane.runs run WHERE run.id=head.run_id AND run.ref=binding.target_ref
              AND run.organization_id=head.organization_id AND run.project_id IS NOT DISTINCT FROM head.project_id
              AND run.state IN ('QUEUED','RUNNING','WAITING_HUMAN','CANCELLING')))
        -- Неизвестный executable binding kind не получает guessed authority.
        OR binding.target_kind NOT IN ('KNOWLEDGE','AGENT','RUN_INPUT','RUN_RESULT')
    );
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.artifact_active_binding_count(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.artifact_active_binding_count(uuid) TO control_plane_runtime,artifact_retention_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.artifact_has_retained_revisions(p_artifact_id uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
    SELECT EXISTS (
        SELECT 1 FROM control_plane.artifact_heads artifact
        WHERE artifact.id=p_artifact_id AND (
            control_plane.artifact_active_binding_count(artifact.id)>0
            OR EXISTS (SELECT 1 FROM control_plane.artifact_revisions revision
                WHERE revision.artifact_id=artifact.id AND
                  control_plane.skill_artifact_reference_count(artifact.organization_id,artifact.ref,revision.revision,revision.digest)>0)
            OR EXISTS (
                SELECT 1 FROM control_plane.runs active_run
                WHERE active_run.organization_id=artifact.organization_id
                  AND active_run.state IN ('QUEUED','RUNNING','WAITING_HUMAN','CANCELLING')
                  AND (
                    EXISTS (SELECT 1 FROM control_plane.attachment_set_items item
                        WHERE item.artifact_id=artifact.id AND item.attachment_set_id=active_run.input_attachment_set_id)
                    OR EXISTS (SELECT 1 FROM control_plane.session_turns turn
                        JOIN control_plane.attachment_set_items item ON item.attachment_set_id=turn.attachment_set_id
                        WHERE item.artifact_id=artifact.id AND (turn.run_id=active_run.id OR
                            (turn.session_id=active_run.session_id AND turn.created_at<active_run.created_at
                             AND (artifact.lifecycle_state='ACTIVE' OR artifact.deleted_at>active_run.created_at))))
                    OR EXISTS (SELECT 1 FROM control_plane.runtime_revisions runtime_revision
                        WHERE runtime_revision.root_run_id=active_run.id AND (
                          EXISTS (SELECT 1 FROM jsonb_array_elements(COALESCE(runtime_revision.safe_snapshot->'artifacts','[]'::jsonb)) pin
                              WHERE pin->>'ref'=artifact.ref)
                          OR EXISTS (SELECT 1 FROM control_plane.runtime_file_catalogs catalog
                              JOIN control_plane.runtime_file_catalog_entries entry ON entry.catalog_id=catalog.id
                              WHERE catalog.runtime_revision_ref=runtime_revision.ref AND entry.artifact_id=artifact.id)))
                    OR EXISTS (SELECT 1 FROM control_plane.run_edges callback
                        JOIN control_plane.callback_receipts receipt ON receipt.callback_edge_id=callback.id
                        CROSS JOIN LATERAL jsonb_array_elements(COALESCE(receipt.result_snapshot->'artifacts','[]'::jsonb)) pin
                        WHERE callback.root_run_id=active_run.id AND pin->>'ref'=artifact.ref)
                  ))
        )
    );
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.artifact_has_retained_revisions(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.artifact_has_retained_revisions(uuid) TO control_plane_runtime,artifact_retention_runtime;
GRANT EXECUTE ON FUNCTION control_plane.project_purge_authorized() TO artifact_retention_runtime;

-- Receipt неизменяем; удаление допускается только закрытым owner purge.
-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_artifact_revision_content() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE head control_plane.artifact_heads%ROWTYPE;
BEGIN
    IF TG_OP='DELETE' AND control_plane.project_purge_authorized() THEN RETURN OLD; END IF;
    IF TG_OP='UPDATE' THEN RAISE EXCEPTION 'artifact revision content is immutable'; END IF;
    SELECT artifact.* INTO STRICT head FROM control_plane.artifact_heads artifact
    JOIN control_plane.artifact_revisions revision ON revision.artifact_id=artifact.id
    WHERE revision.id=CASE WHEN TG_OP='DELETE' THEN OLD.revision_id ELSE NEW.revision_id END
    FOR UPDATE OF artifact;
    IF TG_OP='DELETE' THEN
        IF head.lifecycle_state<>'PURGE_PENDING' OR control_plane.artifact_has_retained_revisions(head.id) THEN
            RAISE EXCEPTION 'artifact revision content is retained';
        END IF;
        RETURN OLD;
    END IF;
    IF head.lifecycle_state<>'ACTIVE' OR NOT EXISTS (
        SELECT 1 FROM control_plane.artifact_revisions revision WHERE revision.id=NEW.revision_id
          AND revision.digest=NEW.digest AND revision.size_bytes=NEW.size_bytes
    ) THEN RAISE EXCEPTION 'artifact revision content binding is invalid'; END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER protect_artifact_revision_content BEFORE INSERT OR UPDATE OR DELETE ON control_plane.artifact_revision_content
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_artifact_revision_content();

-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_artifact_purge() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    IF NEW.lifecycle_state IN ('PURGE_PENDING','PURGED') AND
       NEW.lifecycle_state IS DISTINCT FROM OLD.lifecycle_state THEN
        IF control_plane.artifact_has_retained_revisions(OLD.id) THEN
            RAISE EXCEPTION 'artifact has retained revisions';
        END IF;
    END IF;
    IF NEW.lifecycle_state='PURGED' AND EXISTS (
        SELECT 1 FROM control_plane.artifact_revisions revision
        JOIN control_plane.artifact_revision_content content ON content.revision_id=revision.id
        WHERE revision.artifact_id=OLD.id
    ) THEN RAISE EXCEPTION 'artifact revision content remains'; END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER protect_artifact_purge BEFORE UPDATE ON control_plane.artifact_heads
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_artifact_purge();

GRANT SELECT ON control_plane.artifact_heads,control_plane.artifact_revisions,
    control_plane.artifact_revision_content,control_plane.artifact_history,
    control_plane.artifacts,control_plane.artifact_content TO artifact_retention_runtime;
GRANT UPDATE (lifecycle_state,retention_claim_owner,retention_claim_generation,
    retention_claim_expires_at,version,purged_at) ON control_plane.artifact_heads TO artifact_retention_runtime;
GRANT DELETE ON control_plane.artifact_revision_content TO artifact_retention_runtime;
GRANT DELETE ON control_plane.artifact_revisions TO artifact_retention_runtime,control_plane_runtime;
GRANT UPDATE (current_revision_id) ON control_plane.artifact_heads TO artifact_retention_runtime;
REVOKE UPDATE ON control_plane.artifact_revisions FROM control_plane_runtime;

-- Внешний owner inventory перечисляет все immutable receipts, не current view.
-- +goose StatementBegin
DO $$
DECLARE definition text;
BEGIN
    definition:=pg_get_functiondef('control_plane.project_purge_external_inventory(uuid,uuid)'::regprocedure);
    IF position('FROM control_plane.artifact_content content' IN definition)=0 OR
       position('JOIN control_plane.artifacts artifact ON artifact.id = content.artifact_id' IN definition)=0 THEN
        RAISE EXCEPTION 'project artifact inventory reader profile changed';
    END IF;
    EXECUTE replace(replace(definition,
        'FROM control_plane.artifact_content content','FROM control_plane.artifact_revision_content content'),
        'JOIN control_plane.artifacts artifact ON artifact.id = content.artifact_id',
        'JOIN control_plane.artifact_history artifact ON artifact.revision_id = content.revision_id');
END;
$$;
-- +goose StatementEnd
-- +goose StatementBegin
DO $migration$
DECLARE
    v_nodes integer;
    v_node_fingerprint text;
    v_edges integer;
    v_edge_fingerprint text;
    v_definition text;
    v_old_nodes constant text := 'IF v_nodes <> 101 OR v_node_fingerprint <> ''fd84f327b771b83de59d0839d477432c'' THEN';
    v_new_nodes constant text := 'IF v_nodes <> 102 OR v_node_fingerprint <> ''97624a5baaeeac387fbdb8b45a51d8b4'' THEN';
    v_old_edges constant text := 'IF v_edges <> 264 OR v_edge_fingerprint <> ''149c59d0b352b832823e09f10a28f55b'' THEN';
    v_new_edges constant text := 'IF v_edges <> 268 OR v_edge_fingerprint <> ''ba6d7726ee03622203d9514b20914a3c'' THEN';
BEGIN
    WITH RECURSIVE nodes(relation_id) AS (
        SELECT 'control_plane.projects'::regclass::oid
        UNION
        SELECT constraint_row.conrelid FROM nodes
        JOIN pg_constraint constraint_row ON constraint_row.contype='f' AND constraint_row.confrelid=nodes.relation_id
        JOIN pg_namespace namespace_row ON namespace_row.oid=constraint_row.connamespace AND namespace_row.nspname='control_plane'
    )
    SELECT count(*), md5(string_agg(namespace_row.nspname || '.' || class_row.relname,
        E'\n' ORDER BY namespace_row.nspname,class_row.relname))
    INTO v_nodes,v_node_fingerprint FROM nodes
    JOIN pg_class class_row ON class_row.oid=nodes.relation_id
    JOIN pg_namespace namespace_row ON namespace_row.oid=class_row.relnamespace;

    WITH RECURSIVE nodes(relation_id) AS (
        SELECT 'control_plane.projects'::regclass::oid
        UNION
        SELECT constraint_row.conrelid FROM nodes
        JOIN pg_constraint constraint_row ON constraint_row.contype='f' AND constraint_row.confrelid=nodes.relation_id
        JOIN pg_namespace namespace_row ON namespace_row.oid=constraint_row.connamespace AND namespace_row.nspname='control_plane'
    )
    SELECT count(*), md5(string_agg(child_namespace.nspname || '.' || child_class.relname || '|' ||
        constraint_row.conname || '|' || parent_namespace.nspname || '.' || parent_class.relname || '|' || constraint_row.confdeltype::text,
        E'\n' ORDER BY child_namespace.nspname,child_class.relname,constraint_row.conname))
    INTO v_edges,v_edge_fingerprint FROM pg_constraint constraint_row
    JOIN pg_class child_class ON child_class.oid=constraint_row.conrelid
    JOIN pg_namespace child_namespace ON child_namespace.oid=child_class.relnamespace
    JOIN pg_class parent_class ON parent_class.oid=constraint_row.confrelid
    JOIN pg_namespace parent_namespace ON parent_namespace.oid=parent_class.relnamespace
    WHERE constraint_row.contype='f' AND constraint_row.confrelid IN (SELECT relation_id FROM nodes)
      AND constraint_row.conrelid IN (SELECT relation_id FROM nodes);
    IF v_nodes<>102 OR v_node_fingerprint<>'97624a5baaeeac387fbdb8b45a51d8b4'
       OR v_edges<>268 OR v_edge_fingerprint<>'ba6d7726ee03622203d9514b20914a3c' THEN
        RAISE EXCEPTION 'immutable artifact purge graph migration precondition failed';
    END IF;
    SELECT pg_get_functiondef('control_plane.purge_project_database(uuid,uuid,text)'::regprocedure) INTO v_definition;
    IF v_definition IS NULL OR strpos(v_definition,v_old_nodes)=0 OR strpos(v_definition,v_old_edges)=0
       OR strpos(substr(v_definition,strpos(v_definition,v_old_nodes)+length(v_old_nodes)),v_old_nodes)>0
       OR strpos(substr(v_definition,strpos(v_definition,v_old_edges)+length(v_old_edges)),v_old_edges)>0 THEN
        RAISE EXCEPTION 'immutable artifact purge function migration precondition failed';
    END IF;
    EXECUTE replace(replace(v_definition,v_old_nodes,v_new_nodes),v_old_edges,v_new_edges);
END;
$migration$;
-- +goose StatementEnd
RESET ROLE;

-- +goose Down
-- Forward-only: стабильные ссылки и immutable body receipts не переписываются.
SELECT 1;
