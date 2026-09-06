INSERT INTO control_plane.run_nodes
 (ref,organization_id,root_run_id,run_id,parent_node_id,type,state,display_name)
SELECT 'node_claim_isolation_' || fixture.state || '_' || run.ref,run.organization_id,run.root_run_id,run.id,node.id,
       'EXTERNAL_ACTION',fixture.state,'Claim isolation sibling'
FROM control_plane.runs run
JOIN control_plane.run_nodes node ON node.run_id=run.id AND node.type='ROOT_PROCESS'
CROSS JOIN (VALUES ('PLANNED'),('RUNNING')) fixture(state)
WHERE run.ref=$1;
