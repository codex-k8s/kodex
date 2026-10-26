-- name: project_purge_authority_spoof :exec
-- Все пробы исполняются от настоящего runtime LOGIN в одноразовой БД.
-- Допущенная операция вызывает ошибку с откатом всего блока, даже на пустой таблице.
DO $probe$
DECLARE statement text;
BEGIN
    FOREACH statement IN ARRAY ARRAY[
        'INSERT INTO control_plane.project_purge_context(transaction_id,backend_pid,project_id) VALUES(pg_current_xact_id(),pg_backend_pid(),''20000000-0000-4000-8000-000000000001''::uuid)',
        'UPDATE control_plane.project_purge_context SET backend_pid=pg_backend_pid()',
        'DELETE FROM control_plane.project_purge_context',
        'TRUNCATE control_plane.project_purge_context',
        'INSERT INTO control_plane.project_purge_targets(transaction_id,relation_id,row_tid,row_key) VALUES(pg_current_xact_id(),''control_plane.run_nodes''::regclass,''(0,1)''::tid,jsonb_build_array(''20000000-0000-4000-8000-000000000001''::uuid))',
        'UPDATE control_plane.project_purge_targets SET transaction_id=pg_current_xact_id()',
        'DELETE FROM control_plane.project_purge_targets',
        'TRUNCATE control_plane.project_purge_targets'
    ] LOOP
        BEGIN
            EXECUTE statement;
            RAISE EXCEPTION 'runtime purge authority mutation unexpectedly permitted';
        EXCEPTION WHEN insufficient_privilege THEN NULL;
        END;
    END LOOP;
    IF control_plane.project_purge_authorized() THEN
        RAISE EXCEPTION 'runtime forged purge authority';
    END IF;
END $probe$;
