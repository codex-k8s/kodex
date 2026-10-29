-- Generated: generate-workflow-content-page-proof.py; #1975.
-- +goose Up
SET LOCAL lock_timeout='8s';
SET LOCAL statement_timeout='30s';
-- +goose StatementBegin
DO $predecessor$
BEGIN
 IF (SELECT max(version_id) FROM public.goose_db_version WHERE is_applied) IS DISTINCT FROM 20261029000300::bigint THEN
    RAISE EXCEPTION 'workflow content page requires exact migration031';
 END IF;
END $predecessor$;
-- +goose StatementEnd
SET ROLE control_plane_owner;
-- +goose StatementBegin
-- Actual031 проверяется до исторической проекции, в том же snapshot.
DO $workflow_pdlc_catalog$
DECLARE v_actual jsonb;
BEGIN
-- Только schema metadata: без бизнес-строк, credential values и runtime payload.
WITH function_names(name) AS (VALUES
 ('guard_workflow_effect_records'),('guard_workflow_attempt_journal'),
 ('workflow_evidence_intent_valid'),('workflow_evidence_retains_revision'),
 ('workflow_evidence_outcome_valid'),('workflow_evidence_execution_assigned'),
 ('workflow_pdlc_file_key'),('workflow_pdlc_file_desired_digest'),('workflow_pdlc_file_epoch_digest'),
 ('workflow_pdlc_result_digest'),('workflow_evidence_delivery_operation'),('workflow_pdlc_read_order'),
 ('workflow_pdlc_author_slot'),('workflow_pdlc_intent_valid'),('workflow_receipt_effect_proof'),
 ('workflow_receipt_lineage_proof'),('workflow_native_receipt_proof'),('workflow_pdlc_receipt_fact'),
 ('workflow_pdlc_result_valid'),('workflow_pdlc_outcome_valid')
), functions AS (
 SELECT p.*,l.lanname FROM pg_catalog.pg_proc p
 JOIN pg_catalog.pg_language l ON l.oid=p.prolang
 WHERE p.pronamespace='control_plane'::regnamespace AND p.proname IN(SELECT name FROM function_names)
), function_catalog AS (
 SELECT 'control_plane.'||quote_ident(p.proname)||'('||array_to_string(ARRAY(SELECT format_type(t,NULL) FROM unnest(p.proargtypes) t),',')||')' AS signature,jsonb_build_object(
  'sourceMD5',md5(p.prosrc),'sourceSHA256',encode(sha256(convert_to(p.prosrc,'UTF8')),'hex'),
  'owner',pg_get_userbyid(p.proowner),'language',p.lanname,'kind',p.prokind,
  'securityDefiner',p.prosecdef,'volatility',p.provolatile,'parallel',p.proparallel,
  'strict',p.proisstrict,'leakproof',p.proleakproof,'set',p.proretset,
  'result',format_type(p.prorettype,NULL),'resultDefinition',pg_get_function_result(p.oid),
  'inputTypes',ARRAY(SELECT format_type(t,NULL) FROM unnest(p.proargtypes) t),
  'allTypes',CASE WHEN p.proallargtypes IS NULL THEN NULL ELSE ARRAY(SELECT format_type(t,NULL) FROM unnest(p.proallargtypes) t) END,
  'argumentNames',p.proargnames,'argumentModes',p.proargmodes,
  'defaults',p.pronargdefaults,'defaultExpression',pg_get_expr(p.proargdefaults,0),
  'variadic',p.provariadic::regtype::text,'support',p.prosupport::text,'config',p.proconfig,
  'acl',(SELECT jsonb_agg(jsonb_build_array(CASE WHEN a.grantee=0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END,
    pg_get_userbyid(a.grantor),a.privilege_type,a.is_grantable)
    ORDER BY a.grantee=0,pg_get_userbyid(a.grantee),pg_get_userbyid(a.grantor),a.privilege_type,a.is_grantable)
    FROM aclexplode(COALESCE(p.proacl,acldefault('f',p.proowner))) a)
 ) AS metadata FROM functions p
), trigger_catalog AS (
 SELECT n.nspname||'.'||r.relname||'.'||t.tgname AS name,jsonb_build_array(
  'control_plane.'||quote_ident(p.proname)||'()',t.tgtype,t.tgenabled,t.tgisinternal,t.tgdeferrable,t.tginitdeferred,
  t.tgnargs,t.tgattr::text,pg_get_expr(t.tgqual,t.tgrelid),encode(t.tgargs,'hex'),
  t.tgoldtable,t.tgnewtable,t.tgparentid<>0,t.tgconstraint<>0) AS metadata
 FROM pg_trigger t JOIN pg_class r ON r.oid=t.tgrelid JOIN pg_namespace n ON n.oid=r.relnamespace
 JOIN functions p ON p.oid=t.tgfoid
), constraint_catalog AS (
 SELECT jsonb_build_array(c.contype,c.convalidated,c.conenforced,c.conislocal,c.connoinherit,c.condeferrable,
  c.condeferred,c.coninhcount,pg_get_constraintdef(c.oid)) AS metadata
 FROM pg_constraint c WHERE c.conrelid='control_plane.run_nodes'::regclass AND c.conname='workflow_effect_record_bounds'
)
SELECT jsonb_build_object(
 'functions',(SELECT jsonb_object_agg(signature,metadata ORDER BY signature) FROM function_catalog),
 'functionCount',(SELECT count(*) FROM functions),
 'triggers',(SELECT jsonb_object_agg(name,metadata ORDER BY name) FROM trigger_catalog),
 'effectBounds',(SELECT metadata FROM constraint_catalog)
) INTO v_actual;
 IF v_actual IS DISTINCT FROM $catalog${"effectBounds":["c",true,true,true,false,false,false,0,"CHECK (((jsonb_array_length(workflow_effect_intents) <= 64) AND (jsonb_array_length(workflow_effect_rejoins) <= 64)))"],"functionCount":20,"functions":{"control_plane.guard_workflow_attempt_journal()":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":null,"config":["search_path=pg_catalog, control_plane"],"defaultExpression":null,"defaults":0,"inputTypes":[],"kind":"f","language":"plpgsql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"trigger","resultDefinition":"trigger","securityDefiner":false,"set":false,"sourceMD5":"305ca9b25f0d0fa31f59abb0b7b1d7fa","sourceSHA256":"2cf89c7a8e11054480095b07c2e31c69c6ab502b942134c3dc224e1e909bbe08","strict":false,"support":"-","variadic":"-","volatility":"v"},"control_plane.guard_workflow_effect_records()":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":null,"config":["search_path=pg_catalog, control_plane"],"defaultExpression":null,"defaults":0,"inputTypes":[],"kind":"f","language":"plpgsql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"trigger","resultDefinition":"trigger","securityDefiner":false,"set":false,"sourceMD5":"cc1954201904c1193238a5fbacf51c6f","sourceSHA256":"be1b491c6ea33f10c2ea2ffd45d5e61915afebab040667003e7c372e80140ac1","strict":false,"support":"-","variadic":"-","volatility":"v"},"control_plane.workflow_evidence_delivery_operation(text,text)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":["p_profile","p_operation"],"config":["search_path=pg_catalog"],"defaultExpression":null,"defaults":0,"inputTypes":["text","text"],"kind":"f","language":"sql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"boolean","resultDefinition":"boolean","securityDefiner":false,"set":false,"sourceMD5":"bb5a26aa9776ece834d5aee67768cddf","sourceSHA256":"2bc2f07db5be45b90b7052da22cbb73cf884605240060916d8754afa5c122523","strict":false,"support":"-","variadic":"-","volatility":"i"},"control_plane.workflow_evidence_execution_assigned(uuid,uuid,uuid)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":["p_tenant","p_node","p_invocation"],"config":["search_path=pg_catalog, control_plane"],"defaultExpression":null,"defaults":0,"inputTypes":["uuid","uuid","uuid"],"kind":"f","language":"sql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"boolean","resultDefinition":"boolean","securityDefiner":false,"set":false,"sourceMD5":"f38f1e6f9b6f5d0426b992d169642b13","sourceSHA256":"485a06162c0644d6c91fcc0ef1da5cb6fa6e2737d4fe8397c3747eff4f9ee591","strict":false,"support":"-","variadic":"-","volatility":"s"},"control_plane.workflow_evidence_intent_valid(uuid,uuid,uuid,uuid,jsonb,jsonb)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":["p_tenant","p_anchor","p_origin","p_invocation","p_record","p_requirement"],"config":["search_path=pg_catalog, control_plane"],"defaultExpression":null,"defaults":0,"inputTypes":["uuid","uuid","uuid","uuid","jsonb","jsonb"],"kind":"f","language":"sql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"boolean","resultDefinition":"boolean","securityDefiner":false,"set":false,"sourceMD5":"21e14acf595ce49ec3d447386544a982","sourceSHA256":"f9d46b9aed546087fbb4960231cfb6ffaea8e9da8891f940b5955851c828620a","strict":false,"support":"-","variadic":"-","volatility":"s"},"control_plane.workflow_evidence_outcome_valid(uuid,uuid,jsonb,jsonb)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":["p_tenant","p_anchor","p_entry","p_requirements"],"config":["search_path=pg_catalog, control_plane"],"defaultExpression":null,"defaults":0,"inputTypes":["uuid","uuid","jsonb","jsonb"],"kind":"f","language":"plpgsql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"boolean","resultDefinition":"boolean","securityDefiner":false,"set":false,"sourceMD5":"936275d8c7170cd2057ca1165f84a872","sourceSHA256":"b246e2bfb60861fbc12e8dc71c5db351d6ba43303d90e3d8ccc737beaaba01b1","strict":false,"support":"-","variadic":"-","volatility":"s"},"control_plane.workflow_evidence_retains_revision(uuid,uuid,uuid)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":["p_tenant","p_artifact","p_revision"],"config":["search_path=pg_catalog"],"defaultExpression":null,"defaults":0,"inputTypes":["uuid","uuid","uuid"],"kind":"f","language":"sql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"boolean","resultDefinition":"boolean","securityDefiner":false,"set":false,"sourceMD5":"695139aadf8ca154b86b5cd7cbac4df1","sourceSHA256":"cc94d69b52a28fb29f37feab7c2170a286b0194445a013c7f0b14a5c534bde07","strict":false,"support":"-","variadic":"-","volatility":"s"},"control_plane.workflow_native_receipt_proof(uuid,uuid,uuid,uuid,uuid,text)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":["uuid","uuid","uuid","uuid","uuid","text","text","jsonb","jsonb","text","text","text","text"],"argumentModes":["i","i","i","i","i","i","t","t","t","t","t","t","t"],"argumentNames":["p_organization","p_node","p_root","p_revision","p_lease","p_receipt","operation","bounded_input","resource_scope","result_summary","response_digest","provider_effect_ref","lineage_failure"],"config":["search_path=pg_catalog, control_plane"],"defaultExpression":null,"defaults":0,"inputTypes":["uuid","uuid","uuid","uuid","uuid","text"],"kind":"f","language":"plpgsql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"record","resultDefinition":"TABLE(operation text, bounded_input jsonb, resource_scope jsonb, result_summary text, response_digest text, provider_effect_ref text, lineage_failure text)","securityDefiner":false,"set":true,"sourceMD5":"0ddc47cbef1ef5594bba792b3dcdfd35","sourceSHA256":"f62172cc6c286f0266ba91669b0fcebe0b4be13673f94d49f7127e62c6a7fadd","strict":false,"support":"-","variadic":"-","volatility":"v"},"control_plane.workflow_pdlc_author_slot(jsonb,text,jsonb,jsonb)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":["p_requirement","p_operation","p_scope","p_input"],"config":["search_path=pg_catalog"],"defaultExpression":null,"defaults":0,"inputTypes":["jsonb","text","jsonb","jsonb"],"kind":"f","language":"plpgsql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"text","resultDefinition":"text","securityDefiner":false,"set":false,"sourceMD5":"0b7fdc137f2570b88ba96dd18ee1d510","sourceSHA256":"5831778199839bd07c7a7a806fab58ceed4833a4b550ee7e8cb48187bc5e097b","strict":false,"support":"-","variadic":"-","volatility":"i"},"control_plane.workflow_pdlc_file_desired_digest(text,text,jsonb)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":["p_result_key","p_scope_digest","p_input"],"config":["search_path=pg_catalog"],"defaultExpression":null,"defaults":0,"inputTypes":["text","text","jsonb"],"kind":"f","language":"plpgsql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"text","resultDefinition":"text","securityDefiner":false,"set":false,"sourceMD5":"40873996ea381ba3ff60c608d7cd2011","sourceSHA256":"9515440e92f8c01d2d75ea48fd37a859c529a706f15da0b34fb11305581d4977","strict":false,"support":"-","variadic":"-","volatility":"i"},"control_plane.workflow_pdlc_file_epoch_digest(text,text)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":["p_desired","p_predecessor"],"config":["search_path=pg_catalog"],"defaultExpression":null,"defaults":0,"inputTypes":["text","text"],"kind":"f","language":"sql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"text","resultDefinition":"text","securityDefiner":false,"set":false,"sourceMD5":"b0863a78f87ec5bfc70521c9b3d20231","sourceSHA256":"e9a3d0c1cfb82b8756cc48a30ea93cc4076926aa4eb2758d03cabcb61ded11f1","strict":false,"support":"-","variadic":"-","volatility":"i"},"control_plane.workflow_pdlc_file_key(text,text)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":["p_result_key","p_path"],"config":["search_path=pg_catalog"],"defaultExpression":null,"defaults":0,"inputTypes":["text","text"],"kind":"f","language":"sql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"text","resultDefinition":"text","securityDefiner":false,"set":false,"sourceMD5":"5b57a61050ae5779126623d7693e883c","sourceSHA256":"5d78e7de495072b90d25b6208eb92eb201c8ae9c13b045674777d42e44dfd640","strict":false,"support":"-","variadic":"-","volatility":"i"},"control_plane.workflow_pdlc_intent_valid(uuid,uuid,uuid,uuid,jsonb,jsonb)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":["p_tenant","p_anchor","p_origin","p_invocation","p_record","p_requirement"],"config":["search_path=pg_catalog, control_plane"],"defaultExpression":null,"defaults":0,"inputTypes":["uuid","uuid","uuid","uuid","jsonb","jsonb"],"kind":"f","language":"plpgsql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"boolean","resultDefinition":"boolean","securityDefiner":false,"set":false,"sourceMD5":"d8555a79c977cd437dbef29a45472eba","sourceSHA256":"1ee34a451813d9abb5c9fa3fd7fcd80c7bcbb9f00b070164b27fd026610cb338","strict":false,"support":"-","variadic":"-","volatility":"s"},"control_plane.workflow_pdlc_outcome_valid(uuid,uuid,jsonb,jsonb)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":["p_organization","p_anchor","p_entry","p_requirements"],"config":["search_path=pg_catalog, control_plane"],"defaultExpression":null,"defaults":0,"inputTypes":["uuid","uuid","jsonb","jsonb"],"kind":"f","language":"plpgsql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"boolean","resultDefinition":"boolean","securityDefiner":false,"set":false,"sourceMD5":"dafc8c0e1ca57ef52ed42911dde316a5","sourceSHA256":"51bcff7b1dbba237ecebe9bcd4f4eb708c28ff498f265f2e3181e9437e779fcf","strict":false,"support":"-","variadic":"-","volatility":"v"},"control_plane.workflow_pdlc_read_order(uuid,text,text,text)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":["p_organization","p_write","p_head","p_content"],"config":["search_path=pg_catalog, control_plane"],"defaultExpression":null,"defaults":0,"inputTypes":["uuid","text","text","text"],"kind":"f","language":"sql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"boolean","resultDefinition":"boolean","securityDefiner":false,"set":false,"sourceMD5":"9fdff248e9773cc3551afff38df5f599","sourceSHA256":"9569560201e12b79316b4fbaf27fe35075b286cfac5cd41826b6e4bb1103b9cc","strict":false,"support":"-","variadic":"-","volatility":"s"},"control_plane.workflow_pdlc_receipt_fact(uuid,uuid,uuid,uuid,uuid,text,text)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":["p_organization","p_node","p_root","p_revision","p_lease","p_receipt","p_repository"],"config":["search_path=pg_catalog, control_plane"],"defaultExpression":null,"defaults":0,"inputTypes":["uuid","uuid","uuid","uuid","uuid","text","text"],"kind":"f","language":"plpgsql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"jsonb","resultDefinition":"jsonb","securityDefiner":false,"set":false,"sourceMD5":"0f3c2939f9208dcac4784855b88bf371","sourceSHA256":"47781d03fc4157d8f69e7fea688716d5df41473047f520971c6564aa446d8575","strict":false,"support":"-","variadic":"-","volatility":"v"},"control_plane.workflow_pdlc_result_digest(text[])":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":["p_parts"],"config":["search_path=pg_catalog"],"defaultExpression":null,"defaults":0,"inputTypes":["text[]"],"kind":"f","language":"plpgsql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"text","resultDefinition":"text","securityDefiner":false,"set":false,"sourceMD5":"f1643ab13f9b11750746a4271223d779","sourceSHA256":"c0733253fac2b7e52cbdfa41e21c5c5b9a0cff0ff08efa0c37ce1a7ffef31926","strict":false,"support":"-","variadic":"-","volatility":"i"},"control_plane.workflow_pdlc_result_valid(jsonb,jsonb,jsonb[])":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":["p_requirement","p_fact","p_proofs"],"config":["search_path=pg_catalog, control_plane"],"defaultExpression":null,"defaults":0,"inputTypes":["jsonb","jsonb","jsonb[]"],"kind":"f","language":"plpgsql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"boolean","resultDefinition":"boolean","securityDefiner":false,"set":false,"sourceMD5":"e605964880369802a929370f3cf32eb7","sourceSHA256":"00eb403c68600d4d7e7a6241c2ae6374f4ab4b39e143406e9fa34f38ac9db092","strict":false,"support":"-","variadic":"-","volatility":"i"},"control_plane.workflow_receipt_effect_proof(uuid,uuid,uuid,uuid,uuid,text)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":["uuid","uuid","uuid","uuid","uuid","text","text","jsonb","jsonb","text","text","text","text"],"argumentModes":["i","i","i","i","i","i","t","t","t","t","t","t","t"],"argumentNames":["p_organization","p_node","p_root","p_revision","p_lease","p_receipt","operation","bounded_input","resource_scope","result_summary","response_digest","provider_effect_ref","lineage_failure"],"config":["search_path=pg_catalog, control_plane"],"defaultExpression":null,"defaults":0,"inputTypes":["uuid","uuid","uuid","uuid","uuid","text"],"kind":"f","language":"sql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"record","resultDefinition":"TABLE(operation text, bounded_input jsonb, resource_scope jsonb, result_summary text, response_digest text, provider_effect_ref text, lineage_failure text)","securityDefiner":false,"set":true,"sourceMD5":"2412f777ad7c6486f07b5639444e437c","sourceSHA256":"1b8bb38886ee5a2f5bb5d0eb224e89952ec3df449647c82b7f3177d5b2e003a7","strict":false,"support":"-","variadic":"-","volatility":"v"},"control_plane.workflow_receipt_lineage_proof(uuid,uuid,uuid,uuid,uuid,text)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":["uuid","uuid","uuid","uuid","uuid","text","text","jsonb","jsonb","text","text","text","text"],"argumentModes":["i","i","i","i","i","i","t","t","t","t","t","t","t"],"argumentNames":["p_organization","p_node","p_root","p_revision","p_lease","p_receipt","operation","bounded_input","resource_scope","result_summary","response_digest","provider_effect_ref","lineage_failure"],"config":["search_path=pg_catalog, control_plane"],"defaultExpression":null,"defaults":0,"inputTypes":["uuid","uuid","uuid","uuid","uuid","text"],"kind":"f","language":"sql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"record","resultDefinition":"TABLE(operation text, bounded_input jsonb, resource_scope jsonb, result_summary text, response_digest text, provider_effect_ref text, lineage_failure text)","securityDefiner":false,"set":true,"sourceMD5":"f3ae4c35bdf3ebbb9f01f4c721fda23a","sourceSHA256":"ac51fff3ebcbf1d8ea8cc851b44d7c63b680585669b35930c78d518a1bed2c5a","strict":false,"support":"-","variadic":"-","volatility":"v"}},"triggers":{"control_plane.run_nodes.workflow_attempt_journal_guard":["control_plane.guard_workflow_attempt_journal()",23,"O",false,false,false,0,"",null,"",null,null,false,false],"control_plane.run_nodes.workflow_effect_records_guard":["control_plane.guard_workflow_effect_records()",31,"O",false,false,false,0,"",null,"",null,null,false,false]}}$catalog$::jsonb THEN
    RAISE EXCEPTION 'workflow PDLC schema catalog changed';
 END IF;
END $workflow_pdlc_catalog$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.workflow_pdlc_result_valid(p_requirement jsonb,p_fact jsonb,p_proofs jsonb[])
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
    OR v_content_input-ARRAY['path','ref','expected_sha','offset_bytes','maximum_bytes']<>'{}'::jsonb OR NOT v_content_input ?& ARRAY['path','ref']
    OR v_content_input->>'path' IS DISTINCT FROM p_requirement->>'FileName' OR v_content_input->>'ref' IS DISTINCT FROM p_fact->>'headSha'
    OR jsonb_typeof(v_content_output) IS DISTINCT FROM 'object' OR v_content_output-v_fields<>'{}'::jsonb OR NOT v_content_output ?& v_fields THEN RETURN false; END IF;
 FOREACH v_field IN ARRAY v_fields LOOP
    IF v_field NOT IN('size','offset_bytes','next_offset_bytes','eof') AND jsonb_typeof(v_content_output->v_field) IS DISTINCT FROM 'string' THEN RETURN false; END IF;
 END LOOP;
 FOREACH v_field IN ARRAY ARRAY['size','offset_bytes','next_offset_bytes'] LOOP
    IF (v_content_output->v_field)::text !~ '^[0-9]{1,10}$' THEN RETURN false; END IF;
 END LOOP;
 FOREACH v_field IN ARRAY ARRAY['offset_bytes','maximum_bytes'] LOOP
    IF v_content_input ? v_field AND (v_content_input->v_field)::text !~ '^[0-9]{1,10}$' THEN RETURN false; END IF;
 END LOOP;
 IF v_content_input ? 'expected_sha' AND (jsonb_typeof(v_content_input->'expected_sha') IS DISTINCT FROM 'string' OR COALESCE(v_content_input->>'expected_sha','')='' OR v_content_input->>'expected_sha' IS DISTINCT FROM v_content_output->>'sha') THEN RETURN false; END IF;
 v_max:=COALESCE((v_content_input->>'maximum_bytes')::bigint,16384);v_text:=convert_to(v_content_output->>'text','UTF8');
 IF v_max NOT BETWEEN 4 AND 16384 OR COALESCE((v_content_input->>'offset_bytes')::bigint,0)<>0
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
RESET ROLE;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'workflow content page migration is forward-only'; END $$;
-- +goose StatementEnd
