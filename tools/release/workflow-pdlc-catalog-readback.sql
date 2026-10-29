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
);
