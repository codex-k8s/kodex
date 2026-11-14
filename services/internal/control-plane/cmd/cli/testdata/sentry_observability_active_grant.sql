-- Только disposable rollback transaction: expired ACTIVE grant всё ещё не CLOSED.
SET LOCAL session_replication_role=replica;
INSERT INTO control_plane.session_workspace_volume_attempts(task_id,attempt,lease_ref,fence_digest,workload_instance,input_snapshot,input_digest,grant_state,expires_at)
VALUES(gen_random_uuid(),1,'lea_sentry_expired_counterfactual',repeat('a',64),'synthetic-workload','{}',encode(sha256(convert_to('{}','UTF8')),'hex'),'ACTIVE',clock_timestamp()-interval '1 hour');
