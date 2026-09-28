-- Только локальный визуальный стенд. Создаёт отдельный terminal Run без
-- исполнения, событий и разрешений; не использовать как lifecycle-фикстуру.
BEGIN;

DO $preview$
DECLARE
  source_run control_plane.runs%ROWTYPE;
  preview_run_id uuid;
  root_node_id uuid;
  parent_id uuid;
  child_id uuid;
  node_kind text;
  edge_kind text;
  item integer;
  parent_number integer;
BEGIN
  IF current_database() <> 'control_plane' THEN
    RAISE EXCEPTION 'RUN_GRAPH_PREVIEW_DATABASE_MISMATCH';
  END IF;
  SELECT * INTO source_run
  FROM control_plane.runs
  WHERE ref = 'run_VecAkZ0oJQVaXp_THjb4TLR2'
    AND title = 'Проверка отмены активного запуска 27 сентября'
    AND state = 'CANCELLED'
    AND target_type = 'AGENT'
  FOR SHARE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'RUN_GRAPH_PREVIEW_SOURCE_MISMATCH';
  END IF;
  IF EXISTS (SELECT 1 FROM control_plane.runs WHERE ref = 'run_graph_preview_20260927') THEN
    SELECT id INTO preview_run_id FROM control_plane.runs
    WHERE ref = 'run_graph_preview_20260927'
      AND state = 'CANCELLED'
      AND presentation_metadata->>'syntheticGraphPreview' = 'true'
    FOR UPDATE;
    IF preview_run_id IS NULL
      OR (SELECT count(*) FROM control_plane.run_nodes WHERE root_run_id = preview_run_id) <> 30
      OR (SELECT count(*) FROM control_plane.run_edges WHERE root_run_id = preview_run_id) NOT IN (35, 41, 43) THEN
      RAISE EXCEPTION 'RUN_GRAPH_PREVIEW_EXISTING_MISMATCH';
    END IF;
    UPDATE control_plane.run_nodes
    SET type = 'AGENT_EXECUTION',
        display_name = format('Тестовая ветка %s · ИИ-сотрудник', right(ref, 2))
    WHERE root_run_id = preview_run_id
      AND ref LIKE 'nod_graph_preview_20260927_%'
      AND type IN ('HUMAN_GATE', 'EXTERNAL_ACTION');
    RETURN;
  END IF;

  INSERT INTO control_plane.runs (
    ref, organization_id, project_id, session_id, target_type, target_ref,
    source, title, task, state, initiated_by, result_summary, finished_at,
    graph_revision, presentation_metadata
  ) VALUES (
    'run_graph_preview_20260927', source_run.organization_id,
    source_run.project_id, source_run.session_id, source_run.target_type,
    source_run.target_ref, 'CONTROL_CENTER',
    'Проверка компоновки графа · 30 узлов',
    'Синтетическая схема для визуальной проверки. Исполнение не запускалось.',
    'CANCELLED', source_run.initiated_by,
    'Синтетическая схема; результаты исполнения отсутствуют.',
    clock_timestamp(), 2, '{"syntheticGraphPreview":true}'::jsonb
  ) RETURNING id INTO preview_run_id;
  UPDATE control_plane.runs SET root_run_id = preview_run_id WHERE id = preview_run_id;

  INSERT INTO control_plane.run_nodes (
    ref, organization_id, root_run_id, run_id, type, state, display_name,
    role, next_actions, finished_at
  ) VALUES (
    'nod_graph_preview_20260927_root', source_run.organization_id,
    preview_run_id, preview_run_id, 'ROOT_PROCESS', 'CANCELLED',
    'Тестовая схема из 30 узлов', 'Визуальная проверка', ARRAY['OPEN'],
    clock_timestamp()
  ) RETURNING id INTO root_node_id;

  FOR item IN 1..29 LOOP
    node_kind := 'AGENT_EXECUTION';
    INSERT INTO control_plane.run_nodes (
      ref, organization_id, root_run_id, run_id, parent_node_id, type, state,
      display_name, role, next_actions, finished_at
    ) VALUES (
      format('nod_graph_preview_20260927_%s', lpad(item::text, 2, '0')),
      source_run.organization_id, preview_run_id, preview_run_id,
      root_node_id, node_kind, 'CANCELLED',
      format('Тестовая ветка %s · ИИ-сотрудник', lpad(item::text, 2, '0')),
      'Только проверка компоновки', ARRAY['OPEN'], clock_timestamp()
    ) RETURNING id INTO child_id;
  END LOOP;

  FOR item IN 1..29 LOOP
    SELECT id INTO child_id FROM control_plane.run_nodes
    WHERE ref = format('nod_graph_preview_20260927_%s', lpad(item::text, 2, '0'));
    parent_number := CASE WHEN item <= 3 THEN 0 ELSE ((item - 4) / 3) + 1 END;
    IF parent_number = 0 THEN
      parent_id := root_node_id;
    ELSE
      SELECT id INTO parent_id FROM control_plane.run_nodes
      WHERE ref = format('nod_graph_preview_20260927_%s', lpad(parent_number::text, 2, '0'));
    END IF;
    UPDATE control_plane.run_nodes SET parent_node_id = parent_id WHERE id = child_id;
    edge_kind := 'DELEGATED_TO';
    INSERT INTO control_plane.run_edges (
      ref, organization_id, root_run_id, source_node_id, target_node_id, type
    ) VALUES (
      format('edg_graph_preview_20260927_tree_%s', lpad(item::text, 2, '0')),
      source_run.organization_id, preview_run_id, parent_id, child_id, edge_kind
    );
  END LOOP;

  INSERT INTO control_plane.run_edges (
    ref, organization_id, root_run_id, source_node_id, target_node_id, type
  )
  SELECT format('edg_graph_preview_20260927_cross_%s', crossing.number),
         source_run.organization_id, preview_run_id, source_node.id, target_node.id,
         crossing.edge_kind
  FROM (VALUES
    (1, 3, 14, 'CONTINUES'),
    (2, 5, 18, 'WAITING_FOR'),
    (3, 8, 23, 'DELEGATED_TO'),
    (4, 10, 29, 'CONTINUES'),
    (5, 26, 7, 'CALLBACK_TO'),
    (6, 28, 5, 'CALLBACK_TO')
  ) AS crossing(number, source_number, target_number, edge_kind)
  JOIN control_plane.run_nodes source_node
    ON source_node.ref = format('nod_graph_preview_20260927_%s', lpad(crossing.source_number::text, 2, '0'))
  JOIN control_plane.run_nodes target_node
    ON target_node.ref = format('nod_graph_preview_20260927_%s', lpad(crossing.target_number::text, 2, '0'));

  IF (SELECT count(*) FROM control_plane.run_nodes WHERE root_run_id = preview_run_id) <> 30
    OR (SELECT count(*) FROM control_plane.run_edges WHERE root_run_id = preview_run_id) <> 35 THEN
    RAISE EXCEPTION 'RUN_GRAPH_PREVIEW_CARDINALITY_MISMATCH';
  END IF;
END;
$preview$;

COMMIT;
