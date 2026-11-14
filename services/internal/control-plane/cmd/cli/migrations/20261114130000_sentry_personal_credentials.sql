-- +goose Up
-- Расширение закрытого owner guard после GitHub scope; прежние данные и pins не переписываются.
SET ROLE control_plane_owner;
SET LOCAL lock_timeout='8s';
SET LOCAL statement_timeout='30s';
-- +goose StatementBegin
DO $sentry_profiles$
DECLARE
 item record;
 source text;
 marker text;
 replacement text;
 target regclass;
 column_name text;
 matches integer;
BEGIN
 FOR target,column_name,marker,replacement IN VALUES
  ('control_plane.integration_definitions'::regclass,'adapter',
   '''SSH_COMPOSE''::text', '''SSH_COMPOSE''::text, ''SENTRY''::text, ''OBSERVABILITY''::text'),
  ('control_plane.integration_personal_invocation_credentials'::regclass,'resource_permission_profile',
   '''GITLAB_SCOPED_RESOURCE_V2''::text',
   '''GITLAB_SCOPED_RESOURCE_V2''::text, ''SENTRY_PROJECT_OBJECT_READ_V1''::text, ''SENTRY_ISSUE_STATUS_WRITE_V1''::text, ''SENTRY_PROJECT_MUTATION_V1''::text, ''SENTRY_ORGANIZATION_OBJECT_READ_V1''::text, ''SENTRY_ORGANIZATION_MUTATION_V1''::text, ''OBSERVABILITY_TYPED_RESOURCE_READ_V1''::text, ''GRAFANA_TYPED_WRITE_V1''::text'),
  ('control_plane.integration_personal_credentials'::regclass,'observed_permission_profiles',
   '''GITLAB_PROJECT_WRITE_SCOPE_V1''::text',
   '''GITLAB_PROJECT_WRITE_SCOPE_V1''::text, ''SENTRY_TOKEN_SCOPES_V1''::text, ''GRAFANA_CURRENT_PERMISSIONS_V1''::text')
 LOOP
  matches:=0;
  FOR item IN SELECT constraint_row.conname,pg_get_constraintdef(constraint_row.oid) AS definition
   FROM pg_constraint constraint_row
   JOIN pg_attribute attribute ON attribute.attrelid=constraint_row.conrelid AND attribute.attname=column_name
   WHERE constraint_row.conrelid=target AND constraint_row.contype='c'
    AND constraint_row.conkey=ARRAY[attribute.attnum]::smallint[]
  LOOP
   matches:=matches+1;
   source:=item.definition;
   IF (length(source)-length(replace(source,marker,'')))/length(marker)<>1
    OR position('SENTRY_' IN source)>0 THEN RAISE EXCEPTION 'Sentry credential constraint predecessor changed'; END IF;
   EXECUTE format('ALTER TABLE %s DROP CONSTRAINT %I',target,item.conname);
   EXECUTE format('ALTER TABLE %s ADD CONSTRAINT %I %s',target,item.conname,replace(source,marker,replacement));
  END LOOP;
  IF matches<>1 THEN RAISE EXCEPTION 'Sentry credential constraint is missing or ambiguous'; END IF;
 END LOOP;
END $sentry_profiles$;
-- +goose StatementEnd
-- +goose StatementBegin
DO $sentry_guard$
DECLARE
 source text;
 marker text := $before$(NEW.resource_permission_profile='GITLAB_SCOPED_RESOURCE_V2'$before$;
 replacement text := $after$(connection.definition_key IN ('sentry','sentry-self-hosted')
             AND invocation.resource_kind='HTTPS_RESOURCE'
             AND invocation.operation=invocation.capability_key
             AND binding.verified_capabilities @> ARRAY['sentry.project.read']::text[]
             AND NOT binding.denied_capabilities && ARRAY['sentry.project.read']::text[]
             AND (
               (NEW.resource_permission_profile='SENTRY_PROJECT_OBJECT_READ_V1'
                AND invocation.risk='READ' AND invocation.approval_policy='NONE'
                AND invocation.capability_key IN ('sentry.project.read','sentry.issue.list','sentry.issue.read','sentry.event.read',
                  'sentry.issue.event.list','sentry.issue.comment.list','sentry.release.list','sentry.release.read','sentry.alert.list','sentry.alert.read'))
               OR (NEW.resource_permission_profile='SENTRY_ISSUE_STATUS_WRITE_V1'
                AND invocation.capability_key='sentry.issue.status.update'
                AND invocation.risk='WRITE' AND invocation.approval_policy IN ('HUMAN_EACH_EFFECT','HUMAN_SCOPED')
                AND binding.observed_permission_profiles @> ARRAY['SENTRY_TOKEN_SCOPES_V1']::text[])
               OR (NEW.resource_permission_profile='SENTRY_PROJECT_MUTATION_V1'
                AND invocation.capability_key IN ('sentry.issue.comment.create','sentry.issue.assignment.update')
                AND invocation.risk='WRITE' AND invocation.approval_policy IN ('HUMAN_EACH_EFFECT','HUMAN_SCOPED')
                AND binding.observed_permission_profiles @> ARRAY['SENTRY_TOKEN_SCOPES_V1']::text[])
             ))
            OR (connection.definition_key IN ('sentry-organization','sentry-self-hosted-organization')
             AND invocation.resource_kind='HTTPS_RESOURCE' AND invocation.operation=invocation.capability_key
             AND binding.verified_capabilities @> ARRAY['sentry.organization.read']::text[]
             AND NOT binding.denied_capabilities && ARRAY['sentry.organization.read']::text[]
             AND (
               (NEW.resource_permission_profile='SENTRY_ORGANIZATION_OBJECT_READ_V1'
                AND invocation.risk='READ' AND invocation.approval_policy='NONE'
                AND invocation.capability_key IN ('sentry.organization.read','sentry.organization.project.list',
                  'sentry.organization.release.list','sentry.organization.release.read','sentry.organization.alert.list',
                  'sentry.organization.alert.read','sentry.organization.detector.list','sentry.organization.detector.read'))
               OR (NEW.resource_permission_profile='SENTRY_ORGANIZATION_MUTATION_V1'
                AND invocation.approval_policy='HUMAN_EACH_EFFECT'
                AND ((invocation.risk='WRITE' AND invocation.capability_key IN ('sentry.organization.release.create',
                  'sentry.organization.release.update','sentry.organization.alert.create','sentry.organization.alert.update'))
                  OR (invocation.risk='DESTRUCTIVE' AND invocation.capability_key='sentry.organization.alert.delete'))
                AND binding.observed_permission_profiles @> ARRAY['SENTRY_TOKEN_SCOPES_V1']::text[])
             ))
            OR (connection.definition_key IN ('prometheus','thanos')
             AND NEW.resource_permission_profile='OBSERVABILITY_TYPED_RESOURCE_READ_V1'
             AND invocation.resource_kind='HTTPS_RESOURCE' AND invocation.operation=invocation.capability_key
             AND invocation.risk='READ' AND invocation.approval_policy='NONE'
             AND invocation.capability_key IN (connection.definition_key||'.connection.read',connection.definition_key||'.query.instant',
              connection.definition_key||'.query.range',connection.definition_key||'.labels.list',connection.definition_key||'.label.values',
              connection.definition_key||'.series.list',connection.definition_key||'.rules.list',connection.definition_key||'.alerts.list')
             AND binding.verified_capabilities @> ARRAY[connection.definition_key||'.connection.read']::text[]
             AND NOT binding.denied_capabilities && ARRAY[connection.definition_key||'.connection.read']::text[])
            OR (connection.definition_key IN ('grafana-dashboard','grafana-organization')
             AND invocation.resource_kind='HTTPS_RESOURCE' AND invocation.operation=invocation.capability_key
             AND binding.verified_capabilities @> ARRAY['grafana.connection.read']::text[]
             AND NOT binding.denied_capabilities && ARRAY['grafana.connection.read']::text[]
             AND (
              (NEW.resource_permission_profile='OBSERVABILITY_TYPED_RESOURCE_READ_V1'
               AND invocation.risk='READ' AND invocation.approval_policy='NONE'
               AND (invocation.capability_key IN ('grafana.connection.read','grafana.datasource.read','grafana.datasource.query',
                 'grafana.annotation.list','grafana.annotation.read')
                OR (connection.definition_key='grafana-dashboard' AND invocation.capability_key='grafana.dashboard.read')
                OR (connection.definition_key='grafana-organization' AND invocation.capability_key IN (
                 'grafana.rule.list','grafana.rule.read','grafana.silence.list','grafana.silence.read'))))
              OR (NEW.resource_permission_profile='GRAFANA_TYPED_WRITE_V1' AND connection.definition_key='grafana-organization'
               AND invocation.approval_policy='HUMAN_EACH_EFFECT'
               AND binding.observed_permission_profiles @> ARRAY['GRAFANA_CURRENT_PERMISSIONS_V1']::text[]
               AND ((invocation.risk='WRITE' AND invocation.capability_key IN ('grafana.annotation.create','grafana.annotation.update',
                 'grafana.silence.create','grafana.silence.update'))
                OR (invocation.risk='DESTRUCTIVE' AND invocation.capability_key IN ('grafana.annotation.delete','grafana.silence.delete'))))
             ))
            OR (NEW.resource_permission_profile='GITLAB_SCOPED_RESOURCE_V2'$after$;
 empty_marker text := $empty$(NEW.resource_permission_profile=''$empty$;
 empty_replacement text := $empty$(NEW.resource_permission_profile='' AND connection.definition_key NOT IN ('sentry','sentry-self-hosted','sentry-organization','sentry-self-hosted-organization','prometheus','thanos','grafana-dashboard','grafana-organization')$empty$;
BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_proc WHERE oid='control_plane.guard_personal_invocation_credential()'::regprocedure
  AND pg_get_userbyid(proowner)='control_plane_owner' AND NOT prosecdef)
 THEN RAISE EXCEPTION 'Sentry credential guard owner changed'; END IF;
 SELECT pg_get_functiondef('control_plane.guard_personal_invocation_credential()'::regprocedure) INTO source;
 IF (length(source)-length(replace(source,marker,'')))/length(marker)<>1
  OR (length(source)-length(replace(source,empty_marker,'')))/length(empty_marker)<>1
  OR position('GITHUB_SCOPED_RESOURCE_V1' IN source)=0
  OR position('SENTRY_PROJECT_OBJECT_READ_V1' IN source)>0
  OR position('integration_declared_policy_allowed(connection.id,invocation.capability_key,invocation.operation,' IN source)=0
 THEN RAISE EXCEPTION 'Sentry credential guard predecessor changed'; END IF;
 source:=replace(source,marker,replacement);
 source:=replace(source,empty_marker,empty_replacement);
 EXECUTE source;
END $sentry_guard$;
-- +goose StatementEnd
RESET ROLE;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Sentry personal credential migration is forward-only'; END $$;
-- +goose StatementEnd
