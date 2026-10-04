BEGIN;
SET LOCAL ROLE control_plane_owner;
DO $$
DECLARE
 entry record;
 predecessor jsonb;
 candidate jsonb;
 current_row jsonb;
 revision_value bigint;
 previous_count bigint;
 marker uuid;
 valid_count bigint;
 insert_columns text;
 select_columns text;
BEGIN
 FOR entry IN SELECT * FROM (VALUES
  ('revision_impact_plans','revision-plan'),
  ('role_image_impact_plans','image-plan'),
  ('runtime_secret_draft_impact_plans','secret-plan')) definitions(table_name,kind)
 LOOP
  EXECUTE format('SELECT count(*) FROM control_plane.%I WHERE owner_snapshot_revision=1',entry.table_name) INTO previous_count;
  IF previous_count<>4 THEN RAISE EXCEPTION 'historical matrix count mismatch'; END IF;
  IF EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='control_plane'
   AND table_name=entry.table_name AND column_name='owner_snapshot_revision' AND column_default IS NOT NULL) THEN
   RAISE EXCEPTION 'new protocol must not have an implicit revision default';
  END IF;
  FOR predecessor IN SELECT snapshot FROM public.impact_upgrade_expected WHERE kind=entry.kind LOOP
   EXECUTE format('SELECT to_jsonb(p)-''owner_snapshot_revision'' FROM control_plane.%I p WHERE id=$1::uuid',entry.table_name)
    INTO current_row USING predecessor->>'id';
   IF current_row IS DISTINCT FROM predecessor THEN RAISE EXCEPTION 'historical plan changed during upgrade'; END IF;
   BEGIN
    EXECUTE format('UPDATE control_plane.%I SET owner_snapshot_revision=2 WHERE id=$1::uuid',entry.table_name)
     USING predecessor->>'id';
    RAISE EXCEPTION 'historical revision unexpectedly mutable' USING ERRCODE='P0002';
   EXCEPTION WHEN check_violation OR raise_exception THEN
    -- Существующие immutable guards могут закрыть UPDATE раньше нового guard.
    NULL;
   END;
  END LOOP;

  EXECUTE format('SELECT to_jsonb(p) FROM control_plane.%I p WHERE state=''PREPARED'' ORDER BY ref LIMIT 1',entry.table_name)
   INTO predecessor;
  SELECT string_agg(format('%I',column_name),',' ORDER BY ordinal_position),
   string_agg(format('candidate_record.%I',column_name),',' ORDER BY ordinal_position)
   INTO insert_columns,select_columns FROM information_schema.columns
   WHERE table_schema='control_plane' AND table_name=entry.table_name AND column_name<>'owner_snapshot_revision';
  FOREACH revision_value IN ARRAY ARRAY[NULL,0,1,3]::bigint[] LOOP
   marker := gen_random_uuid();
   candidate := predecessor||jsonb_build_object('id',marker,'ref',split_part(predecessor->>'ref','_',1)||'_'||replace(marker::text,'-',''),
    'owner_snapshot_revision',revision_value,'idempotency_key',marker::text);
   BEGIN
    IF revision_value IS NULL THEN
     -- Именно отсутствие колонки в INSERT, а не подстановка NULL вместо поля.
     EXECUTE format('INSERT INTO control_plane.%I(%s) SELECT %s FROM jsonb_populate_record(NULL::control_plane.%I,$1) candidate_record',
      entry.table_name,insert_columns,select_columns,entry.table_name) USING candidate-'owner_snapshot_revision';
    ELSE
     EXECUTE format('INSERT INTO control_plane.%I SELECT (jsonb_populate_record(NULL::control_plane.%I,$1)).*',entry.table_name,entry.table_name)
      USING candidate;
    END IF;
    RAISE EXCEPTION 'invalid revision insert unexpectedly accepted' USING ERRCODE='P0002';
   EXCEPTION WHEN check_violation OR not_null_violation THEN NULL;
   END;
  END LOOP;
  marker := gen_random_uuid();
  candidate := predecessor||jsonb_build_object('id',marker,'ref',split_part(predecessor->>'ref','_',1)||'_'||replace(marker::text,'-',''),
   'owner_snapshot_revision',2,'idempotency_key',marker::text);
  EXECUTE format('INSERT INTO control_plane.%I SELECT (jsonb_populate_record(NULL::control_plane.%I,$1)).*',entry.table_name,entry.table_name)
   USING candidate;
  EXECUTE format('SELECT count(*) FROM control_plane.%I WHERE id=$1 AND owner_snapshot_revision=2',entry.table_name)
   INTO valid_count USING marker;
  IF valid_count<>1 THEN RAISE EXCEPTION 'explicit revision two insert failed'; END IF;
  BEGIN
   EXECUTE format('UPDATE control_plane.%I SET owner_snapshot_revision=1 WHERE id=$1',entry.table_name) USING marker;
   RAISE EXCEPTION 'new revision unexpectedly mutable' USING ERRCODE='P0002';
  EXCEPTION WHEN check_violation OR raise_exception THEN NULL;
  END;
 END LOOP;

 FOR entry IN SELECT * FROM (VALUES
  ('revision_impact_items','revision-item'),('role_image_impact_items','image-item'),
  ('runtime_secret_draft_impact_items','secret-item'),('runtime_secret_draft_operations','draft-operation')) definitions(table_name,kind)
 LOOP
  FOR predecessor IN SELECT snapshot FROM public.impact_upgrade_expected WHERE kind=entry.kind LOOP
   EXECUTE format('SELECT to_jsonb(p) FROM control_plane.%I p WHERE ref=$1',entry.table_name) INTO current_row USING predecessor->>'ref';
   IF current_row IS DISTINCT FROM predecessor THEN RAISE EXCEPTION 'historical item or operation changed'; END IF;
  END LOOP;
 END LOOP;
 SELECT count(*) INTO valid_count FROM public.impact_upgrade_expected expected
 JOIN control_plane.idempotency_receipts receipt ON expected.kind='receipt'
  AND expected.identity=receipt.operation||':'||receipt.idempotency_key
  AND expected.snapshot=to_jsonb(receipt);
 IF valid_count<>12 OR (SELECT count(*) FROM control_plane.idempotency_receipts)<>12 THEN
  RAISE EXCEPTION 'historical receipt bytes or cardinality changed';
 END IF;
END;
$$;
-- Новые тестовые INSERT не загрязняют историю между повторными проверками.
ROLLBACK;
