-- +goose Up
SET ROLE control_plane_owner;
SET LOCAL lock_timeout = '5s';

ALTER TABLE control_plane.integration_personal_invocation_credentials
 DROP CONSTRAINT integration_personal_invocati_resource_permission_profile_check,
 ADD CONSTRAINT integration_personal_invocati_resource_permission_profile_check CHECK
 (resource_permission_profile IN ('','CONFLUENCE_SERVER_PAGE_UPDATE_V1','CONFLUENCE_SERVER_CONTENT_CREATE_V1',
 'JIRA_SERVER_ISSUE_CREATE_V1','JIRA_SERVER_COMMENT_WRITE_V1','JIRA_SERVER_TRANSITION_APPLY_V1',
 'GITLAB_PROJECT_OBJECT_READ_V1','JIRA_SERVER_PROJECT_OBJECT_READ_V1','CONFLUENCE_SERVER_SPACE_OBJECT_READ_V1',
 'GITLAB_PROJECT_ISSUE_CREATE_V1','GITLAB_PROJECT_BRANCH_CREATE_V1','GITLAB_PROJECT_MERGE_REQUEST_CREATE_V1',
 'GITLAB_SCOPED_RESOURCE_V2','SENTRY_PROJECT_OBJECT_READ_V1','SENTRY_ISSUE_STATUS_WRITE_V1','SENTRY_PROJECT_MUTATION_V1',
 'SENTRY_ORGANIZATION_OBJECT_READ_V1','SENTRY_ORGANIZATION_MUTATION_V1','OBSERVABILITY_TYPED_RESOURCE_READ_V1',
 'GRAFANA_TYPED_WRITE_V1','LOKI_RULER_MUTATION_V1','GITHUB_SCOPED_RESOURCE_V1'));

-- Расширяются закрытые списки операций Server/DC. Существующие привязки и квитанции сохраняют прежние профили.
-- +goose StatementBegin
DO $atlassian_content$
DECLARE
  source text;
  confluence_reads constant text := '(''confluence.page.search'',''confluence.page.read'')';
  jira_reads constant text := '''jira.issue.search'',''jira.issue.read'',''jira.issue.transition.list''';
  write_exclusion constant text := $exclusion$AND invocation.capability_key='confluence.page.update')$exclusion$;
  write_branch constant text := $branch$OR (NEW.resource_permission_profile='CONFLUENCE_SERVER_PAGE_UPDATE_V1'$branch$;
  create_branch constant text := $create$OR (NEW.resource_permission_profile='CONFLUENCE_SERVER_CONTENT_CREATE_V1'
              AND connection.definition_key='confluence-server-dc'
              AND invocation.capability_key IN ('confluence.page.create','confluence.page.comment.create')
              AND invocation.operation=invocation.capability_key
              AND invocation.risk='WRITE'
              AND invocation.resource_kind='CONFLUENCE_SPACE'
              AND binding.verified_capabilities @> ARRAY['confluence.space.read','confluence.identity.read']::text[]
              AND NOT binding.denied_capabilities && ARRAY['confluence.space.read','confluence.identity.read']::text[])
            $create$;
BEGIN
  SELECT pg_get_functiondef('control_plane.guard_personal_invocation_credential()'::regprocedure) INTO STRICT source;
  IF (length(source)-length(replace(source,confluence_reads,'')))/length(confluence_reads) <> 2
   OR (length(source)-length(replace(source,jira_reads,'')))/length(jira_reads) <> 2
   OR (length(source)-length(replace(source,write_exclusion,'')))/length(write_exclusion) <> 1
   OR (length(source)-length(replace(source,write_branch,'')))/length(write_branch) <> 1 THEN
    RAISE EXCEPTION 'Atlassian personal credential guard predecessor changed';
  END IF;
  source := replace(source,confluence_reads,'(''confluence.page.search'',''confluence.page.read'',''confluence.page.comment.list'')');
  source := replace(source,jira_reads,jira_reads || ',''jira.issue.comment.list'',''jira.issue.comment.read''');
  source := replace(source,write_exclusion,$expanded$AND invocation.capability_key IN ('confluence.page.update','confluence.page.create','confluence.page.comment.create'))$expanded$);
  EXECUTE replace(source,write_branch,create_branch || write_branch);
END $atlassian_content$;
-- +goose StatementEnd

RESET ROLE;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Atlassian content permission migration is forward-only'; END $$;
-- +goose StatementEnd
