-- Только локальная визуальная схема. Меняет связи ровно одного синтетического
-- terminal Run, не затрагивает исполнения, события и реальные запуски.
BEGIN;

DO $preview$
DECLARE
  preview_run_id uuid;
  edge_count integer;
BEGIN
  IF current_database() <> 'control_plane' THEN
    RAISE EXCEPTION 'RUN_GRAPH_PREVIEW_DATABASE_MISMATCH';
  END IF;
  SELECT id INTO preview_run_id
  FROM control_plane.runs
  WHERE ref = 'run_graph_preview_20260927'
    AND title = 'Проверка компоновки графа · 30 узлов'
    AND state = 'CANCELLED'
    AND presentation_metadata->>'syntheticGraphPreview' = 'true'
  FOR UPDATE;
  IF preview_run_id IS NULL
    OR (SELECT count(*) FROM control_plane.run_nodes WHERE root_run_id = preview_run_id) <> 30 THEN
    RAISE EXCEPTION 'RUN_GRAPH_PREVIEW_NODE_MISMATCH';
  END IF;
  SELECT count(*) INTO edge_count FROM control_plane.run_edges
  WHERE root_run_id = preview_run_id;
  IF edge_count NOT IN (35, 41, 43) THEN
    RAISE EXCEPTION 'RUN_GRAPH_PREVIEW_EDGE_MISMATCH';
  END IF;
  IF EXISTS (
    SELECT 1 FROM control_plane.run_edges
    WHERE root_run_id = preview_run_id
      AND ref NOT LIKE 'edg_graph_preview_20260927_%'
  ) THEN
    RAISE EXCEPTION 'RUN_GRAPH_PREVIEW_FOREIGN_EDGE';
  END IF;

  DELETE FROM control_plane.run_edges WHERE root_run_id = preview_run_id;
  WITH connections(source_number, target_number, edge_kind) AS (
    VALUES
      (0, 1, 'DELEGATED_TO'), (0, 2, 'DELEGATED_TO'),
      (2, 3, 'DELEGATED_TO'), (2, 4, 'DELEGATED_TO'), (2, 5, 'DELEGATED_TO'),
      (1, 6, 'CONTINUES'), (3, 6, 'CONTINUES'), (4, 6, 'CONTINUES'),
      (4, 7, 'CONTINUES'), (5, 7, 'CONTINUES'),
      (6, 8, 'CONTINUES'), (7, 8, 'CONTINUES'),
      (8, 9, 'DELEGATED_TO'), (8, 10, 'DELEGATED_TO'), (8, 11, 'DELEGATED_TO'),
      (9, 12, 'CONTINUES'), (10, 12, 'CONTINUES'), (11, 12, 'CONTINUES'),
      (12, 13, 'DELEGATED_TO'), (12, 14, 'DELEGATED_TO'),
      (14, 15, 'DELEGATED_TO'), (14, 16, 'DELEGATED_TO'), (14, 17, 'DELEGATED_TO'),
      (13, 18, 'CONTINUES'), (15, 18, 'CONTINUES'), (16, 18, 'CONTINUES'),
      (16, 19, 'CONTINUES'), (17, 19, 'CONTINUES'),
      (18, 20, 'CONTINUES'), (19, 20, 'CONTINUES'),
      (20, 21, 'DELEGATED_TO'), (20, 22, 'DELEGATED_TO'), (20, 23, 'DELEGATED_TO'),
      (21, 24, 'CONTINUES'), (22, 24, 'CONTINUES'), (23, 24, 'CONTINUES'),
      (24, 25, 'DELEGATED_TO'), (24, 26, 'DELEGATED_TO'),
      (26, 27, 'DELEGATED_TO'), (26, 28, 'DELEGATED_TO'),
      (25, 29, 'CONTINUES'), (27, 29, 'CONTINUES'), (28, 29, 'CONTINUES')
  ), pairs AS (
    SELECT connection.*,
      CASE WHEN source_number = 0 THEN 'nod_graph_preview_20260927_root'
        ELSE format('nod_graph_preview_20260927_%s', lpad(source_number::text, 2, '0'))
      END AS source_ref,
      format('nod_graph_preview_20260927_%s', lpad(target_number::text, 2, '0')) AS target_ref
    FROM connections connection
  )
  INSERT INTO control_plane.run_edges (
    ref, organization_id, root_run_id, source_node_id, target_node_id, type
  )
  SELECT format('edg_graph_preview_20260927_%s_%s',
           lpad(pair.source_number::text, 2, '0'),
           lpad(pair.target_number::text, 2, '0')),
         run.organization_id, preview_run_id, source_node.id, target_node.id,
         pair.edge_kind
  FROM pairs pair
  JOIN control_plane.runs run ON run.id = preview_run_id
  JOIN control_plane.run_nodes source_node
    ON source_node.ref = pair.source_ref AND source_node.root_run_id = preview_run_id
  JOIN control_plane.run_nodes target_node
    ON target_node.ref = pair.target_ref AND target_node.root_run_id = preview_run_id;

  WITH primary_parent AS (
    SELECT DISTINCT ON (target_node_id) target_node_id, source_node_id
    FROM control_plane.run_edges
    WHERE root_run_id = preview_run_id
    ORDER BY target_node_id, source_node_id
  )
  UPDATE control_plane.run_nodes child
  SET parent_node_id = parent.source_node_id
  FROM primary_parent parent
  WHERE child.id = parent.target_node_id AND child.root_run_id = preview_run_id;

  UPDATE control_plane.runs
  SET graph_revision = graph_revision + 1, updated_at = clock_timestamp()
  WHERE id = preview_run_id;
  IF (SELECT count(*) FROM control_plane.run_edges WHERE root_run_id = preview_run_id) <> 43 THEN
    RAISE EXCEPTION 'RUN_GRAPH_PREVIEW_TOPOLOGY_MISMATCH';
  END IF;
END;
$preview$;

COMMIT;
