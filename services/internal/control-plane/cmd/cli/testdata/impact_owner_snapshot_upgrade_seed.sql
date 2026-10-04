BEGIN;
SET LOCAL ROLE control_plane_owner;

-- Только состояние реально существующей предыдущей схемы; V2-колонки ещё нет.
DO $$
DECLARE
 tenant uuid := '70000000-0000-4000-8000-000000000001';
 actor uuid := '70000000-0000-4000-8000-000000000002';
 project uuid := '70000000-0000-4000-8000-000000000003';
 role_id uuid := gen_random_uuid();
 recipe uuid := gen_random_uuid();
 build uuid := gen_random_uuid();
 artifact uuid := gen_random_uuid();
 configuration uuid := gen_random_uuid();
 revision uuid := gen_random_uuid();
 secret uuid := gen_random_uuid();
 draft uuid := gen_random_uuid();
 operation uuid := '70000000-0000-4000-8000-000000000004';
 plan_id uuid;
 suffix text;
 position integer;
BEGIN
 IF EXISTS (SELECT 1 FROM information_schema.columns
   WHERE table_schema='control_plane' AND column_name='owner_snapshot_revision') THEN
  RAISE EXCEPTION 'historical fixture requires the previous schema';
 END IF;
 INSERT INTO control_plane.organizations(id,ref,name) VALUES(tenant,'org_impact_upgrade','Impact upgrade fixture');
 INSERT INTO control_plane.subjects(id,organization_id,ref,issuer,external_subject_digest,display_name)
  VALUES(actor,tenant,'sub_impact_upgrade','https://fixture.example.test',repeat('a',64),'Synthetic migration actor');
 INSERT INTO control_plane.projects(id,ref,organization_id,name,created_by)
  VALUES(project,'prj_impact_upgrade',tenant,'Synthetic migration project',actor);
 INSERT INTO control_plane.role_definitions(id,ref,organization_id,project_id,name,role_type,created_by)
  VALUES(role_id,'rol_impact_upgrade',tenant,project,'Synthetic role','developer',actor);
 INSERT INTO control_plane.role_image_recipes(id,ref,organization_id,project_id,role_definition_id,name,state,
   specification,generation,spec_sha256,policy_revision,policy_sha256,role_runtime_contract_revision,
   role_runtime_contract_sha256,created_by,scope_kind)
  VALUES(recipe,'imgrec_impact_upgrade',tenant,project,role_id,'Synthetic image','ACTIVE','{}',1,repeat('b',64),
   1,repeat('c',64),1,repeat('d',64),actor,'PROJECT');
 INSERT INTO control_plane.image_builds(id,ref,organization_id,project_id,recipe_id,recipe_version,recipe_generation,
   specification,spec_sha256,immutable_build_sha256,attempt,maximum_attempts,stage,scope_kind)
  VALUES(build,'imgbld_impact_upgrade',tenant,project,recipe,1,1,'{}',repeat('b',64),repeat('e',64),1,1,'COMPLETED','PROJECT');
 INSERT INTO control_plane.image_artifacts(id,ref,organization_id,project_id,recipe_id,recipe_version,recipe_generation,
   spec_sha256,build_id,build_version,build_attempt,specification,policy_revision,policy_sha256,
   role_runtime_contract_revision,role_runtime_contract_sha256,staging_reference,manifest_digest,
   immutable_build_sha256,provenance_sha256,scope_kind)
  VALUES(artifact,'imgart_impact_upgrade',tenant,project,recipe,1,1,repeat('b',64),build,1,1,'{}',1,repeat('c',64),
   1,repeat('d',64),'fixture.example.test/roles:upgrade','sha256:'||repeat('f',64),repeat('e',64),repeat('1',64),'PROJECT');
 INSERT INTO control_plane.managed_configuration_sets(id,ref,organization_id,project_id,kind,name,managed_by,source,created_by)
  VALUES(configuration,'mcfg_impact_upgrade',tenant,project,'ROLE_IMAGE','Synthetic configuration','UI','control-center',actor);
 INSERT INTO control_plane.managed_configuration_revisions(id,ref,organization_id,configuration_set_id,revision,state,
   content_format,content,digest,created_by,validated_at,published_at)
  VALUES(revision,'mrev_impact_upgrade',tenant,configuration,1,'PUBLISHED','JSON','{}',repeat('2',64),actor,
   clock_timestamp(),clock_timestamp());
 INSERT INTO control_plane.runtime_secrets(id,ref,organization_id,project_id,namespace,name,value_type,state,created_by,scope_kind)
  VALUES(secret,'sec_impact_upgrade',tenant,project,'kodex-runtime','Synthetic secret','STRING','ACTIVE',actor,'PROJECT');
 INSERT INTO control_plane.runtime_secret_drafts(id,ref,organization_id,secret_id,owner_actor_id,staged_namespace,state,expected_content_sha256)
  VALUES(draft,'sdft_impact_upgrade',tenant,secret,actor,'kodex-secret-drafts','PUBLISHED',repeat('3',64));
 INSERT INTO control_plane.runtime_secret_draft_operations(id,ref,organization_id,draft_id,actor_id,kind,state,
   expected_draft_version,expected_secret_version,expected_current_revision,target_revision,token_digest,
   idempotency_key,intent_digest,grant_expires_at,terminal_snapshot,correlation_ref)
  VALUES(operation,'sdop_impact_upgrade',tenant,draft,actor,'PUBLISH','COMPLETED',1,1,0,1,repeat('4',64),
   'impact-upgrade-operation',repeat('5',64),clock_timestamp()+interval '1 day',
   '{"Ref":"sdft_impact_upgrade","State":"PUBLISHED","Version":1}', 'corr_impact_upgrade');

 -- Декартова матрица PREPARED/APPLIED × пустой/непустой план для каждого вида.
 FOR position IN 1..4 LOOP
  suffix := 'upgrade_'||position;
  plan_id := gen_random_uuid();
  INSERT INTO control_plane.revision_impact_plans(id,ref,organization_id,actor_id,kind,snapshot,digest)
   VALUES(plan_id,'rvip_'||suffix,tenant,actor,'PROMPT_TEMPLATE',
    jsonb_build_object('Ref','rvip_'||suffix,'Version',1,'State','PREPARED','ConfigurationRef','mcfg_impact_upgrade'),repeat('6',64));
  IF position IN (2,4) THEN
   INSERT INTO control_plane.revision_impact_items(plan_id,ref,snapshot)
    VALUES(plan_id,'rvit_'||suffix,jsonb_build_object('Ref','rvit_'||suffix,'ConsumerRef','agt_historical_fixture'));
  END IF;
  IF position>2 THEN
   UPDATE control_plane.revision_impact_plans SET state='APPLIED',version=2,applied_at=clock_timestamp(),
    published_revision_ref='mrev_impact_upgrade' WHERE id=plan_id;
  END IF;

  plan_id := gen_random_uuid();
  INSERT INTO control_plane.role_image_impact_plans(id,ref,organization_id,actor_id,configuration_id,revision_id,artifact_id,snapshot,digest)
   VALUES(plan_id,'riip_'||suffix,tenant,actor,configuration,revision,artifact,
    jsonb_build_object('Ref','riip_'||suffix,'Version',1,'State','PREPARED','ArtifactRef','imgart_impact_upgrade'),repeat('7',64));
  IF position IN (2,4) THEN
   INSERT INTO control_plane.role_image_impact_items(plan_id,ref,snapshot)
    VALUES(plan_id,'riit_'||suffix,jsonb_build_object('Ref','riit_'||suffix,'ConsumerRef','env_historical_fixture'));
  END IF;
  IF position>2 THEN
   UPDATE control_plane.role_image_impact_plans SET state='APPLIED',version=2,applied_at=clock_timestamp() WHERE id=plan_id;
  END IF;

  plan_id := gen_random_uuid();
  INSERT INTO control_plane.runtime_secret_draft_impact_plans(id,ref,organization_id,actor_id,draft_id,draft_version,
   secret_version,source_revision,credential_revision,digest,idempotency_key,intent_digest)
   VALUES(plan_id,'sdip_'||suffix,tenant,actor,draft,1,1,0,1,repeat('8',64),'secret-'||suffix,repeat('9',64));
  IF position IN (2,4) THEN
   INSERT INTO control_plane.runtime_secret_draft_impact_items(ref,plan_id,snapshot)
    VALUES('sdit_'||suffix,plan_id,jsonb_build_object('Ref','sdit_'||suffix,'EnvironmentRef','env_historical_fixture'));
  END IF;
  IF position>2 THEN
   UPDATE control_plane.runtime_secret_draft_impact_plans SET state='APPLIED',
    operation_id=CASE WHEN position=3 THEN operation ELSE NULL END WHERE id=plan_id;
  END IF;
 END LOOP;

 INSERT INTO control_plane.idempotency_receipts(organization_id,actor_id,operation,idempotency_key,intent_digest,
  response_type,response_payload,expires_at)
 SELECT tenant,actor,kind,'receipt-'||kind||'-'||positions.ordinal,repeat('a',64),'COMMAND_RESULT',
  convert_to(jsonb_build_object('HistoricalKind',kind,'Ref',prefix||'upgrade_'||positions.ordinal,'State',
   CASE WHEN positions.ordinal>2 THEN 'APPLIED' ELSE 'PREPARED' END)::text,'UTF8'),clock_timestamp()+interval '1 day'
 FROM (VALUES('revision-impact','rvip_'),('role-image-impact','riip_'),('secret-draft-impact','sdip_')) kinds(kind,prefix)
 CROSS JOIN generate_series(1,4) positions(ordinal);
END;
$$;

-- Полные прежние строки, включая времена, snapshot bytes, результаты и receipts.
CREATE TABLE public.impact_upgrade_expected(kind text NOT NULL,identity text NOT NULL,snapshot jsonb NOT NULL,PRIMARY KEY(kind,identity));
INSERT INTO public.impact_upgrade_expected
SELECT 'revision-plan',ref,to_jsonb(p) FROM control_plane.revision_impact_plans p
UNION ALL SELECT 'image-plan',ref,to_jsonb(p) FROM control_plane.role_image_impact_plans p
UNION ALL SELECT 'secret-plan',ref,to_jsonb(p) FROM control_plane.runtime_secret_draft_impact_plans p
UNION ALL SELECT 'revision-item',ref,to_jsonb(p) FROM control_plane.revision_impact_items p
UNION ALL SELECT 'image-item',ref,to_jsonb(p) FROM control_plane.role_image_impact_items p
UNION ALL SELECT 'secret-item',ref,to_jsonb(p) FROM control_plane.runtime_secret_draft_impact_items p
UNION ALL SELECT 'receipt',operation||':'||idempotency_key,to_jsonb(p) FROM control_plane.idempotency_receipts p
UNION ALL SELECT 'draft-operation',ref,to_jsonb(p) FROM control_plane.runtime_secret_draft_operations p;
COMMIT;
