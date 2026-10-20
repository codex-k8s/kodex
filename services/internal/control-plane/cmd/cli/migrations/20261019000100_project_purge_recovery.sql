-- +goose Up
SET ROLE control_plane_owner;

-- Отказ от неизвестного результата не является подтверждением удаления.
-- Ledger остаётся владельцем exact technical cleanup после удаления проекта.
ALTER TABLE control_plane.project_purge_receipts ADD COLUMN recovery_version bigint NOT NULL DEFAULT 1 CHECK (recovery_version>0);
ALTER TABLE control_plane.project_purge_receipts
 ADD COLUMN recovery_resolution_ref text CHECK (recovery_resolution_ref ~ '^ppr_[A-Za-z0-9_-]{8,80}$'),
 ADD COLUMN recovery_resolution_digest text CHECK (recovery_resolution_digest ~ '^[a-f0-9]{64}$'),
 ADD COLUMN recovery_resolution_actor uuid;
CREATE UNIQUE INDEX project_purge_receipts_origin ON control_plane.project_purge_receipts(organization_id,project_ref);
ALTER TABLE control_plane.prepared_content
 ADD COLUMN owner_resolution jsonb,
 ADD COLUMN recovery_storage_identity text CHECK (recovery_storage_identity ~ '^[a-f0-9]{64}$'),
 ADD COLUMN recovery_state text NOT NULL DEFAULT '' CHECK (recovery_state IN ('','PENDING','CLAIMED','OBSERVING','DELETED_CONFIRMED','WAITING_OWNER')),
 ADD COLUMN recovery_check_after timestamptz,
 ADD COLUMN recovery_absent_at timestamptz;
ALTER TABLE control_plane.prepared_content DROP CONSTRAINT prepared_content_state_check;
ALTER TABLE control_plane.prepared_content ADD CONSTRAINT prepared_content_state_check
 CHECK (state IN ('IN_FLIGHT','UNKNOWN','STAGED','ADOPTED','ABANDONED','CLEANING','CLEANED','WAITING_OWNER','OWNER_ABANDONED'));

-- +goose StatementBegin
DO $$
DECLARE selected text;
BEGIN
 SELECT conname INTO STRICT selected FROM pg_constraint
 WHERE conrelid='control_plane.prepared_content'::regclass AND contype='c'
   AND position('cleanup_owner IS NOT NULL' IN pg_get_constraintdef(oid))>0
   AND position('CLEANING' IN pg_get_constraintdef(oid))>0;
 EXECUTE format('ALTER TABLE control_plane.prepared_content DROP CONSTRAINT %I',selected);
END $$;
-- +goose StatementEnd
ALTER TABLE control_plane.prepared_content ADD CONSTRAINT prepared_content_cleanup_lease_shape
 CHECK ((state='CLEANING' OR (state='OWNER_ABANDONED' AND recovery_state='CLAIMED')) =
        (cleanup_owner IS NOT NULL AND cleanup_deadline IS NOT NULL));
ALTER TABLE control_plane.prepared_content DROP CONSTRAINT prepared_content_terminal_mask;
ALTER TABLE control_plane.prepared_content ADD CONSTRAINT prepared_content_terminal_mask CHECK (
 (deletion_receipt_digest IS NULL AND owner_resolution IS NULL AND ref IS NOT NULL
  AND file_name IS NOT NULL AND media_type IS NOT NULL AND digest IS NOT NULL AND size_bytes IS NOT NULL
  AND scan_state IS NOT NULL AND preview_state IS NOT NULL AND object_key IS NOT NULL
  AND object_version IS NOT NULL AND object_etag IS NOT NULL) OR
 (deletion_receipt_digest IS NOT NULL AND state IN ('ADOPTED','CLEANED')
  AND owner_resolution IS NULL AND ref IS NULL AND file_name IS NULL AND media_type IS NULL
  AND digest IS NULL AND size_bytes IS NULL AND scan_state IS NULL AND preview_state IS NULL
  AND object_key IS NULL AND object_version IS NULL AND object_etag IS NULL) OR
 (state='OWNER_ABANDONED' AND deletion_receipt_digest IS NULL AND owner_resolution IS NOT NULL
  AND ref IS NULL AND file_name IS NULL AND media_type IS NULL AND scan_state IS NULL AND preview_state IS NULL
  AND digest IS NOT NULL AND size_bytes IS NOT NULL AND object_key IS NOT NULL
  AND object_version IS NOT NULL AND object_etag IS NOT NULL));
ALTER TABLE control_plane.prepared_content ADD CONSTRAINT prepared_content_owner_resolution_shape CHECK (
 (state='OWNER_ABANDONED')=(owner_resolution IS NOT NULL) AND
 (owner_resolution IS NULL OR (jsonb_typeof(owner_resolution)='object'
   AND owner_resolution ?& ARRAY['ref','actorId','snapshotDigest','resolvedAt','projectRef']
   AND owner_resolution-ARRAY['ref','actorId','snapshotDigest','resolvedAt','projectRef']='{}'::jsonb
   AND owner_resolution->>'ref' ~ '^ppr_[A-Za-z0-9_-]{8,80}$'
   AND owner_resolution->>'snapshotDigest' ~ '^[a-f0-9]{64}$'
   AND recovery_storage_identity IS NOT NULL AND recovery_state<>'' AND recovery_check_after IS NOT NULL)));
CREATE INDEX prepared_content_owner_recovery_due ON control_plane.prepared_content(recovery_check_after,id)
 WHERE state='OWNER_ABANDONED' AND recovery_state IN ('PENDING','OBSERVING','CLAIMED');
CREATE INDEX prepared_content_owner_recovery_origin ON control_plane.prepared_content(organization_id,origin_project_ref,id)
 WHERE state IN ('WAITING_OWNER','OWNER_ABANDONED');

CREATE TABLE control_plane.project_purge_recovery_context (
 transaction_id xid8 NOT NULL, backend_pid integer NOT NULL, ledger_id uuid NOT NULL,
 purpose text NOT NULL CHECK (purpose IN ('RESOLVE','CLAIM','PIN','FINISH','SCRUB')),
 PRIMARY KEY(transaction_id,ledger_id));
REVOKE ALL ON control_plane.project_purge_recovery_context FROM PUBLIC,control_plane_runtime;

-- Начало ledger не несёт owner decision или cleanup authority. Даже INSERT
-- от table owner не заменяет специализированный RESOLVE-переход UPDATE.
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_prepared_content_recovery_insert() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF NEW.state IS DISTINCT FROM 'IN_FLIGHT' OR NEW.generation IS DISTINCT FROM 1
    OR NEW.cleanup_attempts IS DISTINCT FROM 0 OR NEW.cleanup_owner IS NOT NULL OR NEW.cleanup_deadline IS NOT NULL
    OR NEW.object_version IS DISTINCT FROM '' OR NEW.object_etag IS DISTINCT FROM ''
    OR NEW.adopted_revision_id IS NOT NULL OR NEW.adopted_revision_ref IS NOT NULL
    OR NEW.deletion_receipt_digest IS NOT NULL OR NEW.owner_resolution IS NOT NULL
    OR NEW.recovery_storage_identity IS NOT NULL OR NEW.recovery_state IS DISTINCT FROM ''
    OR NEW.recovery_check_after IS NOT NULL OR NEW.recovery_absent_at IS NOT NULL THEN
   RAISE EXCEPTION 'prepared content initial state is protected' USING ERRCODE='42501'; END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER guard_prepared_content_recovery_insert BEFORE INSERT ON control_plane.prepared_content
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_prepared_content_recovery_insert();

-- +goose StatementBegin
DO $migration$
DECLARE definition text; marker text; replacement text;
BEGIN
 SELECT pg_get_functiondef('control_plane.protect_prepared_content()'::regprocedure) INTO definition;
 marker:='  IF TG_OP=''DELETE'' THEN RAISE EXCEPTION ''prepared content intent cannot be deleted''; END IF;';
 IF strpos(definition,marker)=0 THEN RAISE EXCEPTION 'prepared recovery trigger precondition failed'; END IF;
 replacement:=marker || E'\n' || $guard$
  IF TG_OP='UPDATE' AND (OLD.state='OWNER_ABANDONED' OR NEW.state='OWNER_ABANDONED') THEN
    IF current_user<>'control_plane_owner' OR NOT EXISTS (
      SELECT 1 FROM control_plane.project_purge_recovery_context context
      WHERE context.transaction_id=pg_current_xact_id_if_assigned() AND context.backend_pid=pg_backend_pid()
        AND context.ledger_id=OLD.id) THEN
      RAISE EXCEPTION 'prepared owner resolution is not authorized';
    END IF;
    IF OLD.state='WAITING_OWNER' AND NEW.state='OWNER_ABANDONED' THEN
      IF NOT EXISTS (SELECT 1 FROM control_plane.project_purge_recovery_context context
          WHERE context.transaction_id=pg_current_xact_id_if_assigned() AND context.backend_pid=pg_backend_pid()
            AND context.ledger_id=OLD.id AND context.purpose='RESOLVE')
        OR OLD.adopted_revision_id IS NOT NULL OR OLD.adopted_revision_ref IS NOT NULL
        OR OLD.object_version<>'' OR OLD.object_etag<>'' OR OLD.cleanup_owner IS NOT NULL
        OR OLD.cleanup_deadline IS NOT NULL OR OLD.cleanup_attempts<>3
        OR OLD.writer_deadline>clock_timestamp() OR NEW.generation<>OLD.generation+1
        OR NEW.object_key IS DISTINCT FROM OLD.object_key OR NEW.digest IS DISTINCT FROM OLD.digest
        OR NEW.size_bytes IS DISTINCT FROM OLD.size_bytes
        OR (to_jsonb(OLD)-ARRAY['state','generation','ref','file_name','media_type','scan_state','preview_state','owner_resolution',
             'recovery_storage_identity','recovery_state','recovery_check_after','cleanup_attempts','updated_at']) IS DISTINCT FROM
           (to_jsonb(NEW)-ARRAY['state','generation','ref','file_name','media_type','scan_state','preview_state','owner_resolution',
             'recovery_storage_identity','recovery_state','recovery_check_after','cleanup_attempts','updated_at']) THEN
        RAISE EXCEPTION 'prepared owner resolution binding is invalid';
      END IF;
    ELSIF OLD.state='OWNER_ABANDONED' AND NEW.state='OWNER_ABANDONED' THEN
      IF NEW.owner_resolution IS DISTINCT FROM OLD.owner_resolution
        OR NEW.recovery_storage_identity IS DISTINCT FROM OLD.recovery_storage_identity
        OR NEW.generation<OLD.generation OR
        (to_jsonb(OLD)-ARRAY['project_id','target_artifact_id','plan_id','plan_revision_id','generation',
          'cleanup_owner','cleanup_deadline','cleanup_attempts','object_version','object_etag',
          'recovery_state','recovery_check_after','recovery_absent_at','updated_at']) IS DISTINCT FROM
        (to_jsonb(NEW)-ARRAY['project_id','target_artifact_id','plan_id','plan_revision_id','generation',
          'cleanup_owner','cleanup_deadline','cleanup_attempts','object_version','object_etag',
          'recovery_state','recovery_check_after','recovery_absent_at','updated_at']) THEN
        RAISE EXCEPTION 'prepared owner resolution is immutable';
      END IF;
      IF (NEW.project_id,NEW.target_artifact_id,NEW.plan_id,NEW.plan_revision_id) IS DISTINCT FROM
         (OLD.project_id,OLD.target_artifact_id,OLD.plan_id,OLD.plan_revision_id) AND NOT EXISTS (
        SELECT 1 FROM control_plane.project_purge_context context
        JOIN control_plane.project_purge_receipts receipt ON receipt.project_id=context.project_id
        WHERE context.transaction_id=pg_current_xact_id_if_assigned() AND context.backend_pid=pg_backend_pid()
          AND context.project_id=OLD.project_id AND receipt.organization_id=OLD.organization_id
          AND receipt.state='OBJECTS_CLEARED') THEN
        RAISE EXCEPTION 'prepared owner resolution detach is not authorized';
      END IF;
    ELSE RAISE EXCEPTION 'prepared owner resolution transition is invalid';
    END IF;
    RETURN NEW;
  END IF;
  -- Project purge имеет собственное подтверждение внешней очистки. Оно не
  -- подделывает artifact-retention claim, lifecycle или поколение retention.
  IF OLD.state='ADOPTED' AND NEW.state='ADOPTED'
     AND OLD.deletion_receipt_digest IS NULL AND NEW.deletion_receipt_digest IS NOT NULL
     AND current_user='control_plane_owner' AND EXISTS (
       SELECT 1 FROM control_plane.project_purge_recovery_context recovery
       JOIN control_plane.project_purge_context context ON context.transaction_id=recovery.transaction_id
         AND context.backend_pid=recovery.backend_pid AND context.project_id=OLD.project_id
       JOIN control_plane.project_purge_receipts receipt ON receipt.project_id=context.project_id
         AND receipt.organization_id=OLD.organization_id AND receipt.state='OBJECTS_CLEARED'
       WHERE recovery.transaction_id=pg_current_xact_id_if_assigned()
         AND recovery.backend_pid=pg_backend_pid() AND recovery.ledger_id=OLD.id AND recovery.purpose='SCRUB'
     ) THEN
    IF NEW.adopted_revision_id IS NOT NULL OR
       (to_jsonb(OLD)-ARRAY['ref','file_name','media_type','digest','size_bytes','scan_state','preview_state',
         'object_key','object_version','object_etag','deletion_receipt_digest','adopted_revision_id','updated_at']) IS DISTINCT FROM
       (to_jsonb(NEW)-ARRAY['ref','file_name','media_type','digest','size_bytes','scan_state','preview_state',
         'object_key','object_version','object_etag','deletion_receipt_digest','adopted_revision_id','updated_at']) THEN
       RAISE EXCEPTION 'prepared project purge scrub binding is invalid';
    END IF;
    RETURN NEW;
  END IF;
 $guard$;
 EXECUTE replace(definition,marker,replacement);
END $migration$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION control_plane.project_purge_recovery_snapshot(p_organization_id uuid,p_project_ref text,p_storage_identity text)
RETURNS jsonb LANGUAGE plpgsql STABLE SET search_path=pg_catalog AS $$
DECLARE result jsonb;
BEGIN
 IF p_organization_id IS NULL OR p_project_ref IS NULL OR p_project_ref !~ '^prj_[A-Za-z0-9_-]{8,80}$'
    OR p_storage_identity IS NULL OR p_storage_identity !~ '^[a-f0-9]{64}$' THEN
   RAISE EXCEPTION 'invalid project purge recovery snapshot' USING ERRCODE='22023'; END IF;
 RETURN (
 WITH boundary AS (
   SELECT receipt.project_ref,receipt.recovery_version,receipt.recovery_resolution_ref,receipt.state receipt_state,project.version project_version,
     project.id project_id,project.lifecycle
   FROM control_plane.project_purge_receipts receipt
   LEFT JOIN control_plane.projects project ON project.id=receipt.project_id AND project.organization_id=receipt.organization_id
   WHERE receipt.organization_id=p_organization_id AND receipt.project_ref=p_project_ref
     AND receipt.state IN ('PENDING','DONE')
 ), selected AS (
   SELECT content.id,content.state,content.generation,content.object_key,content.object_version,content.object_etag,
      content.digest,content.size_bytes,content.owner_resolution,content.recovery_state,content.recovery_storage_identity,
      content.writer_deadline,content.cleanup_owner,content.cleanup_deadline,content.adopted_revision_id,content.adopted_revision_ref,
      content.source_run_ref,content.source_lease_ref,content.source_lease_generation,content.source_fence_digest,
      content.source_context_digest,content.request_digest,content.intent_digest,content.origin_project_ref,
      content.cleanup_attempts,content.project_id,content.plan_id,content.plan_revision_id,content.plan_revision,
      content.target_artifact_id,content.prepared_artifact_ref,content.prepared_revision_ref
   FROM control_plane.prepared_content content
   WHERE content.organization_id=p_organization_id AND content.origin_project_ref=p_project_ref
      AND content.state IN ('WAITING_OWNER','OWNER_ABANDONED')
   ORDER BY content.id LIMIT 257
 ), summary AS (
   SELECT count(*) FILTER (WHERE state='WAITING_OWNER')::integer unknown_count,
     count(*) FILTER (WHERE state='OWNER_ABANDONED' AND recovery_state<>'DELETED_CONFIRMED')::integer pending_count,
     COALESCE(bool_or(state='WAITING_OWNER' OR recovery_state='WAITING_OWNER'),false) waiting,
     COALESCE(bool_or(recovery_state IN ('PENDING','CLAIMED')),false) queued,
     COALESCE(bool_or(recovery_state='OBSERVING'),false) observing,
     COALESCE(max(owner_resolution->>'ref'),'') resolution_ref,
     COALESCE(bool_and(CASE WHEN state='WAITING_OWNER' THEN
       object_version='' AND object_etag='' AND writer_deadline<=clock_timestamp() AND cleanup_owner IS NULL
         AND cleanup_deadline IS NULL AND cleanup_attempts=3
         AND adopted_revision_id IS NULL AND adopted_revision_ref IS NULL
         AND project_id=(SELECT project_id FROM boundary)
       WHEN recovery_state='WAITING_OWNER' THEN recovery_storage_identity=p_storage_identity AND cleanup_owner IS NULL
       ELSE true END),false) eligible,
     COALESCE(jsonb_agg(to_jsonb(selected) ORDER BY id),'[]'::jsonb) manifest,
     count(*) total_count
   FROM selected
 )
 SELECT jsonb_build_object('projectRef',boundary.project_ref,'version',boundary.recovery_version,
   'snapshotDigest',encode(public.digest(convert_to(jsonb_build_object('v',1,'project',boundary.project_ref,
       'version',boundary.recovery_version,'projectVersion',COALESCE(boundary.project_version,0),
       'receiptState',boundary.receipt_state,'storageIdentity',p_storage_identity,'manifest',summary.manifest)::text,'UTF8'),'sha256'),'hex'),
   'state',CASE WHEN summary.waiting THEN 'WAITING_OWNER' WHEN summary.queued THEN 'QUEUED'
     WHEN summary.observing THEN 'OBSERVING' WHEN summary.total_count>0 THEN 'DELETED_CONFIRMED' ELSE 'NONE' END,
   'unknownContentCount',summary.unknown_count,'cleanupPendingCount',summary.pending_count,
   'projectPresent',boundary.project_id IS NOT NULL,'projectVersion',COALESCE(boundary.project_version,0),
   'canResolve',summary.waiting AND summary.eligible AND summary.total_count BETWEEN 1 AND 256
      AND (summary.unknown_count=0 OR (boundary.receipt_state='PENDING' AND boundary.lifecycle='PURGE_PENDING')),
   'resolutionRef',COALESCE(boundary.recovery_resolution_ref,summary.resolution_ref))
 FROM boundary CROSS JOIN summary
 );
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.project_purge_recovery_snapshot(uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.project_purge_recovery_snapshot(uuid,text,text) TO control_plane_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.project_purge_recovery_resolve(
 p_organization_id uuid,p_project_ref text,p_expected_version bigint,p_snapshot_digest text,
 p_actor_id uuid,p_resolution_ref text,p_storage_identity text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE receipt control_plane.project_purge_receipts%ROWTYPE; item control_plane.prepared_content%ROWTYPE;
 snapshot jsonb; selected_project uuid; touched integer:=0;
BEGIN
 IF p_actor_id IS NULL OR p_expected_version IS NULL OR p_expected_version<1
    OR p_snapshot_digest IS NULL OR p_snapshot_digest !~ '^[a-f0-9]{64}$'
    OR p_resolution_ref IS NULL OR p_resolution_ref !~ '^ppr_[A-Za-z0-9_-]{8,80}$' THEN
   RAISE EXCEPTION 'invalid project purge recovery resolution' USING ERRCODE='22023'; END IF;
 -- Actor приходит только из verified transport; БД повторно проверяет текущую
 -- организационную роль, а не принимает полномочия из request или GUC.
 IF NOT EXISTS (
   SELECT 1 FROM control_plane.subjects actor
   JOIN control_plane.access_bindings binding ON binding.subject_id=actor.id AND binding.organization_id=actor.organization_id
   JOIN control_plane.application_role_versions version ON version.id=binding.role_version_id
   JOIN control_plane.application_roles role ON role.id=version.role_id
   WHERE actor.id=p_actor_id AND actor.organization_id=p_organization_id AND actor.active
     AND binding.scope_kind='ORGANIZATION' AND binding.state='ACTIVE' AND role.kind='SYSTEM'
     AND role.stable_key IN ('OWNER','ADMINISTRATOR')
     AND (binding.valid_from IS NULL OR binding.valid_from<=clock_timestamp())
     AND (binding.valid_until IS NULL OR binding.valid_until>clock_timestamp())
 ) THEN RAISE EXCEPTION 'project purge recovery is denied' USING ERRCODE='42501'; END IF;
 SELECT * INTO receipt FROM control_plane.project_purge_receipts
 WHERE organization_id=p_organization_id AND project_ref=p_project_ref FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'project purge recovery was not found' USING ERRCODE='P0002'; END IF;
 IF receipt.recovery_resolution_ref=p_resolution_ref THEN
   IF receipt.recovery_resolution_digest IS DISTINCT FROM p_snapshot_digest
      OR receipt.recovery_resolution_actor IS DISTINCT FROM p_actor_id OR EXISTS (
        SELECT 1 FROM control_plane.prepared_content content
        WHERE content.organization_id=p_organization_id AND content.origin_project_ref=p_project_ref
          AND content.state='OWNER_ABANDONED' AND content.recovery_storage_identity IS DISTINCT FROM p_storage_identity) THEN
     RAISE EXCEPTION 'project purge recovery intent changed' USING ERRCODE='40001'; END IF;
   RETURN control_plane.project_purge_recovery_snapshot(p_organization_id,p_project_ref,p_storage_identity);
 END IF;
 SELECT project.id INTO selected_project FROM control_plane.projects project
 WHERE project.id=receipt.project_id AND project.organization_id=p_organization_id FOR UPDATE;
 PERFORM 1 FROM control_plane.prepared_content content
 WHERE content.organization_id=p_organization_id AND content.origin_project_ref=p_project_ref
   AND content.state IN ('WAITING_OWNER','OWNER_ABANDONED') ORDER BY content.id FOR UPDATE;
 snapshot:=control_plane.project_purge_recovery_snapshot(p_organization_id,p_project_ref,p_storage_identity);
 IF snapshot IS NULL THEN RAISE EXCEPTION 'project purge recovery was not found' USING ERRCODE='P0002'; END IF;
 IF receipt.recovery_version<>p_expected_version OR snapshot->>'snapshotDigest'<>p_snapshot_digest THEN
   RAISE EXCEPTION 'project purge recovery version changed' USING ERRCODE='40001'; END IF;
 IF NOT COALESCE((snapshot->>'canResolve')::boolean,false) THEN
   RAISE EXCEPTION 'project purge recovery transition is unavailable' USING ERRCODE='P0001'; END IF;
 FOR item IN SELECT content.* FROM control_plane.prepared_content content
   WHERE content.organization_id=p_organization_id AND content.origin_project_ref=p_project_ref
     AND (content.state='WAITING_OWNER' OR (content.state='OWNER_ABANDONED' AND content.recovery_state='WAITING_OWNER'))
   ORDER BY content.id
 LOOP
   INSERT INTO control_plane.project_purge_recovery_context VALUES(pg_current_xact_id(),pg_backend_pid(),item.id,'RESOLVE');
   IF item.state='WAITING_OWNER' THEN
     UPDATE control_plane.prepared_content SET state='OWNER_ABANDONED',generation=generation+1,
       ref=NULL,file_name=NULL,media_type=NULL,scan_state=NULL,preview_state=NULL,
       owner_resolution=jsonb_build_object('ref',p_resolution_ref,'actorId',p_actor_id,'snapshotDigest',p_snapshot_digest,
          'resolvedAt',clock_timestamp(),'projectRef',p_project_ref),
       recovery_storage_identity=p_storage_identity,recovery_state='PENDING',cleanup_attempts=0,
       recovery_check_after=clock_timestamp(),updated_at=clock_timestamp() WHERE id=item.id;
   ELSE
     UPDATE control_plane.prepared_content SET generation=generation+1,recovery_state='PENDING',cleanup_attempts=0,
       recovery_check_after=clock_timestamp(),updated_at=clock_timestamp() WHERE id=item.id;
   END IF;
   DELETE FROM control_plane.project_purge_recovery_context WHERE transaction_id=pg_current_xact_id()
     AND backend_pid=pg_backend_pid() AND ledger_id=item.id;
   touched:=touched+1;
 END LOOP;
 IF touched<1 OR touched>256 THEN RAISE EXCEPTION 'project purge recovery selection changed' USING ERRCODE='40001'; END IF;
 UPDATE control_plane.project_purge_receipts SET recovery_version=recovery_version+1,
   recovery_resolution_ref=p_resolution_ref,recovery_resolution_digest=p_snapshot_digest,recovery_resolution_actor=p_actor_id
 WHERE organization_id=p_organization_id AND project_ref=p_project_ref;
 UPDATE control_plane.projects SET version=version+1,updated_at=clock_timestamp()
 WHERE id=selected_project AND organization_id=p_organization_id;
 RETURN control_plane.project_purge_recovery_snapshot(p_organization_id,p_project_ref,p_storage_identity);
END;
$$;
-- +goose StatementEnd

-- Порядок locks всегда receipt -> ledger, как в owner finalize/resolve.
-- +goose StatementBegin
CREATE FUNCTION control_plane.project_purge_recovery_claim(p_owner text,p_limit integer,p_storage_identity text)
RETURNS TABLE(ledger_id uuid,organization_id uuid,project_ref text,object_key text,object_version text,
 object_etag text,digest text,size_bytes bigint,generation bigint,cleanup_deadline timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE receipt record; item control_plane.prepared_content%ROWTYPE; processed_count integer:=0;
BEGIN
 IF p_owner IS NULL OR char_length(p_owner) NOT BETWEEN 1 AND 128 OR p_owner ~ '[\r\n]'
    OR p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 32
    OR p_storage_identity IS NULL OR p_storage_identity !~ '^[a-f0-9]{64}$' THEN
   RAISE EXCEPTION 'invalid project purge recovery claim' USING ERRCODE='22023'; END IF;
 -- Старый project requested_at не даёт приоритет бесконечному наблюдению:
 -- ABSENT сдвигает due вперёд и освобождает следующий цикл другим проектам.
 FOR receipt IN SELECT r.organization_id,r.project_ref FROM control_plane.project_purge_receipts r
   CROSS JOIN LATERAL (
     SELECT min(c.recovery_check_after) AS next_check FROM control_plane.prepared_content c WHERE c.organization_id=r.organization_id
       AND c.origin_project_ref=r.project_ref AND c.state='OWNER_ABANDONED'
       AND c.recovery_storage_identity=p_storage_identity AND c.recovery_check_after<=clock_timestamp()
       AND (c.recovery_state IN ('PENDING','OBSERVING') OR
         (c.recovery_state='CLAIMED' AND c.cleanup_deadline<=clock_timestamp()))
   ) due
   WHERE r.state IN ('PENDING','DONE') AND due.next_check IS NOT NULL
   ORDER BY due.next_check,r.project_id LIMIT p_limit FOR UPDATE OF r SKIP LOCKED
 LOOP
   FOR item IN SELECT c.* FROM control_plane.prepared_content c
     WHERE c.organization_id=receipt.organization_id AND c.origin_project_ref=receipt.project_ref
       AND c.state='OWNER_ABANDONED' AND c.recovery_storage_identity=p_storage_identity
       AND c.recovery_check_after<=clock_timestamp()
       AND (c.recovery_state IN ('PENDING','OBSERVING') OR
         (c.recovery_state='CLAIMED' AND c.cleanup_deadline<=clock_timestamp()))
     ORDER BY c.recovery_check_after,c.id LIMIT (p_limit-processed_count) FOR UPDATE SKIP LOCKED
   LOOP
     processed_count:=processed_count+1;
     INSERT INTO control_plane.project_purge_recovery_context VALUES(pg_current_xact_id(),pg_backend_pid(),item.id,'CLAIM');
     IF item.cleanup_attempts>=3 THEN
       UPDATE control_plane.prepared_content c SET recovery_state='WAITING_OWNER',cleanup_owner=NULL,
         cleanup_deadline=NULL,generation=c.generation+1,updated_at=clock_timestamp() WHERE c.id=item.id;
       DELETE FROM control_plane.project_purge_recovery_context WHERE transaction_id=pg_current_xact_id()
         AND backend_pid=pg_backend_pid() AND project_purge_recovery_context.ledger_id=item.id;
       UPDATE control_plane.project_purge_receipts r SET recovery_version=r.recovery_version+1
         WHERE r.organization_id=receipt.organization_id AND r.project_ref=receipt.project_ref;
       CONTINUE;
     END IF;
     UPDATE control_plane.prepared_content c SET recovery_state='CLAIMED',cleanup_owner=p_owner,
       cleanup_deadline=clock_timestamp()+interval '90 seconds',cleanup_attempts=c.cleanup_attempts+1,
       generation=c.generation+1,updated_at=clock_timestamp()
       WHERE c.id=item.id RETURNING c.* INTO item;
     DELETE FROM control_plane.project_purge_recovery_context WHERE transaction_id=pg_current_xact_id()
       AND backend_pid=pg_backend_pid() AND project_purge_recovery_context.ledger_id=item.id;
     UPDATE control_plane.project_purge_receipts r SET recovery_version=r.recovery_version+1
       WHERE r.organization_id=receipt.organization_id AND r.project_ref=receipt.project_ref;
     ledger_id:=item.id; organization_id:=item.organization_id; project_ref:=item.origin_project_ref;
     object_key:=item.object_key; object_version:=item.object_version; object_etag:=item.object_etag;
     digest:=item.digest; size_bytes:=item.size_bytes; generation:=item.generation; cleanup_deadline:=item.cleanup_deadline;
     RETURN NEXT;
   END LOOP;
   EXIT WHEN processed_count>=p_limit;
 END LOOP;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION control_plane.project_purge_recovery_pin(p_id uuid,p_owner text,p_generation bigint,
 p_storage_identity text,p_version text,p_etag text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE item control_plane.prepared_content%ROWTYPE;
BEGIN
 IF p_storage_identity IS NULL OR p_storage_identity !~ '^[a-f0-9]{64}$'
    OR p_version IS NULL OR char_length(p_version) NOT BETWEEN 1 AND 1024 OR p_version ~ '[\r\n]'
    OR p_etag IS NULL OR char_length(p_etag) NOT BETWEEN 1 AND 1024 OR p_etag ~ '[\r\n]' THEN
   RAISE EXCEPTION 'invalid project purge recovery receipt' USING ERRCODE='22023'; END IF;
 PERFORM 1 FROM control_plane.project_purge_receipts r JOIN control_plane.prepared_content c
   ON c.organization_id=r.organization_id AND c.origin_project_ref=r.project_ref
   WHERE c.id=p_id FOR UPDATE OF r;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT * INTO item FROM control_plane.prepared_content c WHERE c.id=p_id AND c.state='OWNER_ABANDONED'
   AND c.recovery_state='CLAIMED' AND c.cleanup_owner=p_owner AND c.generation=p_generation
   AND c.cleanup_deadline>clock_timestamp() AND c.recovery_storage_identity=p_storage_identity FOR UPDATE;
 IF NOT FOUND OR (item.object_version<>'' AND (item.object_version<>p_version OR item.object_etag<>p_etag)) THEN RETURN false; END IF;
 INSERT INTO control_plane.project_purge_recovery_context VALUES(pg_current_xact_id(),pg_backend_pid(),item.id,'PIN');
 UPDATE control_plane.prepared_content SET object_version=p_version,object_etag=p_etag,updated_at=clock_timestamp() WHERE id=item.id;
 DELETE FROM control_plane.project_purge_recovery_context WHERE transaction_id=pg_current_xact_id()
   AND backend_pid=pg_backend_pid() AND ledger_id=item.id;
 UPDATE control_plane.project_purge_receipts SET recovery_version=recovery_version+1
   WHERE organization_id=item.organization_id AND project_ref=item.origin_project_ref;
 RETURN true;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION control_plane.project_purge_recovery_finish(p_id uuid,p_owner text,p_generation bigint,
 p_storage_identity text,p_outcome text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE item control_plane.prepared_content%ROWTYPE;
BEGIN
 IF p_storage_identity IS NULL OR p_storage_identity !~ '^[a-f0-9]{64}$'
    OR p_outcome IS NULL OR p_outcome NOT IN ('ABSENT','DELETED','FAILED') THEN
   RAISE EXCEPTION 'invalid project purge recovery outcome' USING ERRCODE='22023'; END IF;
 PERFORM 1 FROM control_plane.project_purge_receipts r JOIN control_plane.prepared_content c
   ON c.organization_id=r.organization_id AND c.origin_project_ref=r.project_ref
   WHERE c.id=p_id FOR UPDATE OF r;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT * INTO item FROM control_plane.prepared_content c WHERE c.id=p_id AND c.state='OWNER_ABANDONED'
   AND c.recovery_state='CLAIMED' AND c.cleanup_owner=p_owner AND c.generation=p_generation
   AND c.cleanup_deadline>clock_timestamp() AND c.recovery_storage_identity=p_storage_identity FOR UPDATE;
 IF NOT FOUND THEN RETURN false; END IF;
 IF (p_outcome='ABSENT' AND (item.object_version<>'' OR item.object_etag<>'')) OR
    (p_outcome='DELETED' AND (item.object_version='' OR item.object_etag='')) THEN RETURN false; END IF;
 INSERT INTO control_plane.project_purge_recovery_context VALUES(pg_current_xact_id(),pg_backend_pid(),item.id,'FINISH');
 UPDATE control_plane.prepared_content SET recovery_state=CASE p_outcome
     WHEN 'ABSENT' THEN 'OBSERVING' WHEN 'DELETED' THEN 'DELETED_CONFIRMED'
     ELSE CASE WHEN cleanup_attempts>=3 THEN 'WAITING_OWNER' ELSE 'PENDING' END END,
   cleanup_attempts=CASE WHEN p_outcome='ABSENT' THEN 0 ELSE cleanup_attempts END,
   recovery_absent_at=CASE WHEN p_outcome='ABSENT' THEN clock_timestamp() ELSE recovery_absent_at END,
   recovery_check_after=clock_timestamp()+interval '1 minute',cleanup_owner=NULL,cleanup_deadline=NULL,updated_at=clock_timestamp()
   WHERE id=item.id;
 DELETE FROM control_plane.project_purge_recovery_context WHERE transaction_id=pg_current_xact_id()
   AND backend_pid=pg_backend_pid() AND ledger_id=item.id;
 UPDATE control_plane.project_purge_receipts SET recovery_version=recovery_version+1
   WHERE organization_id=item.organization_id AND project_ref=item.origin_project_ref;
 RETURN true;
END;
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION control_plane.project_purge_recovery_resolve(uuid,text,bigint,text,uuid,text,text),
 control_plane.project_purge_recovery_claim(text,integer,text),control_plane.project_purge_recovery_pin(uuid,text,bigint,text,text,text),
 control_plane.project_purge_recovery_finish(uuid,text,bigint,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.project_purge_recovery_resolve(uuid,text,bigint,text,uuid,text,text),
 control_plane.project_purge_recovery_claim(text,integer,text),control_plane.project_purge_recovery_pin(uuid,text,bigint,text,text,text),
 control_plane.project_purge_recovery_finish(uuid,text,bigint,text,text) TO control_plane_runtime;

-- Сохраняем прежние retention guards и расширяем лишь точный protected
-- project-purge путь. Таблицы recovery не добавляют FK в удаляемый граф.
-- +goose StatementBegin
CREATE FUNCTION control_plane.project_purge_recovery_prepare_purge(p_organization_id uuid,p_project_id uuid,p_objects_digest text)
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE item control_plane.prepared_content%ROWTYPE;
BEGIN
 IF NOT EXISTS (
   SELECT 1 FROM control_plane.project_purge_context context
   JOIN control_plane.project_purge_receipts receipt ON receipt.project_id=context.project_id
   JOIN control_plane.projects project ON project.id=receipt.project_id AND project.organization_id=receipt.organization_id
   WHERE context.transaction_id=pg_current_xact_id_if_assigned() AND context.backend_pid=pg_backend_pid()
     AND context.project_id=p_project_id AND receipt.organization_id=p_organization_id
     AND receipt.state='OBJECTS_CLEARED' AND receipt.objects_digest=p_objects_digest
     AND project.lifecycle='PURGE_PENDING'
 ) OR control_plane.project_purge_inventory_digest(p_organization_id,p_project_id) IS DISTINCT FROM p_objects_digest THEN
   RAISE EXCEPTION 'prepared project purge proof is invalid' USING ERRCODE='42501'; END IF;
 PERFORM 1 FROM control_plane.prepared_content content
   WHERE content.organization_id=p_organization_id AND content.project_id=p_project_id ORDER BY content.id FOR UPDATE;
 IF EXISTS (SELECT 1 FROM control_plane.prepared_content content
     WHERE content.organization_id=p_organization_id AND content.project_id=p_project_id
       AND content.state NOT IN ('ADOPTED','CLEANED') AND NOT
         (content.state='OWNER_ABANDONED' AND content.owner_resolution IS NOT NULL
          AND (content.recovery_state='DELETED_CONFIRMED' OR
            (content.recovery_state='OBSERVING' AND content.recovery_absent_at IS NOT NULL)))) THEN
   RAISE EXCEPTION 'prepared content cleanup must finish before project purge' USING ERRCODE='P0001'; END IF;
 FOR item IN SELECT content.* FROM control_plane.prepared_content content
   WHERE content.organization_id=p_organization_id AND content.project_id=p_project_id ORDER BY content.id
 LOOP
   INSERT INTO control_plane.project_purge_recovery_context VALUES(pg_current_xact_id(),pg_backend_pid(),item.id,'SCRUB');
   IF item.state='ADOPTED' AND item.deletion_receipt_digest IS NULL THEN
     IF NOT EXISTS (
       SELECT 1 FROM control_plane.artifact_revisions revision
       JOIN control_plane.artifact_heads head ON head.id=revision.artifact_id
       JOIN control_plane.artifact_revision_content content ON content.revision_id=revision.id
       WHERE revision.id=item.adopted_revision_id AND revision.ref=item.adopted_revision_ref
         AND head.ref=item.prepared_artifact_ref AND head.organization_id=p_organization_id AND head.project_id=p_project_id
         AND content.object_key=item.object_key AND content.object_version=item.object_version
         AND content.object_etag=item.object_etag AND content.digest=item.digest AND content.size_bytes=item.size_bytes
         AND revision.digest=item.digest AND revision.size_bytes=item.size_bytes
     ) THEN RAISE EXCEPTION 'prepared adopted project purge binding is invalid' USING ERRCODE='P0001'; END IF;
     UPDATE control_plane.prepared_content SET ref=NULL,file_name=NULL,media_type=NULL,digest=NULL,size_bytes=NULL,
       scan_state=NULL,preview_state=NULL,object_key=NULL,object_version=NULL,object_etag=NULL,adopted_revision_id=NULL,
       deletion_receipt_digest=encode(public.digest(convert_to(jsonb_build_object('kind','PROJECT_PURGE','ledger',item.id,
         'revision',item.adopted_revision_ref,'project',p_project_id,'objectsDigest',p_objects_digest,
         'objectVersion',item.object_version,'objectETag',item.object_etag)::text,'UTF8'),'sha256'),'hex'),
       updated_at=clock_timestamp() WHERE id=item.id;
   ELSIF item.state='CLEANED' AND item.deletion_receipt_digest IS NULL THEN
     UPDATE control_plane.prepared_content SET ref=NULL,file_name=NULL,media_type=NULL,digest=NULL,size_bytes=NULL,
       scan_state=NULL,preview_state=NULL,object_key=NULL,object_version=NULL,object_etag=NULL,
       deletion_receipt_digest=encode(public.digest(convert_to(jsonb_build_object('ledger',item.id,
         'generation',item.generation,'state',item.state)::text,'UTF8'),'sha256'),'hex'),updated_at=clock_timestamp() WHERE id=item.id;
   END IF;
 END LOOP;
 UPDATE control_plane.prepared_content_bindings binding SET plan_id=NULL,plan_revision_id=NULL
   FROM control_plane.prepared_content content WHERE binding.ledger_id=content.id
     AND content.organization_id=p_organization_id AND content.project_id=p_project_id;
 UPDATE control_plane.prepared_content SET project_id=NULL,target_artifact_id=NULL,plan_id=NULL,plan_revision_id=NULL,
   updated_at=clock_timestamp() WHERE organization_id=p_organization_id AND project_id=p_project_id
     AND state IN ('ADOPTED','CLEANED','OWNER_ABANDONED');
 DELETE FROM control_plane.project_purge_recovery_context WHERE transaction_id=pg_current_xact_id()
   AND backend_pid=pg_backend_pid() AND purpose='SCRUB';
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.project_purge_recovery_prepare_purge(uuid,uuid,text) FROM PUBLIC,control_plane_runtime;

-- +goose StatementBegin
DO $migration$
DECLARE definition text; marker text; replacement text; first_position integer; final_position integer;
BEGIN
 SELECT pg_get_functiondef('control_plane.protect_prepared_content_binding()'::regprocedure) INTO definition;
 marker:='content.state NOT IN (''ADOPTED'',''CLEANED'')';
 IF strpos(definition,marker)=0 THEN RAISE EXCEPTION 'prepared recovery binding guard precondition failed'; END IF;
 EXECUTE replace(definition,marker,
   '(content.state NOT IN (''ADOPTED'',''CLEANED'') AND NOT (content.state=''OWNER_ABANDONED'' AND content.owner_resolution IS NOT NULL AND (content.recovery_state=''DELETED_CONFIRMED'' OR (content.recovery_state=''OBSERVING'' AND content.recovery_absent_at IS NOT NULL))))');
 SELECT pg_get_functiondef('control_plane.guard_prepared_content_purge()'::regprocedure) INTO definition;
 IF strpos(definition,marker)=0 THEN RAISE EXCEPTION 'prepared recovery purge guard precondition failed'; END IF;
 EXECUTE replace(definition,marker,
   '(content.state NOT IN (''ADOPTED'',''CLEANED'') AND NOT (content.state=''OWNER_ABANDONED'' AND content.owner_resolution IS NOT NULL AND (content.recovery_state=''DELETED_CONFIRMED'' OR (content.recovery_state=''OBSERVING'' AND content.recovery_absent_at IS NOT NULL))))');
 SELECT pg_get_functiondef('control_plane.purge_project_database(uuid,uuid,text)'::regprocedure) INTO definition;
 marker:='    IF EXISTS (SELECT 1 FROM control_plane.prepared_content content WHERE content.project_id=p_project_id';
 first_position:=strpos(definition,marker);
 marker:='       WHERE project_id=p_project_id AND state IN (''ADOPTED'',''CLEANED'');';
 final_position:=strpos(definition,marker);
 IF first_position=0 OR final_position<=first_position THEN RAISE EXCEPTION 'prepared recovery project purge precondition failed'; END IF;
 replacement:='    PERFORM control_plane.project_purge_recovery_prepare_purge(p_organization_id,p_project_id,p_objects_digest);';
 definition:=substring(definition FROM 1 FOR first_position-1) || replacement ||
   substring(definition FROM final_position+char_length(marker));
 EXECUTE definition;
END $migration$;
-- +goose StatementEnd

-- Runtime не может сам назначить resolution/recovery version вне owner API.
-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_project_purge_recovery_receipt() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF TG_OP='DELETE' THEN
   RAISE EXCEPTION 'project purge recovery receipt cannot be deleted' USING ERRCODE='42501'; END IF;
 IF TG_OP='INSERT' THEN
   IF NEW.recovery_version IS DISTINCT FROM 1 OR NEW.recovery_resolution_ref IS NOT NULL
      OR NEW.recovery_resolution_digest IS NOT NULL OR NEW.recovery_resolution_actor IS NOT NULL
      OR NEW.state IS DISTINCT FROM 'PENDING' OR NEW.objects_digest IS NOT NULL
      OR NEW.objects_cleared_at IS NOT NULL OR NEW.purged_at IS NOT NULL OR NEW.deleted_rows IS DISTINCT FROM 0 THEN
     RAISE EXCEPTION 'project purge recovery receipt initial state is protected' USING ERRCODE='42501'; END IF;
   RETURN NEW;
 END IF;
 IF NEW.state=OLD.state THEN
   IF (to_jsonb(NEW)-ARRAY['recovery_version','recovery_resolution_ref','recovery_resolution_digest','recovery_resolution_actor']) IS DISTINCT FROM
      (to_jsonb(OLD)-ARRAY['recovery_version','recovery_resolution_ref','recovery_resolution_digest','recovery_resolution_actor']) THEN
     RAISE EXCEPTION 'project purge recovery receipt origin and proof are immutable' USING ERRCODE='42501'; END IF;
 ELSIF OLD.state='PENDING' AND NEW.state='OBJECTS_CLEARED' THEN
   IF (to_jsonb(NEW)-ARRAY['state','objects_digest','objects_cleared_at']) IS DISTINCT FROM
      (to_jsonb(OLD)-ARRAY['state','objects_digest','objects_cleared_at']) THEN
     RAISE EXCEPTION 'project purge recovery external proof transition is invalid' USING ERRCODE='42501'; END IF;
 ELSIF OLD.state='OBJECTS_CLEARED' AND NEW.state='DONE' AND current_user='control_plane_owner' AND EXISTS (
   SELECT 1 FROM control_plane.project_purge_context context
   WHERE context.transaction_id=pg_current_xact_id_if_assigned() AND context.backend_pid=pg_backend_pid()
     AND context.project_id=OLD.project_id) THEN
   IF (to_jsonb(NEW)-ARRAY['state','purged_at','deleted_rows']) IS DISTINCT FROM
      (to_jsonb(OLD)-ARRAY['state','purged_at','deleted_rows']) THEN
     RAISE EXCEPTION 'project purge recovery completion transition is invalid' USING ERRCODE='42501'; END IF;
 ELSE RAISE EXCEPTION 'project purge recovery receipt transition is invalid' USING ERRCODE='42501';
 END IF;
 IF (NEW.recovery_version,NEW.recovery_resolution_ref,NEW.recovery_resolution_digest,NEW.recovery_resolution_actor)
    IS DISTINCT FROM (OLD.recovery_version,OLD.recovery_resolution_ref,OLD.recovery_resolution_digest,OLD.recovery_resolution_actor)
    AND current_user<>'control_plane_owner' THEN
   RAISE EXCEPTION 'project purge recovery receipt is protected' USING ERRCODE='42501'; END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER protect_project_purge_recovery_receipt BEFORE INSERT OR UPDATE OR DELETE ON control_plane.project_purge_receipts
 FOR EACH ROW EXECUTE FUNCTION control_plane.protect_project_purge_recovery_receipt();
REVOKE DELETE ON control_plane.project_purge_receipts FROM control_plane_runtime;

RESET ROLE;

-- +goose Down
-- Forward-only: owner decision и surviving obligation не удаляются.
SELECT 1;
