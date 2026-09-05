-- name: interaction_terminal_create_approval :one
WITH gate_node AS (
    INSERT INTO control_plane.run_nodes
        (ref,organization_id,root_run_id,run_id,parent_node_id,type,state,display_name,role,next_actions)
    SELECT @node_ref, run.organization_id, run.id, run.id, root_node.id,
        'HUMAN_GATE','WAITING','i18n:INTERACTION_DELIVERY_GATE_TITLE','i18n:OWNER_GATE_NODE_ROLE',ARRAY['OPEN','RESOLVE_GATE']
    FROM control_plane.runs run
    JOIN control_plane.run_nodes root_node ON root_node.root_run_id=run.id AND root_node.parent_node_id IS NULL
    WHERE run.id=@root_run_id::uuid AND run.organization_id=@organization_id::uuid
    RETURNING id,ref
), approval AS (
    INSERT INTO control_plane.owner_gates
        (ref,organization_id,project_id,root_run_id,node_id,title,prompt,context_summary,allowed_decisions,state)
    SELECT @gate_ref,@organization_id::uuid,@project_id::uuid,@root_run_id::uuid,gate_node.id,
        'i18n:INTERACTION_DELIVERY_GATE_TITLE','i18n:INTERACTION_DELIVERY_GATE_PROMPT',
        left(connection.name || E'\n' || (connection.public_configuration->>'channel_name'),300)
            || E'\n' || @capability_name || E'\n' || left(run.title,300) || E'\n' || left(run.result_summary,4000),
        ARRAY['APPROVE','REJECT','CANCEL'],'OPEN'
    FROM gate_node,control_plane.runs run,control_plane.integration_connections connection
    WHERE run.id=@root_run_id::uuid AND connection.organization_id=run.organization_id
      AND connection.ref=@connection_ref
    RETURNING id,ref
), delivery AS (
    INSERT INTO control_plane.interaction_deliveries
        (ref,organization_id,project_id,connection_id,grant_id,root_run_id,capability_key,message_key,
         template_data,state,approval_gate_id,connection_version,definition_version,definition_digest,execution_max_attempts)
    SELECT @delivery_ref,run.organization_id,run.project_id,connection.id,@grant_id::uuid,run.id,
        @capability_key,CASE @capability_key WHEN 'mattermost.notifications' THEN 'MATTERMOST_RUN_NOTIFICATION' ELSE 'MATTERMOST_RUN_RESULT' END,
        jsonb_build_object('title',left(run.title,300),'state',run.state,'result',left(run.result_summary,4000),
            'runRef',run.ref,'artifactCount',(SELECT count(*) FROM control_plane.artifacts artifact
                JOIN control_plane.runs artifact_run ON artifact_run.id=artifact.run_id
                WHERE artifact_run.root_run_id=run.id OR artifact_run.id=run.id)),
        'WAITING_APPROVAL',approval.id,connection.version,connection.definition_version,connection.definition_digest,@max_attempts
    FROM approval,control_plane.runs run,control_plane.integration_connections connection
    WHERE run.id=@root_run_id::uuid AND run.organization_id=@organization_id::uuid
      AND connection.organization_id=run.organization_id AND connection.ref=@connection_ref
    RETURNING ref
)
SELECT approval.id::text,gate_node.ref,delivery.ref FROM approval,gate_node,delivery;
