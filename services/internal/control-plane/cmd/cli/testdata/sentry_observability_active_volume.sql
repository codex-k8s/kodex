-- Только disposable rollback transaction: неизвестная READY task не считается joined.
SET LOCAL session_replication_role=replica;
INSERT INTO control_plane.session_workspace_volume_tasks(ref,volume_id,volume_generation,origin_node_id,root_run_id,origin_actor_id,kind,state,frozen_input)
VALUES('svt_sentry_active_counterfactual',gen_random_uuid(),1,gen_random_uuid(),gen_random_uuid(),gen_random_uuid(),'CREATE_WORKSPACE_VOLUME','READY','{}');
