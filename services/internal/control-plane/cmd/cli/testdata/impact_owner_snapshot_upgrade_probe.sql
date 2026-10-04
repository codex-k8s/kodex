-- Положительный контроль только SQL-read boundary, не domain/RPC validation.
-- Никакая историческая строка не получает V2; создаются новые rollback-only rows.
DO $$
DECLARE
 entry record;
 source_row jsonb;
 candidate jsonb;
 marker uuid;
BEGIN
 FOR entry IN SELECT * FROM (VALUES
  ('revision_impact_plans','rvip_'),
  ('role_image_impact_plans','riip_'),
  ('runtime_secret_draft_impact_plans','sdip_')) definitions(table_name,prefix)
 LOOP
  EXECUTE format('SELECT to_jsonb(p) FROM control_plane.%I p WHERE ref=$1 AND owner_snapshot_revision=1',entry.table_name)
   INTO source_row USING entry.prefix||'upgrade_1';
  IF source_row IS NULL THEN RAISE EXCEPTION 'historical positive control source missing'; END IF;
  marker := gen_random_uuid();
  candidate := source_row||jsonb_build_object('id',marker,'ref',entry.prefix||'current_v2','owner_snapshot_revision',2,
   'idempotency_key','secret-current-v2');
  EXECUTE format('INSERT INTO control_plane.%I SELECT (jsonb_populate_record(NULL::control_plane.%I,$1)).*',entry.table_name,entry.table_name)
   USING candidate;
 END LOOP;
END;
$$;
