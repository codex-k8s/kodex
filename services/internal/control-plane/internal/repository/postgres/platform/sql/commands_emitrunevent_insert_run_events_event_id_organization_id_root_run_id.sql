-- name: commands_emitrunevent_insert_run_events_event_id_organization_id_root_run_id :exec
INSERT INTO control_plane.run_events(
    event_id,ref,organization_id,project_id,root_run_id,aggregate_ref,aggregate_version,
    sequence,type,node_ref,edge_ref,gate_ref,artifact_ref,safe_summary,run_state,node_state,
    safe_delta,correlation_ref,occurred_at,actor_kind,actor_ref,actor_name,message_kind,tool_call
) VALUES(
    $1,$2,$3::uuid,$4::uuid,$5::uuid,$6,$7,$8,$9,NULLIF($10,''),NULLIF($11,''),
    NULLIF($12,''),NULLIF($13,''),$14,NULLIF($15,''),NULLIF($16,''),$17::jsonb,$18,$19,
    $20,$21,$22,$23,$24::jsonb
)
