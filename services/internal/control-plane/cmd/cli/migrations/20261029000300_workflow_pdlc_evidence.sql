-- +goose Up
SET LOCAL lock_timeout='8s';
SET LOCAL statement_timeout='30s';

-- Новая политика не переписывает опубликованные версии, историю либо grants.
-- До полного consumer rollout новый профиль в staging не включается.
-- +goose StatementBegin
DO $predecessor$
BEGIN
 IF (SELECT max(version_id) FROM public.goose_db_version WHERE is_applied) IS DISTINCT FROM 20261029000200::bigint THEN
    RAISE EXCEPTION 'workflow PDLC requires exact migration030';
 END IF;
END $predecessor$;
-- +goose StatementEnd
SET ROLE control_plane_owner;

-- +goose StatementBegin
DO $preimage$
BEGIN
 IF md5((SELECT prosrc FROM pg_proc WHERE oid='control_plane.guard_workflow_effect_records()'::regprocedure)) IS DISTINCT FROM 'c18ed66f125d8a507cf832aeae0a664e'
    OR md5((SELECT prosrc FROM pg_proc WHERE oid='control_plane.guard_workflow_attempt_journal()'::regprocedure)) IS DISTINCT FROM '7f8dc9469eb755a7b7760e8d4d7fc2cd'
    OR md5((SELECT prosrc FROM pg_proc WHERE oid='control_plane.workflow_public_attempt_results(uuid,uuid)'::regprocedure)) IS DISTINCT FROM '80b6457cc872824d87164494cd44dc1a' THEN
    RAISE EXCEPTION 'workflow PDLC predecessor fingerprint changed';
 END IF;
END $preimage$;
-- +goose StatementEnd

-- Чистые hash-функции задают общий Go/SQL формат, а не полномочия.
-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_pdlc_file_key(p_result_key text,p_path text)
RETURNS text LANGUAGE sql IMMUTABLE SECURITY INVOKER SET search_path=pg_catalog AS $$
 SELECT 'file_'||encode(sha256(convert_to(p_result_key,'UTF8')||decode('00','hex')||convert_to(p_path,'UTF8')),'hex');
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_pdlc_file_desired_digest(p_result_key text,p_scope_digest text,p_input jsonb)
RETURNS text LANGUAGE plpgsql IMMUTABLE SECURITY INVOKER SET search_path=pg_catalog AS $$
DECLARE v_content bytea;v_zero bytea:=decode('00','hex');v_base64 text;
BEGIN
 IF jsonb_typeof(p_input) IS DISTINCT FROM 'object' OR NOT p_input ?& ARRAY['path','branch','content_base64']
    OR jsonb_typeof(p_input->'path') IS DISTINCT FROM 'string' OR jsonb_typeof(p_input->'branch') IS DISTINCT FROM 'string'
    OR jsonb_typeof(p_input->'content_base64') IS DISTINCT FROM 'string' THEN RETURN NULL; END IF;
 v_base64:=p_input->>'content_base64';
 IF octet_length(v_base64)>393216 OR v_base64 !~ '^[A-Za-z0-9+/=\r\n]*$' THEN RETURN NULL; END IF;
 v_base64:=replace(replace(v_base64,chr(10),''),chr(13),'');
 IF octet_length(v_base64)>349528 THEN RETURN NULL; END IF;
 BEGIN
    v_content:=decode(v_base64,'base64');
    PERFORM convert_from(v_content,'UTF8');
 EXCEPTION WHEN data_exception THEN RETURN NULL;
 END;
 IF octet_length(v_content)>262144 OR replace(encode(v_content,'base64'),chr(10),'') IS DISTINCT FROM v_base64 THEN RETURN NULL; END IF;
 RETURN encode(sha256(convert_to('kodex.workflow-file-intent.v1','UTF8')||v_zero||convert_to(p_result_key,'UTF8')||v_zero||convert_to(p_scope_digest,'UTF8')||v_zero
    ||convert_to(p_input->>'path','UTF8')||v_zero||convert_to(p_input->>'branch','UTF8')||v_zero||convert_to(encode(sha256(v_content),'hex'),'UTF8')),'hex');
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_pdlc_file_epoch_digest(p_desired text,p_predecessor text)
RETURNS text LANGUAGE sql IMMUTABLE SECURITY INVOKER SET search_path=pg_catalog AS $$
 SELECT encode(sha256(convert_to('kodex.workflow-file-epoch.v1','UTF8')||decode('00','hex')||convert_to(p_desired,'UTF8')||decode('00','hex')||convert_to(p_predecessor,'UTF8')),'hex');
$$;
-- +goose StatementEnd

-- Общий закрытый byte-frame только нового профиля. JSON escaping/whitespace
-- не участвуют в composite digest; поле с NUL в PostgreSQL text невозможно.
-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_pdlc_result_digest(p_parts text[])
RETURNS text LANGUAGE plpgsql IMMUTABLE SECURITY INVOKER SET search_path=pg_catalog AS $$
DECLARE v_frame bytea:=''::bytea;v_part text;v_first boolean:=true;
BEGIN
 IF p_parts IS NULL OR array_ndims(p_parts) IS DISTINCT FROM 1 OR array_lower(p_parts,1)<>1
    OR EXISTS(SELECT 1 FROM unnest(p_parts) part WHERE part IS NULL)
    OR NOT (p_parts[1]='kodex.workflow-repository-file.v1' AND cardinality(p_parts)=12
        OR p_parts[1]='kodex.workflow-pr-comment.v1' AND cardinality(p_parts)=7) THEN RETURN NULL; END IF;
 FOREACH v_part IN ARRAY p_parts LOOP
    IF NOT v_first THEN v_frame:=v_frame||decode('00','hex'); END IF;
    v_frame:=v_frame||convert_to(v_part,'UTF8');v_first:=false;
 END LOOP;
 RETURN encode(sha256(v_frame),'hex');
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.workflow_pdlc_result_digest(text[]) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.workflow_pdlc_result_digest(text[]) TO control_plane_runtime;

-- Общий закрытый реестр операций, которым требуется owner outcome delivery.
-- Он не предоставляет grant и не заменяет pinned requirement/authority checks.
-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_evidence_delivery_operation(p_profile text,p_operation text)
RETURNS boolean LANGUAGE sql IMMUTABLE SECURITY INVOKER SET search_path=pg_catalog AS $$
 SELECT COALESCE(p_profile IN('GITHUB_REVIEW_QA_V1','GITHUB_PDLC_V1') AND (
    p_operation IN('github.pull_request.review.create','github.issue.create')
    OR p_profile='GITHUB_PDLC_V1' AND p_operation IN('github.repository.content.create','github.repository.content.update','github.pull_request.comment.create','github.pull_request.ready')),false);
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.workflow_evidence_delivery_operation(text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.workflow_evidence_delivery_operation(text,text) TO control_plane_runtime;

-- Selector связывает только форму и опубликованный target. Native origin,
-- actor/grant/lease и policy доказываются вызывающим owner guard отдельно.
-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_pdlc_author_slot(p_requirement jsonb,p_operation text,p_scope jsonb,p_input jsonb)
RETURNS text LANGUAGE plpgsql IMMUTABLE SECURITY INVOKER SET search_path=pg_catalog AS $$
DECLARE v_path text;v_branch text;v_part text;v_number bigint;v_fields text[];
BEGIN
 IF jsonb_typeof(p_requirement) IS DISTINCT FROM 'object' OR jsonb_typeof(p_scope) IS DISTINCT FROM 'object'
    OR p_scope-ARRAY['owner','repository']<>'{}'::jsonb OR NOT p_scope ?& ARRAY['owner','repository']
    OR jsonb_typeof(p_scope->'owner') IS DISTINCT FROM 'string' OR jsonb_typeof(p_scope->'repository') IS DISTINCT FROM 'string'
    OR p_requirement->>'Repository' IS DISTINCT FROM (p_scope->>'owner')||'/'||(p_scope->>'repository')
    OR COALESCE(p_requirement->>'Key','') !~ '^[A-Za-z0-9_-]{1,96}$' OR jsonb_typeof(p_input) IS DISTINCT FROM 'object'
    OR COALESCE(p_requirement->>'TargetRef','') !~ '^[1-9][0-9]{0,9}$' THEN RETURN NULL; END IF;
 v_number:=(p_requirement->>'TargetRef')::bigint;
 IF v_number>2147483647 THEN RETURN NULL; END IF;
 IF p_requirement->>'Kind'='REPOSITORY_FILE' AND p_operation IN('github.repository.content.create','github.repository.content.update') THEN
    v_fields:=ARRAY['path','branch','message','content_base64'];
    IF p_operation='github.repository.content.update' THEN v_fields:=v_fields||ARRAY['sha']; END IF;
    IF p_input-v_fields<>'{}'::jsonb OR NOT p_input ?& v_fields OR EXISTS(SELECT 1 FROM unnest(v_fields) field WHERE jsonb_typeof(p_input->field) IS DISTINCT FROM 'string')
       OR COALESCE(p_input->>'message','') !~ '[^[:space:]]' OR p_operation='github.repository.content.update' AND COALESCE(p_input->>'sha','') !~ '^([a-f0-9]{40}|[a-f0-9]{64})$'
       OR control_plane.workflow_pdlc_file_desired_digest('shape','shape',p_input) IS NULL THEN RETURN NULL; END IF;
    v_path:=p_input->>'path';v_branch:=p_input->>'branch';
    IF octet_length(v_path) NOT BETWEEN 1 AND 1024 OR position(chr(92) in v_path)>0 OR position(chr(13) in v_path)>0 OR position(chr(10) in v_path)>0
       OR EXISTS(SELECT 1 FROM unnest(string_to_array(v_path,'/')) part WHERE part IN('','.','..'))
       OR v_branch IS DISTINCT FROM p_requirement->>'Branch' OR v_branch !~ '^[A-Za-z0-9][A-Za-z0-9_./-]{0,254}$'
       OR left(v_branch,5)='refs/' OR position('..' in v_branch)>0 OR right(v_branch,1)='.' THEN RETURN NULL; END IF;
    FOREACH v_part IN ARRAY string_to_array(v_branch,'/') LOOP
       IF v_part='' OR left(v_part,1)='.' OR right(v_part,5)='.lock' THEN RETURN NULL; END IF;
    END LOOP;
    RETURN control_plane.workflow_pdlc_file_key(p_requirement->>'Key',v_path);
 ELSIF p_requirement->>'Kind'='PR_COMMENT' AND p_operation='github.pull_request.comment.create' THEN
    IF p_input-ARRAY['pull_request_number','body']<>'{}'::jsonb OR NOT p_input ?& ARRAY['pull_request_number','body']
       OR p_input->'pull_request_number' IS DISTINCT FROM to_jsonb(v_number) OR jsonb_typeof(p_input->'body') IS DISTINCT FROM 'string'
       OR p_input->>'body' !~ '[^[:space:]]' THEN RETURN NULL; END IF;
    RETURN p_requirement->>'Key';
 ELSIF p_requirement->>'Kind'='PULL_REQUEST' AND p_operation='github.pull_request.ready' THEN
    IF p_input-ARRAY['pull_request_number','expected_head_sha']<>'{}'::jsonb OR NOT p_input ?& ARRAY['pull_request_number','expected_head_sha']
       OR p_input->'pull_request_number' IS DISTINCT FROM to_jsonb(v_number) OR jsonb_typeof(p_input->'expected_head_sha') IS DISTINCT FROM 'string'
       OR p_input->>'expected_head_sha' !~ '^([a-f0-9]{40}|[a-f0-9]{64})$' THEN RETURN NULL; END IF;
    RETURN 'ready_'||encode(sha256(convert_to(p_requirement->>'Key','UTF8')),'hex');
 END IF;
 RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- Дополнение вызывается внутри прежнего exact native origin guard; оно не
-- заменяет проверку active root, fresh RR/lease/actor, current grant и scope.
-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_pdlc_intent_valid(p_tenant uuid,p_anchor uuid,p_origin uuid,p_invocation uuid,p_record jsonb,p_requirement jsonb)
RETURNS boolean LANGUAGE plpgsql STABLE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
DECLARE v_invocation record;v_previous jsonb;v_desired text;v_predecessor text:='';v_key text;
BEGIN
 SELECT invocation.*,root.id AS exact_root INTO v_invocation
 FROM control_plane.run_nodes anchor
 JOIN control_plane.run_nodes origin ON origin.id=p_origin AND origin.organization_id=p_tenant
    AND origin.root_run_id=anchor.root_run_id AND COALESCE(origin.workflow_step_origin_node_id,origin.id)=anchor.id
    AND origin.agent_id=anchor.agent_id AND origin.workflow_step_key=anchor.workflow_step_key
 JOIN control_plane.runs root ON root.id=anchor.root_run_id AND root.organization_id=p_tenant
 JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=p_tenant
 JOIN control_plane.integration_invocations invocation ON invocation.id=p_invocation AND invocation.organization_id=p_tenant
    AND invocation.node_id=origin.id AND invocation.run_id=origin.run_id
 WHERE anchor.id=p_anchor AND anchor.organization_id=p_tenant AND version.spec->>'ExecutionMode'='SERVER_DAG_V1'
    AND version.spec->>'ResultEvidenceProfile'='GITHUB_PDLC_V1' AND invocation.risk='WRITE'
    AND invocation.resource_kind='GITHUB_REPOSITORY' AND invocation.approval_policy='NONE';
 IF NOT FOUND THEN RETURN false; END IF;
 v_key:=control_plane.workflow_pdlc_author_slot(p_requirement,v_invocation.operation,v_invocation.resource_scope,v_invocation.bounded_input);
 IF v_key IS NULL OR p_record->>'resultKey' IS DISTINCT FROM v_key THEN RETURN false; END IF;
 IF v_invocation.operation NOT IN('github.repository.content.create','github.repository.content.update') THEN
    RETURN NOT (p_record ?| ARRAY['fileCallKey','filePredecessorRef']);
 END IF;
 IF jsonb_typeof(p_record->'fileCallKey') IS DISTINCT FROM 'string' OR p_record->>'fileCallKey' IS DISTINCT FROM v_invocation.idempotency_key
    OR octet_length(p_record->>'fileCallKey') NOT BETWEEN 8 AND 128 THEN RETURN false; END IF;
 IF p_record ? 'filePredecessorRef' THEN
    IF jsonb_typeof(p_record->'filePredecessorRef') IS DISTINCT FROM 'string' OR (p_record->>'filePredecessorRef') !~ '^[A-Za-z0-9_-]{8,96}$' THEN RETURN false; END IF;
    v_predecessor:=p_record->>'filePredecessorRef';
 END IF;
 SELECT item.value INTO v_previous
 FROM control_plane.run_nodes anchor CROSS JOIN LATERAL jsonb_array_elements(anchor.workflow_effect_intents) WITH ORDINALITY item(value,n)
 WHERE anchor.id=p_anchor AND anchor.organization_id=p_tenant AND item.value->>'resultKey'=v_key
    AND item.value->>'operation' IN('github.repository.content.create','github.repository.content.update')
 ORDER BY item.n DESC LIMIT 1;
 IF FOUND THEN
    IF v_predecessor IS DISTINCT FROM v_previous->>'invocationRef'
       OR v_previous->'connectionVersion' IS DISTINCT FROM p_record->'connectionVersion'
       OR v_previous->>'connectionRef' IS DISTINCT FROM p_record->>'connectionRef'
       OR v_previous->>'definitionDigest' IS DISTINCT FROM p_record->>'definitionDigest'
       OR v_previous->>'scopeDigest' IS DISTINCT FROM p_record->>'scopeDigest' THEN RETURN false; END IF;
    IF NOT EXISTS(SELECT 1 FROM control_plane.integration_invocations previous
       JOIN control_plane.run_nodes producer ON producer.id=previous.node_id AND producer.organization_id=p_tenant
          AND producer.root_run_id=v_invocation.exact_root AND COALESCE(producer.workflow_step_origin_node_id,producer.id)=p_anchor
       JOIN control_plane.integration_effect_receipts receipt ON receipt.id=previous.effect_receipt_id AND receipt.invocation_id=previous.id
          AND receipt.organization_id=p_tenant AND receipt.effect_key=previous.effect_key AND receipt.input_digest=previous.input_digest
          AND receipt.result_summary=previous.result_summary AND receipt.response_digest ~ '^[a-f0-9]{64}$'
          AND receipt.response_digest=encode(sha256(convert_to(receipt.result_summary,'UTF8')),'hex')
       WHERE previous.ref=v_predecessor AND previous.organization_id=p_tenant AND previous.state='SUCCEEDED'
          AND previous.connection_id=v_invocation.connection_id AND previous.resource_scope_digest=v_invocation.resource_scope_digest
          AND previous.operation IN('github.repository.content.create','github.repository.content.update')
          AND previous.bounded_input->>'path'=v_invocation.bounded_input->>'path'
          AND previous.bounded_input->>'branch'=v_invocation.bounded_input->>'branch'
          AND previous.effect_key=v_previous->>'effectKey' AND previous.input_digest=v_previous->>'inputDigest'
          AND control_plane.workflow_pdlc_file_desired_digest(v_key,previous.resource_scope_digest,previous.bounded_input)
             IS DISTINCT FROM control_plane.workflow_pdlc_file_desired_digest(v_key,v_invocation.resource_scope_digest,v_invocation.bounded_input)) THEN RETURN false; END IF;
 ELSIF v_predecessor<>'' THEN RETURN false;
 END IF;
 v_desired:=control_plane.workflow_pdlc_file_desired_digest(v_key,v_invocation.resource_scope_digest,v_invocation.bounded_input);
 RETURN v_desired IS NOT NULL AND p_record->>'semanticDigest'=control_plane.workflow_pdlc_file_epoch_digest(v_desired,v_predecessor);
END;
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION control_plane.workflow_pdlc_file_key(text,text),control_plane.workflow_pdlc_file_desired_digest(text,text,jsonb),control_plane.workflow_pdlc_file_epoch_digest(text,text),control_plane.workflow_pdlc_author_slot(jsonb,text,jsonb,jsonb),control_plane.workflow_pdlc_intent_valid(uuid,uuid,uuid,uuid,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.workflow_pdlc_file_key(text,text),control_plane.workflow_pdlc_file_desired_digest(text,text,jsonb),control_plane.workflow_pdlc_file_epoch_digest(text,text),control_plane.workflow_pdlc_author_slot(jsonb,text,jsonb,jsonb),control_plane.workflow_pdlc_intent_valid(uuid,uuid,uuid,uuid,jsonb,jsonb) TO control_plane_runtime;

-- Сохраняем полное объявление, OID, INVOKER, search_path и ACL. Каждый patch
-- обязан встретиться ровно один раз в проверенном predecessor body.
-- +goose StatementBegin
DO $effect_guard$
DECLARE v_definition text;v_source text;v_updated text;v_old text;v_new text;
BEGIN
 SELECT pg_get_functiondef(oid),prosrc INTO STRICT v_definition,v_source FROM pg_proc
 WHERE oid='control_plane.guard_workflow_effect_records()'::regprocedure;
 IF md5(v_source)<>'c18ed66f125d8a507cf832aeae0a664e' THEN RAISE EXCEPTION 'PDLC effect guard preimage changed'; END IF;
 v_updated:=v_source;
 v_old:=$old$v_count integer;$old$;
 v_new:=$new$v_count integer;v_limit integer:=16;v_profile text;$new$;
 IF (length(v_updated)-length(replace(v_updated,v_old,'')))/length(v_old)<>1 THEN RAISE EXCEPTION 'PDLC effect declaration patch is ambiguous'; END IF;
 v_updated:=replace(v_updated,v_old,v_new);

 v_old:=$old$v_anchor:=COALESCE(NEW.workflow_step_origin_node_id,NEW.id);$old$;
 v_new:=$new$v_anchor:=COALESCE(NEW.workflow_step_origin_node_id,NEW.id);
    SELECT version.spec->>'ResultEvidenceProfile' INTO STRICT v_profile FROM control_plane.runs root
    JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
    WHERE root.id=NEW.root_run_id AND root.organization_id=NEW.organization_id;
    IF v_profile='GITHUB_PDLC_V1' THEN v_limit:=64; END IF;
    IF jsonb_array_length(NEW.workflow_effect_intents)>v_limit OR jsonb_array_length(NEW.workflow_effect_rejoins)>v_limit THEN
        RAISE EXCEPTION 'workflow effect profile journal limit exceeded' USING ERRCODE='23514',CONSTRAINT='workflow_effect_record_bounds';
    END IF;$new$;
 IF (length(v_updated)-length(replace(v_updated,v_old,'')))/length(v_old)<>1 THEN RAISE EXCEPTION 'PDLC effect limit patch is ambiguous'; END IF;
 v_updated:=replace(v_updated,v_old,v_new);

 v_old:=$old$v_record->>'operation'='github.issue.create' AND v_record ?& ARRAY['qaReport','qaLogs']
            AND (SELECT count(*) FROM jsonb_object_keys(v_record))=23$old$;
 v_new:=$new$(v_record->>'operation'='github.issue.create' AND v_record ?& ARRAY['qaReport','qaLogs']
            AND (SELECT count(*) FROM jsonb_object_keys(v_record))=23)
            OR (v_profile='GITHUB_PDLC_V1' AND v_record->>'operation' IN('github.repository.content.create','github.repository.content.update')
                AND v_record ? 'fileCallKey' AND (SELECT count(*) FROM jsonb_object_keys(v_record))=22+CASE WHEN v_record ? 'filePredecessorRef' THEN 1 ELSE 0 END)$new$;
 IF (length(v_updated)-length(replace(v_updated,v_old,'')))/length(v_old)<>1 THEN RAISE EXCEPTION 'PDLC effect shape patch is ambiguous'; END IF;
 v_updated:=replace(v_updated,v_old,v_new);

 v_old:=$old$AND requirement.value->>'Key'=v_record->>'resultKey'$old$;
 v_new:=$new$AND (requirement.value->>'Key'=v_record->>'resultKey'
                    OR control_plane.workflow_pdlc_author_slot(requirement.value,invocation.operation,invocation.resource_scope,invocation.bounded_input)=v_record->>'resultKey')$new$;
 IF (length(v_updated)-length(replace(v_updated,v_old,'')))/length(v_old)<>1 THEN RAISE EXCEPTION 'PDLC effect key patch is ambiguous'; END IF;
 v_updated:=replace(v_updated,v_old,v_new);

 v_old:=$old$requirement.value->>'Kind'='PULL_REQUEST' AND invocation.operation IN ('github.repository.bundle.publish','github.pull_request.create')$old$;
 v_new:=$new$COALESCE(version.spec->>'ResultEvidenceProfile','')<>'GITHUB_PDLC_V1' AND requirement.value->>'Kind'='PULL_REQUEST' AND invocation.operation IN ('github.repository.bundle.publish','github.pull_request.create')$new$;
 IF (length(v_updated)-length(replace(v_updated,v_old,'')))/length(v_old)<>1 THEN RAISE EXCEPTION 'PDLC existing PR patch is ambiguous'; END IF;
 v_updated:=replace(v_updated,v_old,v_new);

 v_old:=$old$OR control_plane.workflow_evidence_intent_valid(NEW.organization_id,NEW.id,origin.id,invocation.id,v_record,requirement.value))$old$;
 v_new:=$new$OR control_plane.workflow_evidence_intent_valid(NEW.organization_id,NEW.id,origin.id,invocation.id,v_record,requirement.value)
                    OR control_plane.workflow_pdlc_intent_valid(NEW.organization_id,NEW.id,origin.id,invocation.id,v_record,requirement.value))$new$;
 IF (length(v_updated)-length(replace(v_updated,v_old,'')))/length(v_old)<>1 THEN RAISE EXCEPTION 'PDLC native intent patch is ambiguous'; END IF;
 v_updated:=replace(v_updated,v_old,v_new);

 v_old:=$old$AND (v_record->>'operation' NOT IN ('github.pull_request.review.create','github.issue.create')
                    OR item->'originBusinessAttempt'=v_record->'originBusinessAttempt'))<>1$old$;
 v_new:=$new$AND CASE WHEN v_profile='GITHUB_PDLC_V1' AND v_record->>'operation' IN('github.repository.content.create','github.repository.content.update') THEN
                    COALESCE(item->>'filePredecessorRef','')=COALESCE(v_record->>'filePredecessorRef','')
                ELSE (v_record->>'operation' NOT IN ('github.pull_request.review.create','github.issue.create')
                    OR item->'originBusinessAttempt'=v_record->'originBusinessAttempt') END)<>1$new$;
 IF (length(v_updated)-length(replace(v_updated,v_old,'')))/length(v_old)<>1 THEN RAISE EXCEPTION 'PDLC file epoch uniqueness patch is ambiguous'; END IF;
 v_updated:=replace(v_updated,v_old,v_new);

 v_old:=$old$v_count+jsonb_array_length(NEW.workflow_effect_rejoins)>16$old$;
 v_new:=$new$v_count+jsonb_array_length(NEW.workflow_effect_rejoins)>v_limit$new$;
 IF (length(v_updated)-length(replace(v_updated,v_old,'')))/length(v_old)<>1 THEN RAISE EXCEPTION 'PDLC rejoin limit patch is ambiguous'; END IF;
 v_updated:=replace(v_updated,v_old,v_new);

 v_old:=$old$AND item->'businessAttempt'=v_record->'businessAttempt') THEN$old$;
 v_new:=$new$AND item->'businessAttempt'=v_record->'businessAttempt'
                AND (v_profile IS DISTINCT FROM 'GITHUB_PDLC_V1' OR v_record->>'operation' NOT IN('github.repository.content.create','github.repository.content.update')
                    OR item->>'invocationRef'=v_record->>'invocationRef')) THEN$new$;
 IF (length(v_updated)-length(replace(v_updated,v_old,'')))/length(v_old)<>1 THEN RAISE EXCEPTION 'PDLC exact rejoin epoch patch is ambiguous'; END IF;
 v_updated:=replace(v_updated,v_old,v_new);
 IF (length(v_definition)-length(replace(v_definition,v_source,'')))/length(v_source)<>1 THEN RAISE EXCEPTION 'PDLC effect function declaration is ambiguous'; END IF;
 EXECUTE replace(v_definition,v_source,v_updated);
END $effect_guard$;
-- +goose StatementEnd

ALTER TABLE control_plane.run_nodes DROP CONSTRAINT workflow_effect_record_bounds;
ALTER TABLE control_plane.run_nodes ADD CONSTRAINT workflow_effect_record_bounds
 CHECK (jsonb_array_length(workflow_effect_intents)<=64 AND jsonb_array_length(workflow_effect_rejoins)<=64);


-- Общий owner proof сохраняет прежний closed-capture/lineage/rejoin алгоритм.
-- VOLATILE требуется для прежнего FOR SHARE NOWAIT; функция не изменяет business rows.
-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_receipt_effect_proof(p_organization uuid,p_node uuid,p_root uuid,p_revision uuid,p_lease uuid,p_receipt text)
RETURNS TABLE(operation text,bounded_input jsonb,resource_scope jsonb,result_summary text,response_digest text,provider_effect_ref text,lineage_failure text)
LANGUAGE sql VOLATILE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $proof$
WITH RECURSIVE final AS MATERIALIZED (
    SELECT node.id,node.organization_id,node.root_run_id,node.run_id,node.agent_id,node.turn_id,node.attempt,
           COALESCE(node.workflow_step_origin_node_id,node.id) AS origin_id,node.workflow_business_attempt,
           node.workflow_captured_artifacts,node.workflow_step_key,root.project_id,root.initiated_by AS actor_id,
           run.session_id,revision.id AS revision_id,revision.ref AS revision_ref,revision.generation,revision.safe_snapshot
    FROM control_plane.run_nodes node
    JOIN control_plane.runs root ON root.id=node.root_run_id AND root.organization_id=node.organization_id
    JOIN control_plane.runs run ON run.id=node.run_id AND run.organization_id=node.organization_id
        AND run.root_run_id=root.id AND run.project_id=root.project_id
    JOIN control_plane.runtime_revisions revision ON revision.node_id=node.id AND revision.run_id=node.run_id
        AND revision.organization_id=node.organization_id AND revision.root_run_id=node.root_run_id
        AND revision.turn_id=node.turn_id AND revision.agent_id=node.agent_id AND revision.attempt=node.attempt
    JOIN control_plane.runtime_leases lease ON lease.runtime_revision_id=revision.id
        AND lease.organization_id=revision.organization_id AND lease.run_id=revision.run_id
        AND lease.node_id=revision.node_id AND lease.generation=revision.generation AND lease.state='COMPLETED'
    JOIN control_plane.subjects actor ON actor.id=root.initiated_by AND actor.organization_id=root.organization_id AND actor.active
    WHERE node.organization_id=$1::uuid AND node.id=$2::uuid
        AND node.root_run_id=$3::uuid AND revision.id=$4::uuid AND lease.id=$5::uuid
        AND node.workflow_business_attempt BETWEEN 1 AND 4 AND node.state='SUCCEEDED'
        AND node.workflow_captured_artifacts->>'runtimeRevisionId'=revision.id::text
        AND node.workflow_captured_artifacts->>'leaseId'=lease.id::text
        AND node.workflow_captured_artifacts->'generation'=to_jsonb(lease.generation)
), lineage AS (
    SELECT final.id,final.turn_id,final.attempt,final.workflow_captured_artifacts,0 AS depth,ARRAY[final.id] AS path,false AS cycle
    FROM final
    UNION ALL
    SELECT previous.id,previous.turn_id,previous.attempt,previous.workflow_captured_artifacts,
           lineage.depth+1,lineage.path || previous.id,previous.id=ANY(lineage.path)
    FROM lineage CROSS JOIN final
    JOIN control_plane.run_edges edge ON edge.organization_id=final.organization_id AND edge.root_run_id=final.root_run_id
        AND edge.type='CONTINUES' AND edge.target_node_id=lineage.id
    JOIN control_plane.run_nodes previous ON previous.id=edge.source_node_id AND previous.organization_id=final.organization_id
        AND previous.root_run_id=final.root_run_id AND previous.run_id=final.run_id AND previous.agent_id=final.agent_id
        AND previous.state='SUCCEEDED' AND previous.attempt+1=lineage.attempt
        AND COALESCE(previous.workflow_step_origin_node_id,previous.id)=final.origin_id
        AND previous.workflow_business_attempt=final.workflow_business_attempt AND previous.workflow_step_key=final.workflow_step_key
    JOIN control_plane.session_turns turn ON turn.id=previous.turn_id AND turn.run_id=previous.run_id AND turn.session_id=final.session_id
    JOIN control_plane.runtime_revisions revision ON revision.id::text=previous.workflow_captured_artifacts->>'runtimeRevisionId'
        AND revision.organization_id=previous.organization_id AND revision.root_run_id=previous.root_run_id
        AND revision.node_id=previous.id AND revision.run_id=previous.run_id AND revision.turn_id=previous.turn_id
        AND revision.agent_id=previous.agent_id AND revision.attempt=previous.attempt
    JOIN control_plane.runtime_leases lease ON lease.id::text=previous.workflow_captured_artifacts->>'leaseId'
        AND lease.organization_id=revision.organization_id AND lease.runtime_revision_id=revision.id
        AND lease.node_id=previous.id AND lease.run_id=previous.run_id AND lease.generation=revision.generation
        AND to_jsonb(lease.generation)=previous.workflow_captured_artifacts->'generation' AND lease.state='COMPLETED'
    WHERE NOT lineage.cycle AND lineage.depth<65
        AND 1=(SELECT count(*) FROM control_plane.run_edges incoming
            WHERE incoming.organization_id=final.organization_id AND incoming.root_run_id=final.root_run_id
                AND incoming.type='CONTINUES' AND incoming.target_node_id=lineage.id)
)
, bridges AS MATERIALIZED (
SELECT invocation.operation,invocation.bounded_input,invocation.resource_scope,receipt.result_summary,
       receipt.response_digest,receipt.provider_effect_ref,'' AS failure_reason,
       anchor.id AS anchor_id,intent.value->>'resultKey' AS result_key,invocation.id AS invocation_id,receipt.id AS receipt_id,producer.depth
FROM lineage producer CROSS JOIN final
JOIN control_plane.run_nodes current_node ON current_node.id=producer.id AND current_node.organization_id=final.organization_id
    AND current_node.root_run_id=final.root_run_id AND current_node.run_id=final.run_id
JOIN control_plane.runtime_revisions current_proof ON current_proof.id::text=producer.workflow_captured_artifacts->>'runtimeRevisionId'
    AND current_proof.organization_id=final.organization_id AND current_proof.root_run_id=final.root_run_id
    AND current_proof.run_id=final.run_id AND current_proof.node_id=producer.id AND current_proof.agent_id=final.agent_id
    AND current_proof.turn_id=producer.turn_id AND current_proof.attempt=producer.attempt
JOIN control_plane.runtime_leases current_lease ON current_lease.id::text=producer.workflow_captured_artifacts->>'leaseId'
    AND current_lease.organization_id=current_proof.organization_id AND current_lease.runtime_revision_id=current_proof.id
    AND current_lease.node_id=producer.id AND current_lease.run_id=current_proof.run_id AND current_lease.generation=current_proof.generation
    AND to_jsonb(current_lease.generation)=producer.workflow_captured_artifacts->'generation' AND current_lease.state='COMPLETED'
CROSS JOIN LATERAL jsonb_array_elements(current_node.workflow_effect_rejoins) rejoin(value)
JOIN control_plane.run_nodes anchor ON anchor.id=final.origin_id AND anchor.organization_id=final.organization_id
    AND anchor.root_run_id=final.root_run_id AND anchor.agent_id=final.agent_id AND anchor.workflow_step_key=final.workflow_step_key
    AND anchor.workflow_step_origin_node_id IS NULL AND anchor.workflow_business_attempt=1
CROSS JOIN LATERAL jsonb_array_elements(anchor.workflow_effect_intents) intent(value)
JOIN control_plane.run_nodes origin ON origin.id::text=intent.value->>'originNodeId' AND origin.organization_id=final.organization_id
    AND origin.root_run_id=final.root_run_id AND origin.agent_id=final.agent_id AND origin.workflow_step_key=final.workflow_step_key
    AND COALESCE(origin.workflow_step_origin_node_id,origin.id)=anchor.id
    AND origin.run_id::text=intent.value->>'originRunId' AND to_jsonb(origin.attempt)=intent.value->'originAttempt'
    AND to_jsonb(origin.workflow_business_attempt)=intent.value->'originBusinessAttempt'
JOIN control_plane.runtime_revisions proof ON proof.id::text=intent.value->>'originRevisionId' AND proof.node_id=origin.id
    AND proof.organization_id=final.organization_id AND proof.root_run_id=final.root_run_id AND proof.run_id=origin.run_id
    AND proof.agent_id=final.agent_id AND proof.turn_id=origin.turn_id AND proof.attempt=origin.attempt
    AND to_jsonb(proof.generation)=intent.value->'originGeneration'
JOIN control_plane.runtime_leases origin_lease ON origin_lease.id::text=intent.value->>'originLeaseId' AND origin_lease.runtime_revision_id=proof.id
    AND origin_lease.organization_id=proof.organization_id AND origin_lease.run_id=origin.run_id AND origin_lease.node_id=origin.id AND origin_lease.generation=proof.generation
JOIN control_plane.integration_invocations invocation ON invocation.ref=intent.value->>'invocationRef' AND invocation.organization_id=final.organization_id
    AND invocation.run_id=origin.run_id AND invocation.node_id=origin.id AND invocation.state='SUCCEEDED'
    AND invocation.operation=intent.value->>'operation' AND invocation.effect_key=intent.value->>'effectKey'
    AND invocation.input_digest=intent.value->>'inputDigest' AND invocation.resource_scope_digest=intent.value->>'scopeDigest'
    AND invocation.definition_digest=intent.value->>'definitionDigest' AND to_jsonb(invocation.grant_version)=intent.value->'grantVersion'
    AND invocation.approval_policy=intent.value->>'approvalPolicy' AND (invocation.resource_kind='GITHUB_REPOSITORY' OR (invocation.resource_kind='SSH_TARGET' AND invocation.operation='ssh.compose.deploy'
      AND invocation.ssh_compose_snapshot->>'originRuntimeRevisionId'=proof.id::text
      AND invocation.ssh_compose_snapshot->>'originLeaseId'=origin_lease.id::text
      AND invocation.ssh_compose_snapshot->'originGeneration'=to_jsonb(proof.generation)
      AND invocation.ssh_compose_snapshot->>'deploymentRef'=invocation.ref
      AND invocation.ssh_compose_snapshot->>'deploymentEffectKey'=invocation.effect_key
      AND invocation.ssh_compose_snapshot->>'deploymentInputDigest'=invocation.input_digest))
JOIN control_plane.integration_effect_receipts receipt ON receipt.id=invocation.effect_receipt_id AND receipt.organization_id=final.organization_id
    AND receipt.invocation_id=invocation.id AND receipt.effect_key=invocation.effect_key AND receipt.input_digest=invocation.input_digest
    AND receipt.result_summary=invocation.result_summary AND receipt.ref=rejoin.value->>'receiptRef' AND receipt.response_digest=rejoin.value->>'receiptDigest'
JOIN control_plane.integration_connections connection ON connection.id=invocation.connection_id AND connection.organization_id=final.organization_id
    AND connection.ref=intent.value->>'connectionRef' AND to_jsonb(connection.version)=intent.value->'connectionVersion'
    AND connection.enabled AND connection.state='CONNECTED'
    AND ((connection.definition_key='github' AND invocation.resource_kind='GITHUB_REPOSITORY')
      OR (connection.definition_key='ssh-compose' AND invocation.resource_kind='SSH_TARGET' AND invocation.operation='ssh.compose.deploy'))
    AND connection.definition_digest=invocation.definition_digest AND connection.definition_version=invocation.definition_version
JOIN control_plane.agents agent ON agent.id=final.agent_id AND agent.organization_id=final.organization_id AND agent.project_id=final.project_id AND agent.enabled AND agent.state='READY'
JOIN control_plane.integration_grants grant_binding ON grant_binding.id=invocation.grant_id AND grant_binding.organization_id=final.organization_id
    AND grant_binding.connection_id=connection.id AND grant_binding.ref=intent.value->>'grantRef'
    AND grant_binding.enabled AND grant_binding.version=invocation.grant_version AND grant_binding.target_kind='AGENT' AND grant_binding.target_ref=agent.ref
    AND grant_binding.resource_scope=invocation.resource_scope AND grant_binding.resource_scope_digest=invocation.resource_scope_digest
    AND grant_binding.definition_digest=invocation.definition_digest AND grant_binding.definition_version=invocation.definition_version
WHERE receipt.ref=$6 AND rejoin.value->>'schema'='kodex.workflow-effect-rejoin.v1'
    AND intent.value->>'schema'='kodex.workflow-effect-intent.v1' AND rejoin.value->>'resultKey'=intent.value->>'resultKey'
    AND rejoin.value->>'operation'=intent.value->>'operation' AND rejoin.value->>'semanticDigest'=intent.value->>'semanticDigest'
    AND rejoin.value->>'invocationRef'=invocation.ref AND rejoin.value->>'runtimeRevisionId'=current_proof.id::text
    AND rejoin.value->>'leaseId'=current_lease.id::text AND rejoin.value->'generation'=to_jsonb(current_lease.generation)
    AND rejoin.value->'runtimeAttempt'=to_jsonb(current_node.attempt) AND rejoin.value->'businessAttempt'=to_jsonb(current_node.workflow_business_attempt)
    AND NOT EXISTS (SELECT 1 FROM lineage WHERE cycle OR depth>64)
    AND NOT EXISTS (SELECT 1 FROM lineage item WHERE
        (SELECT count(*) FROM control_plane.run_edges incoming WHERE incoming.organization_id=final.organization_id
         AND incoming.root_run_id=final.root_run_id AND incoming.type='CONTINUES' AND incoming.target_node_id=item.id)>1
        OR EXISTS (SELECT 1 FROM control_plane.run_edges incoming WHERE incoming.organization_id=final.organization_id
            AND incoming.root_run_id=final.root_run_id AND incoming.type='CONTINUES' AND incoming.target_node_id=item.id
            AND NOT EXISTS (SELECT 1 FROM lineage previous WHERE previous.id=incoming.source_node_id AND previous.depth=item.depth+1)))
    AND EXISTS (SELECT 1 FROM jsonb_array_elements(COALESCE(proof.safe_snapshot->'integrationGrants','[]'::jsonb)) pin
        WHERE pin->>'ref'=grant_binding.ref AND pin->>'grantVersion'=grant_binding.version::text AND pin->>'connectionRef'=connection.ref
            AND pin->>'operation'=invocation.operation AND pin->>'definitionDigest'=invocation.definition_digest AND pin->>'approvalPolicy'=invocation.approval_policy)
    AND EXISTS (SELECT 1 FROM jsonb_array_elements(COALESCE(current_proof.safe_snapshot->'integrationGrants','[]'::jsonb)) pin
        WHERE pin->>'ref'=grant_binding.ref AND pin->>'grantVersion'=grant_binding.version::text AND pin->>'connectionRef'=connection.ref
            AND pin->>'operation'=invocation.operation AND pin->>'definitionDigest'=invocation.definition_digest AND pin->>'approvalPolicy'=invocation.approval_policy)
FOR SHARE OF grant_binding NOWAIT
)
SELECT operation,bounded_input,resource_scope,result_summary,response_digest,provider_effect_ref,failure_reason
FROM (
    SELECT bridges.*,row_number() OVER (
        PARTITION BY anchor_id,result_key,operation,invocation_id,receipt_id ORDER BY depth
    ) AS exact_bridge_rank FROM bridges
) proved
WHERE exact_bridge_rank=1
ORDER BY anchor_id,result_key,operation,invocation_id,receipt_id
LIMIT 2;
$proof$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.workflow_receipt_effect_proof(uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.workflow_receipt_effect_proof(uuid,uuid,uuid,uuid,uuid,text) TO control_plane_runtime;

-- Общий owner proof сохраняет прежний closed-capture/lineage/rejoin алгоритм.
-- VOLATILE требуется для прежнего FOR SHARE NOWAIT; функция не изменяет business rows.
-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_receipt_lineage_proof(p_organization uuid,p_node uuid,p_root uuid,p_revision uuid,p_lease uuid,p_receipt text)
RETURNS TABLE(operation text,bounded_input jsonb,resource_scope jsonb,result_summary text,response_digest text,provider_effect_ref text,lineage_failure text)
LANGUAGE sql VOLATILE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $proof$
WITH RECURSIVE final AS MATERIALIZED (
    SELECT node.id,node.organization_id,node.root_run_id,node.run_id,node.agent_id,node.turn_id,node.attempt,
           COALESCE(node.workflow_step_origin_node_id,node.id) AS origin_id,node.workflow_business_attempt,
           node.workflow_captured_artifacts,node.workflow_step_key,root.project_id,root.initiated_by AS actor_id,
           run.session_id,revision.id AS revision_id,revision.ref AS revision_ref,revision.generation,revision.safe_snapshot
    FROM control_plane.run_nodes node
    JOIN control_plane.runs root ON root.id=node.root_run_id AND root.organization_id=node.organization_id
    JOIN control_plane.runs run ON run.id=node.run_id AND run.organization_id=node.organization_id
        AND run.root_run_id=root.id AND run.project_id=root.project_id
    JOIN control_plane.runtime_revisions revision ON revision.node_id=node.id AND revision.run_id=node.run_id
        AND revision.organization_id=node.organization_id AND revision.root_run_id=node.root_run_id
        AND revision.turn_id=node.turn_id AND revision.agent_id=node.agent_id AND revision.attempt=node.attempt
    JOIN control_plane.runtime_leases lease ON lease.runtime_revision_id=revision.id
        AND lease.organization_id=revision.organization_id AND lease.run_id=revision.run_id
        AND lease.node_id=revision.node_id AND lease.generation=revision.generation AND lease.state='COMPLETED'
    JOIN control_plane.subjects actor ON actor.id=root.initiated_by AND actor.organization_id=root.organization_id AND actor.active
    WHERE node.organization_id=$1::uuid AND node.id=$2::uuid
        AND node.root_run_id=$3::uuid AND revision.id=$4::uuid AND lease.id=$5::uuid
        AND node.workflow_business_attempt BETWEEN 1 AND 4 AND node.state='SUCCEEDED'
        AND node.workflow_captured_artifacts->>'runtimeRevisionId'=revision.id::text
        AND node.workflow_captured_artifacts->>'leaseId'=lease.id::text
        AND node.workflow_captured_artifacts->'generation'=to_jsonb(lease.generation)
), lineage AS (
    SELECT final.id,final.turn_id,final.attempt,final.workflow_captured_artifacts,0 AS depth,ARRAY[final.id] AS path,false AS cycle
    FROM final
    UNION ALL
    SELECT previous.id,previous.turn_id,previous.attempt,previous.workflow_captured_artifacts,
           lineage.depth+1,lineage.path || previous.id,previous.id=ANY(lineage.path)
    FROM lineage CROSS JOIN final
    JOIN control_plane.run_edges edge ON edge.organization_id=final.organization_id AND edge.root_run_id=final.root_run_id
        AND edge.type='CONTINUES' AND edge.target_node_id=lineage.id
    JOIN control_plane.run_nodes previous ON previous.id=edge.source_node_id AND previous.organization_id=final.organization_id
        AND previous.root_run_id=final.root_run_id AND previous.run_id=final.run_id AND previous.agent_id=final.agent_id
        AND previous.state='SUCCEEDED' AND previous.attempt+1=lineage.attempt
        AND COALESCE(previous.workflow_step_origin_node_id,previous.id)=final.origin_id
        AND previous.workflow_business_attempt=final.workflow_business_attempt AND previous.workflow_step_key=final.workflow_step_key
    JOIN control_plane.session_turns turn ON turn.id=previous.turn_id AND turn.run_id=previous.run_id AND turn.session_id=final.session_id
    JOIN control_plane.runtime_revisions revision ON revision.id::text=previous.workflow_captured_artifacts->>'runtimeRevisionId'
        AND revision.organization_id=previous.organization_id AND revision.root_run_id=previous.root_run_id
        AND revision.node_id=previous.id AND revision.run_id=previous.run_id AND revision.turn_id=previous.turn_id
        AND revision.agent_id=previous.agent_id AND revision.attempt=previous.attempt
    JOIN control_plane.runtime_leases lease ON lease.id::text=previous.workflow_captured_artifacts->>'leaseId'
        AND lease.organization_id=revision.organization_id AND lease.runtime_revision_id=revision.id
        AND lease.node_id=previous.id AND lease.run_id=previous.run_id AND lease.generation=revision.generation
        AND to_jsonb(lease.generation)=previous.workflow_captured_artifacts->'generation' AND lease.state='COMPLETED'
    WHERE NOT lineage.cycle AND lineage.depth<65
        AND 1=(SELECT count(*) FROM control_plane.run_edges incoming
            WHERE incoming.organization_id=final.organization_id AND incoming.root_run_id=final.root_run_id
                AND incoming.type='CONTINUES' AND incoming.target_node_id=lineage.id)
)
SELECT invocation.operation,invocation.bounded_input,invocation.resource_scope,receipt.result_summary,
       receipt.response_digest,receipt.provider_effect_ref,
       CASE
        WHEN EXISTS (SELECT 1 FROM lineage WHERE cycle OR depth>64) OR EXISTS (
            SELECT 1 FROM lineage current
            WHERE (SELECT count(*) FROM control_plane.run_edges incoming
                WHERE incoming.organization_id=final.organization_id AND incoming.root_run_id=final.root_run_id
                    AND incoming.type='CONTINUES' AND incoming.target_node_id=current.id)>1
            OR EXISTS (SELECT 1 FROM control_plane.run_edges incoming
                WHERE incoming.organization_id=final.organization_id AND incoming.root_run_id=final.root_run_id
                    AND incoming.type='CONTINUES' AND incoming.target_node_id=current.id
                    AND NOT EXISTS (SELECT 1 FROM lineage previous
                        WHERE previous.id=incoming.source_node_id AND previous.depth=current.depth+1))
        ) THEN 'RECEIPT_LINEAGE_INVALID'
        WHEN invocation.operation='ssh.compose.deploy' AND NOT COALESCE((
            invocation.ssh_compose_snapshot->>'originRuntimeRevisionId'=proof.id::text
            AND invocation.ssh_compose_snapshot->>'originLeaseId'=producer_lease.id::text
            AND invocation.ssh_compose_snapshot->'originGeneration'=to_jsonb(proof.generation)
            AND invocation.ssh_compose_snapshot->>'deploymentRef'=invocation.ref
            AND invocation.ssh_compose_snapshot->>'deploymentEffectKey'=invocation.effect_key
            AND invocation.ssh_compose_snapshot->>'deploymentInputDigest'=invocation.input_digest
          ),false) THEN 'RECEIPT_LINEAGE_INVALID'
        WHEN invocation.operation NOT IN('github.repository.bundle.publish','ssh.compose.deploy') AND
            (SELECT count(*) FROM control_plane.runtime_revisions candidate
             WHERE candidate.organization_id=final.organization_id AND candidate.node_id=producer.id)<>1
            THEN 'RECEIPT_LINEAGE_AMBIGUOUS'
        WHEN NOT EXISTS (
            SELECT 1 FROM jsonb_array_elements(COALESCE(proof.safe_snapshot->'integrationGrants','[]'::jsonb)) pin(value)
            WHERE pin.value->>'ref'=grant_binding.ref AND pin.value->>'grantVersion'=invocation.grant_version::text
                AND pin.value->>'connectionRef'=connection.ref AND pin.value->>'capabilityKey'=invocation.capability_key
                AND pin.value->>'operation'=invocation.operation AND pin.value->>'definitionVersion'=invocation.definition_version
                AND pin.value->>'definitionDigest'=invocation.definition_digest
                AND pin.value->>'approvalPolicy'=invocation.approval_policy
        ) THEN 'RECEIPT_SCOPE_MISMATCH'
        WHEN invocation.operation='github.repository.bundle.publish' AND NOT EXISTS (
            SELECT 1 FROM control_plane.repository_bundle_publications publication
            WHERE publication.organization_id=final.organization_id AND publication.project_id=final.project_id
                AND publication.root_run_id=final.root_run_id AND publication.root_actor_id=final.actor_id
                AND publication.origin_run_id=final.run_id AND publication.origin_node_id=producer.id
                AND publication.origin_runtime_revision_id=proof.id AND publication.origin_lease_ref=producer_lease.ref
                AND publication.origin_generation=proof.generation
                AND publication.integration_invocation_id=invocation.id AND publication.connection_id=invocation.connection_id
                AND publication.grant_id=invocation.grant_id AND publication.state='SUCCEEDED'
                AND publication.candidate_commit_sha=receipt.provider_effect_ref
        ) THEN 'RECEIPT_LINEAGE_INVALID'
        ELSE '' END AS failure_reason
FROM lineage producer CROSS JOIN final
JOIN control_plane.runtime_revisions proof ON proof.id::text=producer.workflow_captured_artifacts->>'runtimeRevisionId'
    AND proof.organization_id=final.organization_id AND proof.root_run_id=final.root_run_id
    AND proof.run_id=final.run_id AND proof.node_id=producer.id AND proof.agent_id=final.agent_id
    AND proof.turn_id=producer.turn_id AND proof.attempt=producer.attempt
JOIN control_plane.runtime_leases producer_lease ON producer_lease.id::text=producer.workflow_captured_artifacts->>'leaseId'
    AND producer_lease.organization_id=proof.organization_id AND producer_lease.runtime_revision_id=proof.id
    AND producer_lease.node_id=producer.id AND producer_lease.run_id=proof.run_id AND producer_lease.generation=proof.generation
    AND to_jsonb(producer_lease.generation)=producer.workflow_captured_artifacts->'generation' AND producer_lease.state='COMPLETED'
JOIN control_plane.integration_invocations invocation ON invocation.node_id=producer.id AND invocation.run_id=final.run_id
    AND invocation.organization_id=final.organization_id AND invocation.state='SUCCEEDED'
    AND (invocation.resource_kind='GITHUB_REPOSITORY' OR (invocation.resource_kind='SSH_TARGET' AND invocation.operation='ssh.compose.deploy'))
JOIN control_plane.integration_effect_receipts receipt ON receipt.id=invocation.effect_receipt_id
    AND receipt.invocation_id=invocation.id AND receipt.organization_id=invocation.organization_id
    AND receipt.effect_key=invocation.effect_key AND receipt.input_digest=invocation.input_digest
    AND receipt.result_summary=invocation.result_summary
JOIN control_plane.integration_connections connection ON connection.id=invocation.connection_id
    AND connection.organization_id=final.organization_id
    AND ((connection.definition_key='github' AND invocation.resource_kind='GITHUB_REPOSITORY')
      OR (connection.definition_key='ssh-compose' AND invocation.resource_kind='SSH_TARGET' AND invocation.operation='ssh.compose.deploy'))
JOIN control_plane.agents agent ON agent.id=final.agent_id AND agent.organization_id=final.organization_id
    AND agent.project_id=final.project_id
JOIN control_plane.integration_grants grant_binding ON grant_binding.id=invocation.grant_id
    AND grant_binding.organization_id=final.organization_id AND grant_binding.connection_id=connection.id
    AND grant_binding.target_kind='AGENT' AND grant_binding.target_ref=agent.ref
    AND grant_binding.enabled AND grant_binding.version=invocation.grant_version
    AND grant_binding.resource_scope=invocation.resource_scope
    AND grant_binding.resource_scope_digest=invocation.resource_scope_digest
WHERE receipt.ref=$6
ORDER BY producer.id
LIMIT 2
FOR SHARE OF grant_binding NOWAIT;
$proof$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.workflow_receipt_lineage_proof(uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.workflow_receipt_lineage_proof(uuid,uuid,uuid,uuid,uuid,text) TO control_plane_runtime;

-- Сохраняет прежний приоритет rejoin proof; неоднозначность не скрывается fallback.
-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_native_receipt_proof(p_organization uuid,p_node uuid,p_root uuid,p_revision uuid,p_lease uuid,p_receipt text)
RETURNS TABLE(operation text,bounded_input jsonb,resource_scope jsonb,result_summary text,response_digest text,provider_effect_ref text,lineage_failure text)
LANGUAGE plpgsql VOLATILE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $proof$
DECLARE v_count bigint;
BEGIN
 RETURN QUERY SELECT fact.operation,fact.bounded_input,fact.resource_scope,fact.result_summary,
    fact.response_digest,fact.provider_effect_ref,fact.lineage_failure
 FROM control_plane.workflow_receipt_effect_proof(p_organization,p_node,p_root,p_revision,p_lease,p_receipt) fact;
 GET DIAGNOSTICS v_count=ROW_COUNT;
 IF v_count=0 THEN
    RETURN QUERY SELECT fact.operation,fact.bounded_input,fact.resource_scope,fact.result_summary,
       fact.response_digest,fact.provider_effect_ref,fact.lineage_failure
    FROM control_plane.workflow_receipt_lineage_proof(p_organization,p_node,p_root,p_revision,p_lease,p_receipt) fact;
 END IF;
END $proof$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.workflow_native_receipt_proof(uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.workflow_native_receipt_proof(uuid,uuid,uuid,uuid,uuid,text) TO control_plane_runtime;

-- Общая хронология используется Go reader и SQL outcome guard после независимой
-- проверки всех трёх native proofs; сама не предоставляет полномочия.
-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_pdlc_read_order(p_organization uuid,p_write text,p_head text,p_content text)
RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $order$
SELECT EXISTS (
    SELECT 1
    FROM control_plane.integration_effect_receipts written
    JOIN control_plane.integration_invocations writer ON writer.id=written.invocation_id
        AND writer.organization_id=written.organization_id AND writer.effect_receipt_id=written.id
        AND writer.state='SUCCEEDED'
    JOIN control_plane.integration_effect_receipts head ON head.organization_id=written.organization_id
        AND head.ref=$3
    JOIN control_plane.integration_invocations head_read ON head_read.id=head.invocation_id
        AND head_read.organization_id=head.organization_id AND head_read.effect_receipt_id=head.id
        AND head_read.state='SUCCEEDED' AND head_read.operation='github.pull_request.read'
    JOIN control_plane.integration_effect_receipts content ON content.organization_id=written.organization_id
        AND content.ref=$4
    JOIN control_plane.integration_invocations content_read ON content_read.id=content.invocation_id
        AND content_read.organization_id=content.organization_id AND content_read.effect_receipt_id=content.id
        AND content_read.state='SUCCEEDED' AND content_read.operation='github.repository.content.read'
    WHERE written.organization_id=$1::uuid AND written.ref=$2
        AND writer.operation IN('github.repository.content.create','github.repository.content.update')
        AND head_read.created_at>=written.created_at AND head.created_at>=head_read.created_at
        AND content_read.created_at>=head.created_at AND content.created_at>=content_read.created_at
);
$order$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.workflow_pdlc_read_order(uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.workflow_pdlc_read_order(uuid,text,text,text) TO control_plane_runtime;

-- Закрытая native proof проекция для SQL guard. Полномочия и locks остаются
-- в тех же shared readers, которыми пользуется Go result evaluator.
-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_pdlc_receipt_fact(p_organization uuid,p_node uuid,p_root uuid,p_revision uuid,p_lease uuid,p_receipt text,p_repository text)
RETURNS jsonb LANGUAGE plpgsql VOLATILE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
DECLARE v_count bigint;v_proof jsonb;v_output jsonb;v_scope jsonb;
BEGIN
 SELECT count(*),min(to_jsonb(proof)::text)::jsonb INTO v_count,v_proof
 FROM control_plane.workflow_native_receipt_proof(p_organization,p_node,p_root,p_revision,p_lease,p_receipt) proof;
 IF v_count<>1 OR v_proof->>'lineage_failure' IS DISTINCT FROM ''
    OR v_proof->>'response_digest' IS DISTINCT FROM encode(sha256(convert_to(v_proof->>'result_summary','UTF8')),'hex') THEN RETURN NULL; END IF;
 v_scope:=v_proof->'resource_scope';
 IF jsonb_typeof(v_scope) IS DISTINCT FROM 'object' OR v_scope-ARRAY['owner','repository']<>'{}'::jsonb
    OR jsonb_typeof(v_scope->'owner') IS DISTINCT FROM 'string' OR jsonb_typeof(v_scope->'repository') IS DISTINCT FROM 'string'
    OR (v_scope->>'owner')||'/'||(v_scope->>'repository') IS DISTINCT FROM p_repository THEN RETURN NULL; END IF;
 IF octet_length(v_proof->>'result_summary')>65536 THEN RETURN NULL; END IF;
 v_output:=(v_proof->>'result_summary')::jsonb;
 IF jsonb_typeof(v_output) IS DISTINCT FROM 'object'
    OR EXISTS(SELECT 1 FROM json_each((v_proof->>'result_summary')::json) item GROUP BY item.key HAVING count(*)<>1) THEN RETURN NULL; END IF;
 RETURN jsonb_build_object('operation',v_proof->'operation','input',v_proof->'bounded_input','output',v_output,
    'digest',v_proof->'response_digest','provider',v_proof->'provider_effect_ref',
    'inputDigest',encode(sha256(convert_to((v_proof->'bounded_input')::text,'UTF8')),'hex'));
EXCEPTION WHEN data_exception THEN RETURN NULL;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.workflow_pdlc_receipt_fact(uuid,uuid,uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.workflow_pdlc_receipt_fact(uuid,uuid,uuid,uuid,uuid,text,text) TO control_plane_runtime;

-- Чистое сопоставление уже owner-resolved facts. Внешние payload не вызывают
-- эту функцию вместо resolver; scope/lineage/read chronology доказывает wrapper.
-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_pdlc_result_valid(p_requirement jsonb,p_fact jsonb,p_proofs jsonb[])
RETURNS boolean LANGUAGE plpgsql IMMUTABLE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
DECLARE v_fields text[];v_field text;v_number bigint;v_write jsonb;v_head jsonb;v_content jsonb;
 v_input jsonb;v_output jsonb;v_head_input jsonb;v_head_output jsonb;v_content_input jsonb;v_content_output jsonb;
 v_bytes bytea;v_text bytea;v_digest text;v_max bigint;
BEGIN
 IF COALESCE(p_requirement->>'Kind','') NOT IN('REPOSITORY_FILE','PR_COMMENT','PULL_REQUEST') OR jsonb_typeof(p_fact) IS DISTINCT FROM 'object'
    OR p_fact->>'key' IS DISTINCT FROM p_requirement->>'Key' OR p_fact->>'kind' IS DISTINCT FROM p_requirement->>'Kind'
    OR p_fact->>'repository' IS DISTINCT FROM p_requirement->>'Repository'
    OR COALESCE(p_requirement->>'TargetRef','') !~ '^[1-9][0-9]{0,9}$' THEN RETURN false; END IF;
 v_number:=(p_requirement->>'TargetRef')::bigint;
 IF v_number>2147483647 OR p_fact->'number' IS DISTINCT FROM to_jsonb(v_number) THEN RETURN false; END IF;
 v_fields:=ARRAY['key','kind','repository','number','receiptRef','digest'];
 IF p_requirement->>'Kind'='REPOSITORY_FILE' THEN v_fields:=v_fields||ARRAY['fileName','branch','headSha','headReceiptRef','contentReceiptRef'];
 ELSIF p_requirement->>'Kind'='PULL_REQUEST' THEN v_fields:=v_fields||ARRAY['headSha']; END IF;
 IF p_fact-v_fields<>'{}'::jsonb OR NOT p_fact ?& v_fields THEN RETURN false; END IF;
 FOREACH v_field IN ARRAY v_fields LOOP
    IF v_field<>'number' AND jsonb_typeof(p_fact->v_field) IS DISTINCT FROM 'string' THEN RETURN false; END IF;
 END LOOP;
 IF COALESCE(p_fact->>'digest','') !~ '^[a-f0-9]{64}$' OR cardinality(p_proofs) IS DISTINCT FROM (CASE WHEN p_requirement->>'Kind'='REPOSITORY_FILE' THEN 3 ELSE 1 END)
    OR EXISTS(SELECT 1 FROM unnest(p_proofs) proof WHERE proof IS NULL) THEN RETURN false; END IF;
 v_write:=p_proofs[1];v_input:=v_write->'input';v_output:=v_write->'output';
 IF p_requirement->>'Kind'='PR_COMMENT' THEN
    IF control_plane.workflow_pdlc_author_slot(p_requirement,v_write->>'operation',jsonb_build_object('owner',split_part(p_requirement->>'Repository','/',1),'repository',split_part(p_requirement->>'Repository','/',2)),v_input) IS DISTINCT FROM p_requirement->>'Key'
       OR jsonb_typeof(v_output) IS DISTINCT FROM 'object' OR v_output-ARRAY['pull_request_number','comment_id']<>'{}'::jsonb
       OR v_output->'pull_request_number' IS DISTINCT FROM to_jsonb(v_number) OR COALESCE(v_output->>'comment_id','') !~ '^[1-9][0-9]{0,15}$'
       OR (v_output->>'comment_id')::bigint>9007199254740991 OR v_write->>'provider' IS DISTINCT FROM 'github-comment:'||(v_output->>'comment_id') THEN RETURN false; END IF;
    v_digest:=control_plane.workflow_pdlc_result_digest(ARRAY['kodex.workflow-pr-comment.v1',p_requirement->>'Key',p_requirement->>'Repository',p_requirement->>'TargetRef',p_fact->>'receiptRef',v_write->>'digest',v_write->>'inputDigest']);
    RETURN p_fact->>'digest' IS NOT DISTINCT FROM v_digest;
 ELSIF p_requirement->>'Kind'='PULL_REQUEST' THEN
    v_fields:=ARRAY['number','sha','state','draft','url','already_ready'];
    RETURN COALESCE(v_write->>'operation'='github.pull_request.ready'
       AND control_plane.workflow_pdlc_author_slot(p_requirement,v_write->>'operation',jsonb_build_object('owner',split_part(p_requirement->>'Repository','/',1),'repository',split_part(p_requirement->>'Repository','/',2)),v_input) IS NOT NULL
       AND jsonb_typeof(v_output)='object' AND v_output-v_fields='{}'::jsonb AND v_output ?& v_fields
       AND v_output->'number'=to_jsonb(v_number) AND v_output->>'state'='open' AND v_output->'draft'='false'::jsonb
       AND jsonb_typeof(v_output->'already_ready')='boolean'
       AND v_output->>'sha'=v_input->>'expected_head_sha' AND v_output->>'sha'=p_fact->>'headSha'
       AND v_output->>'url'='https://github.com/'||(p_requirement->>'Repository')||'/pull/'||(p_requirement->>'TargetRef')
       AND v_write->>'provider'='github-pull:'||(p_requirement->>'TargetRef') AND p_fact->>'digest'=v_write->>'digest',false);
 END IF;
 IF control_plane.workflow_pdlc_author_slot(p_requirement,v_write->>'operation',jsonb_build_object('owner',split_part(p_requirement->>'Repository','/',1),'repository',split_part(p_requirement->>'Repository','/',2)),v_input) IS NULL
    OR v_input->>'path' IS DISTINCT FROM p_requirement->>'FileName' OR v_input->>'branch' IS DISTINCT FROM p_requirement->>'Branch'
    OR p_fact->>'fileName' IS DISTINCT FROM p_requirement->>'FileName' OR p_fact->>'branch' IS DISTINCT FROM p_requirement->>'Branch'
    OR jsonb_typeof(v_output) IS DISTINCT FROM 'object' OR v_output-ARRAY['sha','path']<>'{}'::jsonb
    OR jsonb_typeof(v_output->'path') IS DISTINCT FROM 'string' OR jsonb_typeof(v_output->'sha') IS DISTINCT FROM 'string'
    OR v_output->>'path' IS DISTINCT FROM p_requirement->>'FileName' OR COALESCE(v_output->>'sha','') !~ '^([a-f0-9]{40}|[a-f0-9]{64})$'
    OR v_write->>'provider' IS DISTINCT FROM 'github-commit:'||(v_output->>'sha') THEN RETURN false; END IF;
 v_bytes:=decode(v_input->>'content_base64','base64');
 v_head:=p_proofs[2];v_head_input:=v_head->'input';v_head_output:=v_head->'output';
 v_fields:=ARRAY['number','title','body','state','head','base','sha','draft','url','base_sha','changed_files'];
 IF v_head->>'operation' IS DISTINCT FROM 'github.pull_request.read' OR jsonb_typeof(v_head_input) IS DISTINCT FROM 'object'
    OR v_head_input-ARRAY['pull_request_number']<>'{}'::jsonb OR v_head_input->'pull_request_number' IS DISTINCT FROM to_jsonb(v_number)
    OR jsonb_typeof(v_head_output) IS DISTINCT FROM 'object' OR v_head_output-v_fields<>'{}'::jsonb
    OR NOT v_head_output ?& ARRAY['number','state','head','base','sha','draft'] OR v_head_output->'number' IS DISTINCT FROM to_jsonb(v_number)
    OR v_head_output->>'state' IS DISTINCT FROM 'open' OR v_head_output->>'head' IS DISTINCT FROM p_requirement->>'Branch'
    OR jsonb_typeof(v_head_output->'head') IS DISTINCT FROM 'string' OR jsonb_typeof(v_head_output->'base') IS DISTINCT FROM 'string' OR jsonb_typeof(v_head_output->'sha') IS DISTINCT FROM 'string'
    OR COALESCE(v_head_output->>'base','')='' OR v_head_output->>'base'=v_head_output->>'head'
    OR jsonb_typeof(v_head_output->'draft') IS DISTINCT FROM 'boolean' OR COALESCE(v_head_output->>'sha','') !~ '^([a-f0-9]{40}|[a-f0-9]{64})$'
    OR p_fact->>'headSha' IS DISTINCT FROM v_head_output->>'sha' OR v_head->>'provider' IS DISTINCT FROM 'github-pull:'||(p_requirement->>'TargetRef') THEN RETURN false; END IF;
 v_content:=p_proofs[3];v_content_input:=v_content->'input';v_content_output:=v_content->'output';
 v_fields:=ARRAY['path','type','sha','size','text','commit_sha','offset_bytes','next_offset_bytes','eof','source_digest','chunk_digest'];
 IF v_content->>'operation' IS DISTINCT FROM 'github.repository.content.read' OR jsonb_typeof(v_content_input) IS DISTINCT FROM 'object'
    OR v_content_input-ARRAY['path','ref','expected_sha','offset_bytes','max_bytes']<>'{}'::jsonb OR NOT v_content_input ?& ARRAY['path','ref']
    OR v_content_input->>'path' IS DISTINCT FROM p_requirement->>'FileName' OR v_content_input->>'ref' IS DISTINCT FROM p_fact->>'headSha'
    OR jsonb_typeof(v_content_output) IS DISTINCT FROM 'object' OR v_content_output-v_fields<>'{}'::jsonb OR NOT v_content_output ?& v_fields THEN RETURN false; END IF;
 FOREACH v_field IN ARRAY v_fields LOOP
    IF v_field NOT IN('size','offset_bytes','next_offset_bytes','eof') AND jsonb_typeof(v_content_output->v_field) IS DISTINCT FROM 'string' THEN RETURN false; END IF;
 END LOOP;
 FOREACH v_field IN ARRAY ARRAY['size','offset_bytes','next_offset_bytes'] LOOP
    IF (v_content_output->v_field)::text !~ '^[0-9]{1,10}$' THEN RETURN false; END IF;
 END LOOP;
 FOREACH v_field IN ARRAY ARRAY['offset_bytes','max_bytes'] LOOP
    IF v_content_input ? v_field AND (v_content_input->v_field)::text !~ '^[0-9]{1,10}$' THEN RETURN false; END IF;
 END LOOP;
 IF v_content_input ? 'expected_sha' AND (jsonb_typeof(v_content_input->'expected_sha') IS DISTINCT FROM 'string' OR COALESCE(v_content_input->>'expected_sha','')='' OR v_content_input->>'expected_sha' IS DISTINCT FROM v_content_output->>'sha') THEN RETURN false; END IF;
 v_max:=COALESCE((v_content_input->>'max_bytes')::bigint,16384);v_text:=convert_to(v_content_output->>'text','UTF8');
 IF v_max NOT BETWEEN 1 AND 16384 OR COALESCE((v_content_input->>'offset_bytes')::bigint,0)<>0
    OR v_content_output->>'path' IS DISTINCT FROM p_requirement->>'FileName' OR v_content_output->>'type' IS DISTINCT FROM 'file'
    OR COALESCE(v_content_output->>'sha','') !~ '^([a-f0-9]{40}|[a-f0-9]{64})$' OR v_content->>'provider' IS DISTINCT FROM 'github-content:'||(v_content_output->>'sha')
    OR v_content_output->>'commit_sha' IS DISTINCT FROM p_fact->>'headSha' OR (v_content_output->>'offset_bytes')::bigint<>0
    OR (v_content_output->>'size')::bigint<>octet_length(v_bytes) OR (v_content_output->>'next_offset_bytes')::bigint<>octet_length(v_text)
    OR octet_length(v_text)>v_max OR octet_length(v_text)>octet_length(v_bytes)
    OR v_content_output->'eof' IS DISTINCT FROM to_jsonb(octet_length(v_text)=octet_length(v_bytes))
    OR octet_length(v_text)=0 AND v_content_output->'eof'<>'true'::jsonb
    OR v_content_output->>'source_digest' IS DISTINCT FROM 'sha256:'||encode(sha256(v_bytes),'hex')
    OR v_content_output->>'chunk_digest' IS DISTINCT FROM 'sha256:'||encode(sha256(v_text),'hex')
    OR substring(v_bytes FROM 1 FOR octet_length(v_text)) IS DISTINCT FROM v_text THEN RETURN false; END IF;
 v_digest:=control_plane.workflow_pdlc_result_digest(ARRAY['kodex.workflow-repository-file.v1',p_requirement->>'Key',p_requirement->>'Repository',p_requirement->>'Branch',p_requirement->>'FileName',p_requirement->>'TargetRef',p_fact->>'receiptRef',v_write->>'digest',p_fact->>'headReceiptRef',v_head->>'digest',p_fact->>'contentReceiptRef',v_content->>'digest']);
 RETURN p_fact->>'digest' IS NOT DISTINCT FROM v_digest;
EXCEPTION WHEN data_exception THEN RETURN false;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.workflow_pdlc_result_valid(jsonb,jsonb,jsonb[]) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.workflow_pdlc_result_valid(jsonb,jsonb,jsonb[]) TO control_plane_runtime;

-- Применяется только к новому профилю после прежнего exact completed provenance
-- guard. Параметры не назначают actor: owner anchor/root и pinned requirements
-- читаются сервером; каждый receipt проходит тот же current grant reader, что Go.
-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_pdlc_outcome_valid(p_organization uuid,p_anchor uuid,p_entry jsonb,p_requirements jsonb)
RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
DECLARE v_root uuid;v_profile text;v_fact jsonb;v_requirement jsonb;v_proofs jsonb[];v_ref text;v_refs text[];v_count integer;
BEGIN
 SELECT root.id,version.spec->>'ResultEvidenceProfile' INTO v_root,v_profile
 FROM control_plane.run_nodes anchor JOIN control_plane.runs root ON root.id=anchor.root_run_id AND root.organization_id=anchor.organization_id
 JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
 WHERE anchor.id=p_anchor AND anchor.organization_id=p_organization;
 IF NOT FOUND THEN RETURN false; END IF;
 IF v_profile IS DISTINCT FROM 'GITHUB_PDLC_V1' THEN RETURN true; END IF;
 IF p_entry->'accepted' IS DISTINCT FROM 'true'::jsonb THEN RETURN p_entry->'results'='[]'::jsonb; END IF;
 FOR v_fact IN SELECT value FROM jsonb_array_elements(p_entry->'results') LOOP
    SELECT value INTO STRICT v_requirement FROM jsonb_array_elements(p_requirements) requirement(value)
    WHERE value->>'Key'=v_fact->>'key' AND value->>'Kind'=v_fact->>'kind';
    IF v_fact->>'kind' NOT IN('REPOSITORY_FILE','PR_COMMENT','PULL_REQUEST') THEN
       IF v_fact ?| ARRAY['fileName','branch','headReceiptRef','contentReceiptRef'] THEN RETURN false; END IF;
       CONTINUE;
    END IF;
    v_refs:=ARRAY[v_fact->>'receiptRef'];
    IF v_fact->>'kind'='REPOSITORY_FILE' THEN v_refs:=v_refs||ARRAY[v_fact->>'headReceiptRef',v_fact->>'contentReceiptRef']; END IF;
    IF EXISTS(SELECT 1 FROM unnest(v_refs) ref WHERE ref IS NULL OR ref !~ '^[A-Za-z0-9_-]{8,96}$')
       OR (SELECT count(DISTINCT ref) FROM unnest(v_refs) ref)<>cardinality(v_refs) THEN RETURN false; END IF;
    v_proofs:=ARRAY[]::jsonb[];
    FOREACH v_ref IN ARRAY v_refs LOOP
       v_proofs:=array_append(v_proofs,control_plane.workflow_pdlc_receipt_fact(p_organization,(p_entry #>> '{provenance,nodeId}')::uuid,v_root,
          (p_entry->>'runtimeRevisionId')::uuid,(p_entry #>> '{provenance,leaseId}')::uuid,v_ref,v_requirement->>'Repository'));
    END LOOP;
    IF control_plane.workflow_pdlc_result_valid(v_requirement,v_fact,v_proofs) IS DISTINCT FROM true THEN RETURN false; END IF;
    IF v_fact->>'kind'='REPOSITORY_FILE' AND NOT control_plane.workflow_pdlc_read_order(p_organization,v_refs[1],v_refs[2],v_refs[3]) THEN RETURN false; END IF;
 END LOOP;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(p_entry->'results') pull CROSS JOIN jsonb_array_elements(p_entry->'results') file
    WHERE pull->>'kind'='PULL_REQUEST' AND file->>'kind'='REPOSITORY_FILE'
       AND (pull->>'repository' IS DISTINCT FROM file->>'repository' OR pull->'number' IS DISTINCT FROM file->'number' OR pull->>'headSha' IS DISTINCT FROM file->>'headSha')) THEN RETURN false; END IF;
 RETURN true;
EXCEPTION WHEN data_exception OR no_data_found OR too_many_rows THEN RETURN false;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.workflow_pdlc_outcome_valid(uuid,uuid,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.workflow_pdlc_outcome_valid(uuid,uuid,jsonb,jsonb) TO control_plane_runtime;

-- Сохраняем OID, полное именованное объявление и ACL прежнего journal guard.
-- +goose StatementBegin
DO $pdlc_journal$
DECLARE v_definition text;v_source text;v_old text;v_new text;
BEGIN
 SELECT pg_get_functiondef(oid),prosrc INTO STRICT v_definition,v_source FROM pg_proc WHERE oid='control_plane.guard_workflow_attempt_journal()'::regprocedure;
 IF md5(v_source)<>'7f8dc9469eb755a7b7760e8d4d7fc2cd' THEN RAISE EXCEPTION 'PDLC journal guard preimage changed'; END IF;
 v_old:=$old$IF NOT control_plane.workflow_evidence_outcome_valid(NEW.organization_id,NEW.id,entry,requirements) THEN$old$;
 v_new:=$new$IF NOT control_plane.workflow_evidence_outcome_valid(NEW.organization_id,NEW.id,entry,requirements)
        OR control_plane.workflow_pdlc_outcome_valid(NEW.organization_id,NEW.id,entry,requirements) IS DISTINCT FROM true THEN$new$;
 IF (length(v_source)-length(replace(v_source,v_old,'')))/length(v_old)<>1 THEN RAISE EXCEPTION 'PDLC outcome guard patch is ambiguous'; END IF;
 EXECUTE replace(v_definition,v_source,replace(v_source,v_old,v_new));
END $pdlc_journal$;
-- +goose StatementEnd

-- Согласованный opt-in сохраняет весь прежний authority/lifecycle body и ACL.
-- +goose StatementBegin
DO $profile_patch$
DECLARE v_definition text;v_source text;v_updated text;v_old text;v_new text;
BEGIN
 SELECT pg_get_functiondef(oid),prosrc INTO STRICT v_definition,v_source FROM pg_proc WHERE oid='control_plane.workflow_evidence_intent_valid(uuid,uuid,uuid,uuid,jsonb,jsonb)'::regprocedure;
 IF md5(v_source)<>'9f92d166abb3267d2c3f1dc1a8e9ad1c' THEN RAISE EXCEPTION 'PDLC profile helper preimage changed'; END IF;
 v_updated:=v_source;
 v_old:=$old$ version.spec->>'ExecutionMode'='SERVER_DAG_V1' AND version.spec->>'ResultEvidenceProfile'='GITHUB_REVIEW_QA_V1'
 ), subjects AS MATERIALIZED (
    SELECT result.value FROM execution
    CROSS JOIN LATE$old$;
 v_new:=$new$ version.spec->>'ExecutionMode'='SERVER_DAG_V1' AND version.spec->>'ResultEvidenceProfile' IN ('GITHUB_REVIEW_QA_V1','GITHUB_PDLC_V1')
 ), subjects AS MATERIALIZED (
    SELECT result.value FROM execution
    CROSS JOIN LATE$new$;
 IF (length(v_updated)-length(replace(v_updated,v_old,'')))/length(v_old)<>1 THEN RAISE EXCEPTION 'PDLC profile patch 1 is ambiguous'; END IF;
 v_updated:=replace(v_updated,v_old,v_new);
 EXECUTE replace(v_definition,v_source,v_updated);
END $profile_patch$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $profile_patch$
DECLARE v_definition text;v_source text;v_updated text;v_old text;v_new text;
BEGIN
 SELECT pg_get_functiondef(oid),prosrc INTO STRICT v_definition,v_source FROM pg_proc WHERE oid='control_plane.workflow_evidence_retains_revision(uuid,uuid,uuid)'::regprocedure;
 IF md5(v_source)<>'c347038ad84997023ea7f23b14fc527b' THEN RAISE EXCEPTION 'PDLC profile helper preimage changed'; END IF;
 v_updated:=v_source;
 v_old:=$old$ version.spec->>'ExecutionMode'='SERVER_DAG_V1' AND version.spec->>'ResultEvidenceProfile'='GITHUB_REVIEW_QA_V1'
    CROSS JOIN LATERAL jsonb_array_elements(anchor.workflow_effect_intents) intent
    JO$old$;
 v_new:=$new$ version.spec->>'ExecutionMode'='SERVER_DAG_V1' AND version.spec->>'ResultEvidenceProfile' IN ('GITHUB_REVIEW_QA_V1','GITHUB_PDLC_V1')
    CROSS JOIN LATERAL jsonb_array_elements(anchor.workflow_effect_intents) intent
    JO$new$;
 IF (length(v_updated)-length(replace(v_updated,v_old,'')))/length(v_old)<>1 THEN RAISE EXCEPTION 'PDLC profile patch 1 is ambiguous'; END IF;
 v_updated:=replace(v_updated,v_old,v_new);
 EXECUTE replace(v_definition,v_source,v_updated);
END $profile_patch$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $profile_patch$
DECLARE v_definition text;v_source text;v_updated text;v_old text;v_new text;
BEGIN
 SELECT pg_get_functiondef(oid),prosrc INTO STRICT v_definition,v_source FROM pg_proc WHERE oid='control_plane.workflow_evidence_outcome_valid(uuid,uuid,jsonb,jsonb)'::regprocedure;
 IF md5(v_source)<>'c873f7e8247b0f54f0c0cf424ac7ba18' THEN RAISE EXCEPTION 'PDLC profile helper preimage changed'; END IF;
 v_updated:=v_source;
 v_old:=$old$ion_id=p_tenant WHERE root.id=v_anchor.root_run_id AND root.organization_id=p_tenant;
 IF v_spec->>'ResultEvidenceProfile' IS DISTINCT FROM 'GITHUB_REVIEW_QA_V1'
    OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_requirements) requirement(value) W$old$;
 v_new:=$new$ion_id=p_tenant WHERE root.id=v_anchor.root_run_id AND root.organization_id=p_tenant;
 IF (v_spec->>'ResultEvidenceProfile' IS DISTINCT FROM 'GITHUB_REVIEW_QA_V1' AND v_spec->>'ResultEvidenceProfile' IS DISTINCT FROM 'GITHUB_PDLC_V1')
    OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_requirements) requirement(value) W$new$;
 IF (length(v_updated)-length(replace(v_updated,v_old,'')))/length(v_old)<>1 THEN RAISE EXCEPTION 'PDLC profile patch 1 is ambiguous'; END IF;
 v_updated:=replace(v_updated,v_old,v_new);
 v_old:=$old$v_fact-ARRAY['key','kind','digest','artifactRef','artifactRevision','receiptRef','repository','number','headSha','verdict','targetRef','bundleSha256','imageDigest','healthOk']$old$;
 v_new:=$new$v_fact-(ARRAY['key','kind','digest','artifactRef','artifactRevision','receiptRef','repository','number','headSha','verdict','targetRef','bundleSha256','imageDigest','healthOk']||CASE WHEN v_spec->>'ResultEvidenceProfile'='GITHUB_PDLC_V1' THEN ARRAY['fileName','branch','headReceiptRef','contentReceiptRef'] ELSE ARRAY[]::text[] END)$new$;
 IF (length(v_updated)-length(replace(v_updated,v_old,'')))/length(v_old)<>1 THEN RAISE EXCEPTION 'PDLC outcome shape patch is ambiguous'; END IF;
 v_updated:=replace(v_updated,v_old,v_new);
 EXECUTE replace(v_definition,v_source,v_updated);
END $profile_patch$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $profile_patch$
DECLARE v_definition text;v_source text;v_updated text;v_old text;v_new text;
BEGIN
 SELECT pg_get_functiondef(oid),prosrc INTO STRICT v_definition,v_source FROM pg_proc WHERE oid='control_plane.workflow_evidence_execution_assigned(uuid,uuid,uuid)'::regprocedure;
 IF md5(v_source)<>'1fb462b1ccbdce5a35e6dcb497be0d40' THEN RAISE EXCEPTION 'PDLC profile helper preimage changed'; END IF;
 v_updated:=v_source;
 v_old:=$old$ion.run_id=physical.id AND invocation.node_id=node.id AND invocation.risk='WRITE'
    AND invocation.operation IN ('github.pull_request.review.create','github.issue.create')
 JOIN control_plane.integration_connections connection ON connection.id=invocation.conne$old$;
 v_new:=$new$ion.run_id=physical.id AND invocation.node_id=node.id AND invocation.risk='WRITE'
    AND control_plane.workflow_evidence_delivery_operation(version.spec->>'ResultEvidenceProfile',invocation.operation)
 JOIN control_plane.integration_connections connection ON connection.id=invocation.conne$new$;
 IF (length(v_updated)-length(replace(v_updated,v_old,'')))/length(v_old)<>1 THEN RAISE EXCEPTION 'PDLC profile patch 1 is ambiguous'; END IF;
 v_updated:=replace(v_updated,v_old,v_new);
 v_old:=$old$ version.spec->>'ExecutionMode'='SERVER_DAG_V1' AND version.spec->>'ResultEvidenceProfile'='GITHUB_REVIEW_QA_V1'
    AND physical.workflow_version_id=root.workflow_version_id AND physical.target_type IN$old$;
 v_new:=$new$ version.spec->>'ExecutionMode'='SERVER_DAG_V1' AND version.spec->>'ResultEvidenceProfile' IN ('GITHUB_REVIEW_QA_V1','GITHUB_PDLC_V1')
    AND physical.workflow_version_id=root.workflow_version_id AND physical.target_type IN$new$;
 IF (length(v_updated)-length(replace(v_updated,v_old,'')))/length(v_old)<>1 THEN RAISE EXCEPTION 'PDLC profile patch 2 is ambiguous'; END IF;
 v_updated:=replace(v_updated,v_old,v_new);
 v_old:=$old$alue->>'Key'=node.workflow_step_key AND step.value->>'AgentRef'=agent.ref
            AND requirement.value->>'Key'=intent.value->>'resultKey'
            AND ((requirement.value->$old$;
 v_new:=$new$alue->>'Key'=node.workflow_step_key AND step.value->>'AgentRef'=agent.ref
            AND ((requirement.value->>'Key'=intent.value->>'resultKey'
            AND ((requirement.value->$new$;
 IF (length(v_updated)-length(replace(v_updated,v_old,'')))/length(v_old)<>1 THEN RAISE EXCEPTION 'PDLC profile patch 3 is ambiguous'; END IF;
 v_updated:=replace(v_updated,v_old,v_new);
 v_old:=$old$         OR (requirement.value->>'Kind'='QA' AND invocation.operation='github.issue.create')))
    AND revision.generation=(SELECT max(latest.generation) FROM control_plane.runtime$old$;
 v_new:=$new$         OR (requirement.value->>'Kind'='QA' AND invocation.operation='github.issue.create')))
            OR (version.spec->>'ResultEvidenceProfile'='GITHUB_PDLC_V1'
                AND control_plane.workflow_pdlc_author_slot(requirement.value,invocation.operation,invocation.resource_scope,invocation.bounded_input)=intent.value->>'resultKey')))
    AND revision.generation=(SELECT max(latest.generation) FROM control_plane.runtime$new$;
 IF (length(v_updated)-length(replace(v_updated,v_old,'')))/length(v_old)<>1 THEN RAISE EXCEPTION 'PDLC profile patch 4 is ambiguous'; END IF;
 v_updated:=replace(v_updated,v_old,v_new);
 EXECUTE replace(v_definition,v_source,v_updated);
END $profile_patch$;
-- +goose StatementEnd

-- Join и полное schema/backup proof ещё входят в эту не опубликованную миграцию.
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'workflow PDLC migration is forward-only'; END $$;
-- +goose StatementEnd
