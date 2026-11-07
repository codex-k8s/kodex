-- Generated: generate-jira-create-fields-proof.py; exact034 function-only forward change.
-- +goose Up
SET ROLE control_plane_owner;
-- +goose StatementBegin
DO $jira_compatibility$
DECLARE
 v_signature regprocedure := 'control_plane.integration_grant_admission(uuid,uuid,uuid,text,text,text,text,text,text,text[])'::regprocedure;
 v_definition text;
 v_before text := $jira_before$                ELSE (
                    SELECT revision.content::jsonb #> '{spec,capabilities}'$jira_before$;
 v_after text := $jira_after$                WHEN connection.definition_key='jira-server-dc'
                  AND (definition.definition_version,definition.digest,connection.definition_version,connection.definition_digest)
                    = ('1.1.0','fc35495f83ec21d3698c1e7cfa37eec1075ad35dd30b9dd7f9f680acf989cb31','1.0.0','838878a5a984dafaa8d1c46563e3e660fc3b074515e25376e53ae0cae204fec8')
                THEN definition.capabilities
                ELSE (
                    SELECT revision.content::jsonb #> '{spec,capabilities}'$jira_after$;
BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_proc WHERE oid=v_signature AND md5(prosrc)='bd8c8df25fa65fe6922750a3a5c260fd'
   AND pg_get_userbyid(proowner)='control_plane_owner' AND NOT prosecdef)
 THEN RAISE EXCEPTION 'Jira create fields admission predecessor changed'; END IF;
 v_definition := pg_get_functiondef(v_signature);
 IF (length(v_definition)-length(replace(v_definition,v_before,'')))/length(v_before)<>1
 THEN RAISE EXCEPTION 'Jira create fields admission patch is not unique'; END IF;
 EXECUTE replace(v_definition,v_before,v_after);
END $jira_compatibility$;
-- +goose StatementEnd
RESET ROLE;
