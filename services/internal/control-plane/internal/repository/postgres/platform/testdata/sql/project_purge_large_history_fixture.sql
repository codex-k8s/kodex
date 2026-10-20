-- name: project_purge_large_history_fixture :exec
WITH template AS MATERIALIZED (
 SELECT event.organization_id,event.project_id,event.root_run_id FROM control_plane.run_events event
 JOIN control_plane.runs run ON run.id=event.root_run_id
 WHERE run.ref=$1 ORDER BY event.sequence LIMIT 1
)
INSERT INTO control_plane.run_events(ref,organization_id,project_id,root_run_id,aggregate_ref,
 aggregate_version,sequence,type,safe_summary,safe_delta,correlation_ref)
SELECT 'evt_purge_scale_'||number,organization_id,project_id,root_run_id,'purge_scale_fixture',
 1,1000000+number,'TURN_PROGRESS','Synthetic history','{}'::jsonb,'purge_scale_fixture'
FROM template CROSS JOIN generate_series(1,55000) number;
