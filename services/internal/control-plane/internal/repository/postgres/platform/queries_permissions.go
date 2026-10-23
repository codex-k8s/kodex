package platform

import _ "embed"

var (
	// queryPermissionsAuthorizecommandSelectMembershipsOrganizationIdProjectIdSubjectId checks an active project membership containing the requested permission for the command path; the SQL alone is not a complete authority proof.
	// queryPermissionsAuthorizecommandSelectMembershipsOrganizationIdProjectIdSubjectId проверяет активное членство в проекте с запрошенным разрешением для пути команды; сам SQL не является полным доказательством полномочий.
	//go:embed sql/permissions_authorizecommand_select_memberships_organization_id_project_id_subject_id.sql
	queryPermissionsAuthorizecommandSelectMembershipsOrganizationIdProjectIdSubjectId string
	// queryPermissionsRequireprojectpermissionSelectMembershipsOrganizationIdProjectIdSubjectId checks an active project membership containing the requested permission for the project guard, under exact organization, project and subject.
	// queryPermissionsRequireprojectpermissionSelectMembershipsOrganizationIdProjectIdSubjectId проверяет активное членство с запрошенным разрешением для защиты проекта по точным организации, проекту и субъекту.
	//go:embed sql/permissions_requireprojectpermission_select_memberships_organization_id_project_id_subject_id.sql
	queryPermissionsRequireprojectpermissionSelectMembershipsOrganizationIdProjectIdSubjectId string
	// queryListProjectPermissions lists sorted distinct permission keys from active memberships of one subject in an exact organization and project.
	// queryListProjectPermissions возвращает отсортированные уникальные ключи разрешений из активного членства субъекта в точных организации и проекте.
	//go:embed sql/permissions_list_project_permissions.sql
	queryListProjectPermissions string
	// queryPermissionsProjectidbyresourceSelectProjectsOrganizationIdRef resolves project IDs by organization and project ref for a later project-scope check without granting access.
	// queryPermissionsProjectidbyresourceSelectProjectsOrganizationIdRef разрешает ID проектов по организации и ссылке проекта для последующей проверки области без выдачи доступа.
	//go:embed sql/permissions_projectidbyresource_select_projects_organization_id_ref.sql
	queryPermissionsProjectidbyresourceSelectProjectsOrganizationIdRef string
	// queryPermissionsProjectidbyresourceSelectAgentsOrganizationIdRef resolves non-null project IDs of agents by organization and agent ref for later scope checks.
	// queryPermissionsProjectidbyresourceSelectAgentsOrganizationIdRef разрешает ненулевые ID проектов агентов по организации и ссылке агента для последующей проверки области.
	//go:embed sql/permissions_projectidbyresource_select_agents_organization_id_ref.sql
	queryPermissionsProjectidbyresourceSelectAgentsOrganizationIdRef string
	// queryPermissionsProjectidbyresourceSelectWorkflowsOrganizationIdRef resolves workflow project IDs by organization and workflow ref for later scope checks.
	// queryPermissionsProjectidbyresourceSelectWorkflowsOrganizationIdRef разрешает ID проектов процессов по организации и ссылке процесса для последующей проверки области.
	//go:embed sql/permissions_projectidbyresource_select_workflows_organization_id_ref.sql
	queryPermissionsProjectidbyresourceSelectWorkflowsOrganizationIdRef string
	// queryPermissionsProjectidbyresourceSelectSessionsOrganizationIdRef resolves non-null session project IDs by organization and session ref for later scope checks.
	// queryPermissionsProjectidbyresourceSelectSessionsOrganizationIdRef разрешает ненулевые ID проектов сессий по организации и ссылке сессии для последующей проверки области.
	//go:embed sql/permissions_projectidbyresource_select_sessions_organization_id_ref.sql
	queryPermissionsProjectidbyresourceSelectSessionsOrganizationIdRef string
	// queryPermissionsProjectidbyresourceSelectRunsOrganizationIdRef resolves non-null run project IDs by organization and run ref for later scope checks.
	// queryPermissionsProjectidbyresourceSelectRunsOrganizationIdRef разрешает ненулевые ID проектов запусков по организации и ссылке запуска для последующей проверки области.
	//go:embed sql/permissions_projectidbyresource_select_runs_organization_id_ref.sql
	queryPermissionsProjectidbyresourceSelectRunsOrganizationIdRef string
	// queryPermissionsProjectidbyresourceSelectOwnerGatesOrganizationIdRef resolves owner-gate project IDs by organization and gate ref for later scope checks.
	// queryPermissionsProjectidbyresourceSelectOwnerGatesOrganizationIdRef разрешает ID проектов шлюзов подтверждения по организации и ссылке шлюза для последующей проверки области.
	//go:embed sql/permissions_projectidbyresource_select_owner_gates_organization_id_ref.sql
	queryPermissionsProjectidbyresourceSelectOwnerGatesOrganizationIdRef string
	// queryPermissionsProjectidbyresourceSelectArtifactsOrganizationIdRef resolves artifact project IDs by organization and artifact ref for later scope checks; a nullable project result is not an authorization decision.
	// queryPermissionsProjectidbyresourceSelectArtifactsOrganizationIdRef разрешает ID проектов артефактов по организации и ссылке артефакта для последующей проверки области; проект может отсутствовать и сам результат не разрешает доступ.
	//go:embed sql/permissions_projectidbyresource_select_artifacts_organization_id_ref.sql
	queryPermissionsProjectidbyresourceSelectArtifactsOrganizationIdRef string
	// queryPermissionsProjectidbyresourceSelectSchedulesOrganizationIdRef resolves schedule project IDs by organization and schedule ref for later scope checks.
	// queryPermissionsProjectidbyresourceSelectSchedulesOrganizationIdRef разрешает ID проектов расписаний по организации и ссылке расписания для последующей проверки области.
	//go:embed sql/permissions_projectidbyresource_select_schedules_organization_id_ref.sql
	queryPermissionsProjectidbyresourceSelectSchedulesOrganizationIdRef string
	// queryPermissionsProjectidbyresourceSelectAssistantConversationsOrganizationIdRef resolves non-null assistant-conversation project IDs by organization and conversation ref for later scope checks.
	// queryPermissionsProjectidbyresourceSelectAssistantConversationsOrganizationIdRef разрешает ненулевые ID проектов диалогов помощника по организации и ссылке диалога для последующей проверки области.
	//go:embed sql/permissions_projectidbyresource_select_assistant_conversations_organization_id_ref.sql
	queryPermissionsProjectidbyresourceSelectAssistantConversationsOrganizationIdRef string
	// queryPermissionsProjectidbyresourceSelectRuntimeEnvironmentSetsOrganizationIdRef resolves a non-null runtime-environment project ID by organization and set ref for later scope checks.
	// queryPermissionsProjectidbyresourceSelectRuntimeEnvironmentSetsOrganizationIdRef разрешает ненулевой ID проекта runtime-окружения по организации и ссылке набора для последующей проверки области.
	//go:embed sql/permissions_projectidbyresource_select_runtime_environment_sets_organization_id_ref.sql
	queryPermissionsProjectidbyresourceSelectRuntimeEnvironmentSetsOrganizationIdRef string
)
