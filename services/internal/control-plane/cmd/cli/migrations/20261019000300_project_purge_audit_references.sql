-- +goose Up
SET ROLE control_plane_owner;

-- Организационная/чужая проектная история аудита не принадлежит удаляемому
-- проекту только из-за ссылки на его assistant_agent. Сохраняем сам audit,
-- actor/action/resource и отвязываем лишь FK внутри защищённого purge context.
-- Проектный audit по-прежнему входит в точный граф удаления.
-- +goose StatementBegin
DO $migration$
DECLARE definition text; marker text; replacement text;
BEGIN
 SELECT pg_get_functiondef('control_plane.purge_project_database(uuid,uuid,text)'::regprocedure) INTO definition;
 marker:='    PERFORM control_plane.project_purge_recovery_prepare_purge(p_organization_id,p_project_id,p_objects_digest);';
 IF (length(definition)-length(replace(definition,marker,'')))/length(marker)<>1
    OR strpos(definition,'target.relation_id = $2 AND target.relation_id = parent.tableoid::oid')=0 THEN
   RAISE EXCEPTION 'project purge audit reference precondition failed'; END IF;
 replacement:=marker || E'\n' || $patch$
    UPDATE control_plane.audit_events event
    SET assistant_agent_id=NULL
    FROM control_plane.agents agent
    WHERE event.organization_id=p_organization_id AND event.assistant_agent_id=agent.id
      AND agent.organization_id=p_organization_id AND agent.project_id=p_project_id
      AND event.project_id IS DISTINCT FROM p_project_id;
$patch$;
 EXECUTE replace(definition,marker,replacement);
END $migration$;
-- +goose StatementEnd

RESET ROLE;
