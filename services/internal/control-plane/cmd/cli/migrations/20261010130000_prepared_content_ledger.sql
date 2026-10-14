-- +goose Up
SET ROLE control_plane_owner;

-- Частный ledger принадлежит CP. Тело отсутствует; intent переживает любой
-- неизвестный исход единственного Put и никогда не открывает повторную запись.
CREATE TABLE control_plane.prepared_content (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text UNIQUE CHECK (ref ~ '^pfcnt_[A-Za-z0-9_-]{8,80}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid REFERENCES control_plane.projects(id) ON DELETE SET NULL,
    origin_project_ref text NOT NULL,
    actor_id uuid NOT NULL REFERENCES control_plane.subjects(id),
    intent_operation text NOT NULL CHECK (char_length(intent_operation) BETWEEN 1 AND 128),
    idempotency_key text NOT NULL CHECK (char_length(idempotency_key) BETWEEN 1 AND 200),
    intent_digest text NOT NULL CHECK (intent_digest ~ '^[a-f0-9]{64}$'),
    request_digest text NOT NULL CHECK (request_digest ~ '^[a-f0-9]{64}$'),
    operation_key text NOT NULL CHECK (char_length(operation_key) BETWEEN 1 AND 96),
    source_profile text NOT NULL CHECK (source_profile IN ('SYSTEM','PROJECT')),
    source_profile_ref text NOT NULL CHECK (char_length(source_profile_ref) BETWEEN 1 AND 96),
    source_profile_version bigint NOT NULL CHECK (source_profile_version > 0),
    source_context_digest text NOT NULL CHECK (source_context_digest ~ '^[a-f0-9]{64}$'),
    source_lease_ref text NOT NULL CHECK (char_length(source_lease_ref) BETWEEN 1 AND 96),
    source_lease_generation bigint NOT NULL CHECK (source_lease_generation > 0),
    source_fence_digest text NOT NULL CHECK (source_fence_digest ~ '^[a-f0-9]{64}$'),
    source_run_ref text NOT NULL CHECK (char_length(source_run_ref) BETWEEN 1 AND 96),
    target_artifact_id uuid REFERENCES control_plane.artifact_heads(id) ON DELETE SET NULL,
    target_artifact_ref text NOT NULL,
    source_revision_ref text NOT NULL DEFAULT '',
    expected_artifact_version bigint NOT NULL DEFAULT 0 CHECK (expected_artifact_version >= 0),
    prepared_artifact_ref text NOT NULL,
    prepared_revision_ref text NOT NULL,
    file_name text CHECK (char_length(file_name) BETWEEN 1 AND 255),
    media_type text CHECK (char_length(media_type) BETWEEN 1 AND 160),
    digest text CHECK (digest ~ '^sha256:[a-f0-9]{64}$'),
    size_bytes bigint CHECK (size_bytes BETWEEN 0 AND 1048576),
    scan_state text CHECK (scan_state='CLEAN'),
    preview_state text CHECK (preview_state IN ('AVAILABLE','UNAVAILABLE','BLOCKED')),
    object_key text UNIQUE CHECK (char_length(object_key) BETWEEN 1 AND 1024),
    object_version text DEFAULT '',
    object_etag text DEFAULT '',
    deletion_receipt_digest text CHECK (deletion_receipt_digest ~ '^[a-f0-9]{64}$'),
    state text NOT NULL DEFAULT 'IN_FLIGHT' CHECK (state IN
        ('IN_FLIGHT','UNKNOWN','STAGED','ADOPTED','ABANDONED','CLEANING','CLEANED','WAITING_OWNER')),
    generation bigint NOT NULL DEFAULT 1 CHECK (generation > 0),
    writer_deadline timestamptz NOT NULL DEFAULT clock_timestamp()+interval '2 minutes',
    available_until timestamptz NOT NULL DEFAULT clock_timestamp()+interval '24 hours',
    plan_id uuid REFERENCES control_plane.assistant_plans(id) ON DELETE SET NULL,
    plan_revision_id uuid REFERENCES control_plane.assistant_plan_revisions(id) ON DELETE SET NULL,
    plan_revision bigint,
    origin_plan_ref text,
    origin_conversation_ref text,
    origin_plan_revision_ref text,
    origin_plan_revision bigint,
    adopted_revision_id uuid REFERENCES control_plane.artifact_revisions(id) ON DELETE SET NULL,
    adopted_revision_ref text,
    cleanup_owner text,
    cleanup_deadline timestamptz,
    cleanup_attempts integer NOT NULL DEFAULT 0 CHECK (cleanup_attempts BETWEEN 0 AND 3),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id,actor_id,intent_operation,idempotency_key,operation_key),
    CHECK (plan_revision IS NULL OR plan_revision>0),
    CHECK (state<>'ADOPTED' OR adopted_revision_ref IS NOT NULL),
    CHECK (state NOT IN ('STAGED','ADOPTED') OR deletion_receipt_digest IS NOT NULL OR (object_version<>'' AND object_etag<>'')),
    CONSTRAINT prepared_content_terminal_mask CHECK (
      (deletion_receipt_digest IS NULL AND ref IS NOT NULL AND file_name IS NOT NULL AND media_type IS NOT NULL
       AND digest IS NOT NULL AND size_bytes IS NOT NULL AND scan_state IS NOT NULL AND preview_state IS NOT NULL
       AND object_key IS NOT NULL AND object_version IS NOT NULL AND object_etag IS NOT NULL) OR
      (deletion_receipt_digest IS NOT NULL AND state IN ('ADOPTED','CLEANED')
       AND ref IS NULL AND file_name IS NULL AND media_type IS NULL AND digest IS NULL AND size_bytes IS NULL
       AND scan_state IS NULL AND preview_state IS NULL AND object_key IS NULL AND object_version IS NULL AND object_etag IS NULL)),
    CHECK ((state='CLEANING')=(cleanup_owner IS NOT NULL AND cleanup_deadline IS NOT NULL))
);
CREATE INDEX prepared_content_due ON control_plane.prepared_content
    (available_until,writer_deadline,id) WHERE state NOT IN ('ADOPTED','CLEANED','WAITING_OWNER');

CREATE TABLE control_plane.prepared_content_bindings (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ledger_id uuid NOT NULL REFERENCES control_plane.prepared_content(id),
    plan_id uuid REFERENCES control_plane.assistant_plans(id) ON DELETE SET NULL,
    plan_revision_id uuid REFERENCES control_plane.assistant_plan_revisions(id) ON DELETE SET NULL,
    plan_ref text NOT NULL,
    plan_revision_ref text NOT NULL,
    plan_revision bigint NOT NULL CHECK (plan_revision>0),
    operation_key text NOT NULL,
    UNIQUE (ledger_id,plan_ref,plan_revision,operation_key)
);
-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_prepared_content_binding() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE content control_plane.prepared_content%ROWTYPE;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'prepared content binding is immutable'; END IF;
 SELECT * INTO STRICT content FROM control_plane.prepared_content WHERE id=OLD.ledger_id;
 IF (to_jsonb(OLD)-ARRAY['plan_id','plan_revision_id']) IS DISTINCT FROM (to_jsonb(NEW)-ARRAY['plan_id','plan_revision_id']) OR
   (OLD.plan_id IS DISTINCT FROM NEW.plan_id AND NEW.plan_id IS NOT NULL) OR
   (OLD.plan_revision_id IS DISTINCT FROM NEW.plan_revision_id AND NEW.plan_revision_id IS NOT NULL) OR
   content.state NOT IN ('ADOPTED','CLEANED') OR NOT
   (EXISTS (SELECT 1 FROM control_plane.project_purge_context context WHERE
    context.transaction_id=pg_current_xact_id_if_assigned() AND context.backend_pid=pg_backend_pid() AND context.project_id=content.project_id) OR
    EXISTS (SELECT 1 FROM control_plane.assistant_conversation_purge_context context WHERE
    context.transaction_id=pg_current_xact_id_if_assigned() AND context.backend_pid=pg_backend_pid() AND context.conversation_ref=content.origin_conversation_ref)) THEN
  RAISE EXCEPTION 'prepared content binding purge is not authorized'; END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER protect_prepared_content_binding BEFORE UPDATE OR DELETE ON control_plane.prepared_content_bindings
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_prepared_content_binding();
REVOKE UPDATE,DELETE ON control_plane.prepared_content_bindings FROM control_plane_runtime;

-- Непринятый либо неизвестный intent нельзя потерять через purge другого
-- агрегата. Состояние ADOPTED сохраняется даже после снятия точного FK.
-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_prepared_content() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
  IF TG_OP='DELETE' THEN RAISE EXCEPTION 'prepared content intent cannot be deleted'; END IF;
  -- Только подтверждённый terminal owner purge может снять весь технический
  -- content envelope. Состояние и opaque origin остаются неизменными.
  IF OLD.deletion_receipt_digest IS NULL AND NEW.deletion_receipt_digest IS NOT NULL THEN
    IF current_user<>'control_plane_owner' OR OLD.state NOT IN ('ADOPTED','CLEANED','CLEANING') OR
      NEW.state IS DISTINCT FROM (CASE WHEN OLD.state='CLEANING' THEN 'CLEANED' ELSE OLD.state END) OR
      (to_jsonb(OLD)-ARRAY['ref','file_name','media_type','digest','size_bytes','scan_state','preview_state',
       'object_key','object_version','object_etag','deletion_receipt_digest','adopted_revision_id','state','cleanup_owner','cleanup_deadline','updated_at']) IS DISTINCT FROM
      (to_jsonb(NEW)-ARRAY['ref','file_name','media_type','digest','size_bytes','scan_state','preview_state',
       'object_key','object_version','object_etag','deletion_receipt_digest','adopted_revision_id','state','cleanup_owner','cleanup_deadline','updated_at']) OR
      NEW.adopted_revision_id IS NOT NULL OR NOT
      ((OLD.state='ADOPTED' AND EXISTS (
        SELECT 1 FROM control_plane.artifact_revisions revision
        JOIN control_plane.artifact_heads head ON head.id=revision.artifact_id
        WHERE revision.ref=OLD.adopted_revision_ref AND head.ref=OLD.prepared_artifact_ref
          AND head.organization_id=OLD.organization_id AND head.lifecycle_state='PURGED'
          AND head.current_revision_id IS NULL AND head.purged_at IS NOT NULL
          AND head.retention_claim_generation>0 AND head.retention_claim_owner IS NULL AND head.retention_claim_expires_at IS NULL
          AND NOT control_plane.artifact_has_retained_revisions(head.id)
          AND NOT EXISTS (SELECT 1 FROM control_plane.artifact_revision_content receipt
           JOIN control_plane.artifact_revisions retained ON retained.id=receipt.revision_id WHERE retained.artifact_id=head.id))) OR
       (OLD.state='CLEANING' AND OLD.object_version<>'' AND OLD.object_etag<>''
        AND OLD.cleanup_owner IS NOT NULL AND OLD.cleanup_deadline>clock_timestamp()
        AND OLD.generation>1 AND OLD.cleanup_attempts BETWEEN 1 AND 3
        AND NEW.cleanup_owner IS NULL AND NEW.cleanup_deadline IS NULL) OR
       (OLD.state='CLEANED' AND OLD.object_version<>'' AND OLD.object_etag<>'' AND EXISTS (
        SELECT 1 FROM control_plane.project_purge_context context
        WHERE context.transaction_id=pg_current_xact_id_if_assigned() AND context.backend_pid=pg_backend_pid()
          AND context.project_id=OLD.project_id))) THEN
      RAISE EXCEPTION 'prepared content terminal scrub is not authorized';
    END IF;
    RETURN NEW;
  END IF;
  IF (to_jsonb(NEW)-ARRAY['project_id','target_artifact_id','plan_id','plan_revision_id','plan_revision',
      'origin_plan_ref','origin_conversation_ref','origin_plan_revision_ref','origin_plan_revision','adopted_revision_id','adopted_revision_ref',
      'state','generation','writer_deadline','object_version','object_etag','cleanup_owner','cleanup_deadline',
      'cleanup_attempts','available_until','updated_at']) IS DISTINCT FROM
     (to_jsonb(OLD)-ARRAY['project_id','target_artifact_id','plan_id','plan_revision_id','plan_revision',
      'origin_plan_ref','origin_conversation_ref','origin_plan_revision_ref','origin_plan_revision','adopted_revision_id','adopted_revision_ref',
      'state','generation','writer_deadline','object_version','object_etag','cleanup_owner','cleanup_deadline',
      'cleanup_attempts','available_until','updated_at']) OR NEW.generation<OLD.generation
     OR (OLD.state='ADOPTED' AND (NEW.state<>'ADOPTED' OR NEW.adopted_revision_ref IS DISTINCT FROM OLD.adopted_revision_ref))
     OR (OLD.origin_plan_ref IS NOT NULL AND (NEW.origin_plan_ref,NEW.origin_conversation_ref,NEW.origin_plan_revision_ref,NEW.origin_plan_revision)
       IS DISTINCT FROM (OLD.origin_plan_ref,OLD.origin_conversation_ref,OLD.origin_plan_revision_ref,OLD.origin_plan_revision)) THEN
      RAISE EXCEPTION 'prepared content origin is immutable';
  END IF;
  IF (OLD.project_id IS NOT NULL AND NEW.project_id IS NULL) OR
     (OLD.target_artifact_id IS NOT NULL AND NEW.target_artifact_id IS NULL) OR
     (OLD.plan_id IS NOT NULL AND NEW.plan_id IS NULL) OR
     (OLD.plan_revision_id IS NOT NULL AND NEW.plan_revision_id IS NULL) OR
     (OLD.adopted_revision_id IS NOT NULL AND NEW.adopted_revision_id IS NULL) THEN
    IF OLD.state NOT IN ('ADOPTED','CLEANED') OR NOT
      (EXISTS (SELECT 1 FROM control_plane.project_purge_context context
        WHERE context.transaction_id=pg_current_xact_id_if_assigned() AND context.backend_pid=pg_backend_pid()
          AND context.project_id=OLD.project_id) OR
       EXISTS (SELECT 1 FROM control_plane.assistant_conversation_purge_context context
        WHERE context.transaction_id=pg_current_xact_id_if_assigned() AND context.backend_pid=pg_backend_pid()
          AND context.conversation_ref=OLD.origin_conversation_ref)) THEN
      RAISE EXCEPTION 'prepared content purge is not terminal or authorized';
    END IF;
  END IF;
  RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER protect_prepared_content BEFORE UPDATE OR DELETE ON control_plane.prepared_content
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_prepared_content();
REVOKE DELETE ON control_plane.prepared_content FROM control_plane_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.scrub_prepared_content_revision() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE head control_plane.artifact_heads%ROWTYPE;
BEGIN
 IF TG_RELID<>'control_plane.artifact_revisions'::regclass::oid OR TG_OP<>'DELETE' THEN
   RAISE EXCEPTION 'prepared content scrub trigger target is invalid'; END IF;
 IF NOT EXISTS (SELECT 1 FROM control_plane.prepared_content content
   WHERE content.state='ADOPTED' AND (content.adopted_revision_id=OLD.id OR content.adopted_revision_ref=OLD.ref)
     AND content.deletion_receipt_digest IS NULL) THEN RETURN OLD; END IF;
 SELECT * INTO STRICT head FROM control_plane.artifact_heads WHERE id=OLD.artifact_id FOR UPDATE;
 IF head.lifecycle_state<>'PURGED' OR head.current_revision_id IS NOT NULL OR head.purged_at IS NULL
    OR head.retention_claim_generation<=0 OR head.retention_claim_owner IS NOT NULL OR head.retention_claim_expires_at IS NOT NULL
    OR control_plane.artifact_has_retained_revisions(head.id) OR EXISTS (
      SELECT 1 FROM control_plane.artifact_revision_content receipt
      JOIN control_plane.artifact_revisions revision ON revision.id=receipt.revision_id WHERE revision.artifact_id=head.id) THEN
   RAISE EXCEPTION 'prepared content revision purge is not terminal'; END IF;
 UPDATE control_plane.prepared_content content SET ref=NULL,file_name=NULL,media_type=NULL,digest=NULL,size_bytes=NULL,
   scan_state=NULL,preview_state=NULL,object_key=NULL,object_version=NULL,object_etag=NULL,adopted_revision_id=NULL,
   deletion_receipt_digest=encode(public.digest(convert_to(jsonb_build_object('ledger',content.id,'revision',OLD.ref,
     'generation',head.retention_claim_generation,'purged_at',head.purged_at)::text,'UTF8'),'sha256'),'hex'),
   updated_at=clock_timestamp()
   WHERE content.state='ADOPTED' AND content.adopted_revision_ref=OLD.ref AND content.organization_id=head.organization_id
     AND content.prepared_artifact_ref=head.ref AND content.deletion_receipt_digest IS NULL;
 RETURN OLD;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.scrub_prepared_content_revision() FROM PUBLIC;
CREATE TRIGGER scrub_prepared_content_revision BEFORE DELETE ON control_plane.artifact_revisions
FOR EACH ROW EXECUTE FUNCTION control_plane.scrub_prepared_content_revision();

-- +goose StatementBegin
DO $migration$
DECLARE definition text; marker text; replacement text; old_nodes text; old_edges text;
 node_count integer; node_hash text; edge_count integer; edge_hash text;
BEGIN
 SELECT pg_get_functiondef('control_plane.purge_project_database(uuid,uuid,text)'::regprocedure) INTO definition;
 old_nodes:=substring(definition FROM 'IF v_nodes <> [0-9]+ OR v_node_fingerprint <> ''[a-f0-9]{32}'' THEN');
 old_edges:=substring(definition FROM 'IF v_edges <> [0-9]+ OR v_edge_fingerprint <> ''[a-f0-9]{32}'' THEN');
 IF old_nodes IS DISTINCT FROM 'IF v_nodes <> 102 OR v_node_fingerprint <> ''97624a5baaeeac387fbdb8b45a51d8b4'' THEN'
    OR old_edges IS DISTINCT FROM 'IF v_edges <> 268 OR v_edge_fingerprint <> ''ba6d7726ee03622203d9514b20914a3c'' THEN'
    THEN RAISE EXCEPTION 'prepared purge graph precondition failed'; END IF;
 WITH RECURSIVE nodes(relation_id) AS (SELECT 'control_plane.projects'::regclass::oid UNION
  SELECT c.conrelid FROM nodes JOIN pg_constraint c ON c.contype='f' AND c.confrelid=nodes.relation_id
  JOIN pg_namespace n ON n.oid=c.connamespace AND n.nspname='control_plane')
 SELECT count(*),md5(string_agg(n.nspname||'.'||c.relname,E'\n' ORDER BY n.nspname,c.relname))
 INTO node_count,node_hash FROM nodes JOIN pg_class c ON c.oid=nodes.relation_id JOIN pg_namespace n ON n.oid=c.relnamespace;
 WITH RECURSIVE nodes(relation_id) AS (SELECT 'control_plane.projects'::regclass::oid UNION
  SELECT c.conrelid FROM nodes JOIN pg_constraint c ON c.contype='f' AND c.confrelid=nodes.relation_id
  JOIN pg_namespace n ON n.oid=c.connamespace AND n.nspname='control_plane')
 SELECT count(*),md5(string_agg(cn.nspname||'.'||cc.relname||'|'||c.conname||'|'||pn.nspname||'.'||pc.relname||'|'||c.confdeltype::text,
 E'\n' ORDER BY cn.nspname,cc.relname,c.conname)) INTO edge_count,edge_hash
 FROM pg_constraint c JOIN pg_class cc ON cc.oid=c.conrelid JOIN pg_namespace cn ON cn.oid=cc.relnamespace
 JOIN pg_class pc ON pc.oid=c.confrelid JOIN pg_namespace pn ON pn.oid=pc.relnamespace
 WHERE c.contype='f' AND c.confrelid IN (SELECT relation_id FROM nodes) AND c.conrelid IN (SELECT relation_id FROM nodes);
 IF node_count<>104 OR node_hash<>'5cbc6bd6bb4a8508ffd75ed4b1dd6189'
    OR edge_count<>276 OR edge_hash<>'75343e68e299c44be699718099c79527' THEN
    RAISE EXCEPTION 'prepared purge graph fingerprint changed'; END IF;
 definition:=replace(replace(definition,old_nodes,
  format('IF v_nodes <> %s OR v_node_fingerprint <> %L THEN',node_count,node_hash)),old_edges,
  format('IF v_edges <> %s OR v_edge_fingerprint <> %L THEN',edge_count,edge_hash));
 marker:='    VALUES (v_transaction_id, pg_backend_pid(), p_project_id);';
 IF strpos(definition,marker)=0 THEN RAISE EXCEPTION 'prepared purge context precondition failed'; END IF;
 replacement:=marker || E'\n' || $guard$
    IF EXISTS (SELECT 1 FROM control_plane.prepared_content content WHERE content.project_id=p_project_id
       AND content.state NOT IN ('ADOPTED','CLEANED')) THEN
       RAISE EXCEPTION 'prepared content cleanup must finish before project purge';
    END IF;
    UPDATE control_plane.prepared_content content SET ref=NULL,file_name=NULL,media_type=NULL,digest=NULL,size_bytes=NULL,
       scan_state=NULL,preview_state=NULL,object_key=NULL,object_version=NULL,object_etag=NULL,
       deletion_receipt_digest=encode(public.digest(convert_to(jsonb_build_object('ledger',content.id,
         'generation',content.generation,'state',content.state)::text,'UTF8'),'sha256'),'hex'),updated_at=clock_timestamp()
       WHERE content.project_id=p_project_id AND content.state='CLEANED' AND content.deletion_receipt_digest IS NULL;
    UPDATE control_plane.prepared_content_bindings binding SET plan_id=NULL,plan_revision_id=NULL
       FROM control_plane.prepared_content content WHERE binding.ledger_id=content.id AND content.project_id=p_project_id;
    UPDATE control_plane.prepared_content SET project_id=NULL,target_artifact_id=NULL,
       plan_id=NULL,plan_revision_id=NULL,updated_at=clock_timestamp()
       WHERE project_id=p_project_id AND state IN ('ADOPTED','CLEANED');
 $guard$;
 EXECUTE replace(definition,marker,replacement);
END $migration$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_prepared_content_purge() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
  IF EXISTS (SELECT 1 FROM control_plane.prepared_content content
    WHERE content.state NOT IN ('ADOPTED','CLEANED') AND
     ((TG_TABLE_NAME='projects' AND content.project_id=OLD.id) OR
      (TG_TABLE_NAME='assistant_plans' AND content.plan_id=OLD.id) OR
      (TG_TABLE_NAME='assistant_plan_revisions' AND content.plan_revision_id=OLD.id) OR
      (TG_TABLE_NAME='assistant_conversations' AND content.plan_id IN
        (SELECT plan.id FROM control_plane.assistant_plans plan WHERE plan.conversation_ref=OLD.ref)))) THEN
    RAISE EXCEPTION 'prepared content cleanup must finish before purge';
  END IF;
  RETURN OLD;
END $$;
-- +goose StatementEnd
CREATE TRIGGER guard_prepared_content_project_purge BEFORE DELETE ON control_plane.projects
FOR EACH ROW EXECUTE FUNCTION control_plane.guard_prepared_content_purge();
CREATE TRIGGER guard_prepared_content_plan_purge BEFORE DELETE ON control_plane.assistant_plans
FOR EACH ROW EXECUTE FUNCTION control_plane.guard_prepared_content_purge();
CREATE TRIGGER guard_prepared_content_plan_revision_purge BEFORE DELETE ON control_plane.assistant_plan_revisions
FOR EACH ROW EXECUTE FUNCTION control_plane.guard_prepared_content_purge();
CREATE TRIGGER guard_prepared_content_conversation_purge BEFORE DELETE ON control_plane.assistant_conversations
FOR EACH ROW EXECUTE FUNCTION control_plane.guard_prepared_content_purge();

-- Замкнутая API cleanup не выдаёт worker прямую запись в планы либо ledger.
-- +goose StatementBegin
CREATE FUNCTION control_plane.prepared_content_claim(p_owner text,p_limit integer,p_lease integer)
RETURNS TABLE(ledger_id uuid,object_key text,object_version text,object_etag text,
    digest text,size_bytes bigint,generation bigint,uncertain boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE item control_plane.prepared_content%ROWTYPE;
BEGIN
    IF char_length(p_owner) NOT BETWEEN 1 AND 128 OR p_limit NOT BETWEEN 1 AND 100
       OR p_lease NOT BETWEEN 1 AND 600 THEN RAISE EXCEPTION 'invalid prepared cleanup claim'; END IF;
    FOR item IN SELECT candidate.* FROM control_plane.prepared_content candidate
      WHERE candidate.state IN ('IN_FLIGHT','UNKNOWN','STAGED','ABANDONED','CLEANING')
        AND candidate.adopted_revision_id IS NULL
        AND candidate.writer_deadline<=clock_timestamp()
        AND (candidate.state IN ('IN_FLIGHT','UNKNOWN','ABANDONED') OR
             candidate.available_until<=clock_timestamp() OR
             (candidate.state='CLEANING' AND candidate.cleanup_deadline<=clock_timestamp()) OR
             EXISTS (SELECT 1 FROM control_plane.assistant_plans plan WHERE plan.id=candidate.plan_id
               AND (plan.state IN ('STALE','REJECTED','APPLIED') OR plan.current_revision<>candidate.plan_revision)))
        AND (candidate.state<>'CLEANING' OR candidate.cleanup_deadline<=clock_timestamp())
      ORDER BY candidate.available_until,candidate.id LIMIT p_limit FOR UPDATE SKIP LOCKED
    LOOP
      -- Нельзя сделать terminal план новой ревизии из-за старого staged content.
      UPDATE control_plane.assistant_plans plan SET state='STALE',validated_revision=NULL,
        validated_at=NULL,version=plan.version+1
        WHERE plan.id=item.plan_id AND plan.current_revision=item.plan_revision
          AND plan.state IN ('DRAFT','VALID','INVALID');
      IF item.cleanup_attempts>=3 THEN
        UPDATE control_plane.prepared_content SET state='WAITING_OWNER',cleanup_owner=NULL,
          cleanup_deadline=NULL,generation=prepared_content.generation+1,updated_at=clock_timestamp()
          WHERE id=item.id;
        CONTINUE;
      END IF;
      UPDATE control_plane.prepared_content SET state='CLEANING',cleanup_owner=p_owner,
        cleanup_deadline=clock_timestamp()+p_lease*interval '1 second',
        cleanup_attempts=prepared_content.cleanup_attempts+1,generation=prepared_content.generation+1,
        updated_at=clock_timestamp() WHERE id=item.id;
      ledger_id:=item.id; object_key:=item.object_key; object_version:=item.object_version;
      object_etag:=item.object_etag; digest:=item.digest; size_bytes:=item.size_bytes;
      generation:=item.generation+1; uncertain:=item.object_version=''; RETURN NEXT;
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION control_plane.prepared_content_finish(p_id uuid,p_owner text,p_generation bigint,
    p_success boolean,p_version text,p_etag text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE item control_plane.prepared_content%ROWTYPE;
BEGIN
    SELECT candidate.* INTO item FROM control_plane.prepared_content candidate
      WHERE candidate.id=p_id AND candidate.state='CLEANING' AND candidate.cleanup_owner=p_owner
        AND candidate.generation=p_generation AND candidate.cleanup_deadline>clock_timestamp()
        AND candidate.adopted_revision_id IS NULL FOR UPDATE;
    IF NOT FOUND THEN RETURN false; END IF;
    IF p_success AND (p_version='' OR p_etag='' OR item.object_version='' OR item.object_etag='' OR
       item.object_version<>p_version OR item.object_etag<>p_etag)
       THEN RAISE EXCEPTION 'invalid prepared cleanup receipt'; END IF;
    UPDATE control_plane.prepared_content SET
      state=CASE WHEN p_success THEN 'CLEANED' WHEN cleanup_attempts>=3 THEN 'WAITING_OWNER' ELSE 'ABANDONED' END,
      ref=CASE WHEN p_success THEN NULL ELSE prepared_content.ref END,
      file_name=CASE WHEN p_success THEN NULL ELSE prepared_content.file_name END,
      media_type=CASE WHEN p_success THEN NULL ELSE prepared_content.media_type END,
      digest=CASE WHEN p_success THEN NULL ELSE prepared_content.digest END,
      size_bytes=CASE WHEN p_success THEN NULL ELSE prepared_content.size_bytes END,
      scan_state=CASE WHEN p_success THEN NULL ELSE prepared_content.scan_state END,
      preview_state=CASE WHEN p_success THEN NULL ELSE prepared_content.preview_state END,
      object_key=CASE WHEN p_success THEN NULL ELSE prepared_content.object_key END,
      object_version=CASE WHEN p_success THEN NULL ELSE prepared_content.object_version END,
      object_etag=CASE WHEN p_success THEN NULL ELSE prepared_content.object_etag END,
      deletion_receipt_digest=CASE WHEN p_success THEN encode(public.digest(convert_to(jsonb_build_object(
        'ledger',item.id,'generation',item.generation,'owner',p_owner,'version',p_version,'etag',p_etag)::text,'UTF8'),'sha256'),'hex')
        ELSE prepared_content.deletion_receipt_digest END,
      cleanup_owner=NULL,cleanup_deadline=NULL,updated_at=clock_timestamp()
      WHERE id=p_id;
    RETURN true;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION control_plane.prepared_content_record_receipt(p_id uuid,p_owner text,p_generation bigint,
    p_key text,p_version text,p_etag text,p_digest text,p_size bigint)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
  IF p_version='' OR p_etag='' THEN RETURN false; END IF;
  UPDATE control_plane.prepared_content content SET object_version=p_version,object_etag=p_etag,updated_at=clock_timestamp()
    WHERE content.id=p_id AND content.state='CLEANING' AND content.cleanup_owner=p_owner
      AND content.generation=p_generation AND content.cleanup_deadline>clock_timestamp()
      AND content.adopted_revision_id IS NULL AND content.object_key=p_key AND content.digest=p_digest AND content.size_bytes=p_size
      AND (content.object_version='' OR (content.object_version=p_version AND content.object_etag=p_etag));
  RETURN FOUND;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.prepared_content_claim(text,integer,integer),
    control_plane.prepared_content_finish(uuid,text,bigint,boolean,text,text),
    control_plane.prepared_content_record_receipt(uuid,text,bigint,text,text,text,text,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.prepared_content_claim(text,integer,integer),
    control_plane.prepared_content_finish(uuid,text,bigint,boolean,text,text),
    control_plane.prepared_content_record_receipt(uuid,text,bigint,text,text,text,text,bigint) TO artifact_retention_runtime;
RESET ROLE;

-- +goose Down
-- Forward-only: ledger и immutable origin сохраняются после любого исхода.
SELECT 1;
