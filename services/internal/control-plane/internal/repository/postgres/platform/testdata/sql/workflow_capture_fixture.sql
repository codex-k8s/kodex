-- name: workflow_capture_fixture :one
-- Только isolatedAssistantComponentDSN: подготавливает вход capture boundary до claim.
-- Это не проверка scheduler: обычный owner run получает новый planned anchor
-- опубликованной версии, а исходный неисполненный узел исключается из очереди.
WITH pinned AS (
    UPDATE control_plane.runs root SET workflow_version_id=version.id
    FROM control_plane.workflow_versions version JOIN control_plane.workflows workflow ON workflow.id=version.workflow_id
    WHERE root.ref=@run_ref AND workflow.ref=@workflow_ref AND workflow.state='PUBLISHED'
        AND version.version_number=workflow.published_version
        AND workflow.organization_id=root.organization_id AND workflow.project_id=root.project_id
        AND root.execution_started_at IS NULL
        AND NOT EXISTS (SELECT 1 FROM control_plane.runtime_revisions revision WHERE revision.root_run_id=root.id)
    RETURNING root.id,root.organization_id
), replaced AS (
    UPDATE control_plane.run_nodes node SET state='SKIPPED'
    FROM pinned WHERE node.run_id=pinned.id AND node.organization_id=pinned.organization_id
        AND node.type='AGENT_EXECUTION' AND node.state='QUEUED' AND node.workflow_step_key=''
    RETURNING node.id,node.organization_id,node.root_run_id,node.run_id,node.parent_node_id,node.agent_id
)
INSERT INTO control_plane.run_nodes
    (ref,organization_id,root_run_id,run_id,parent_node_id,agent_id,type,state,display_name,workflow_step_key,materialization_state)
SELECT @node_ref,organization_id,root_run_id,run_id,parent_node_id,agent_id,
       'AGENT_EXECUTION','PLANNED','Capture boundary','capture','PLANNED' FROM replaced
RETURNING id::text;
