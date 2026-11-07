package integrationpackage

const (
	// CredentialResourcePermissionGitLabObjectReadV1 requires the fixed project adapter's current
	// object GET, not a stored proof that the token can read every object in that project.
	// CredentialResourcePermissionGitLabObjectReadV1 требует текущий object GET штатного project
	// adapter, а не сохранённое подтверждение чтения ключом всех объектов проекта.
	CredentialResourcePermissionGitLabObjectReadV1 = "GITLAB_PROJECT_OBJECT_READ_V1"
	// CredentialResourcePermissionJiraObjectReadV1 preserves exact project/issue scoping and checks
	// object visibility through the requested Server GET. Cloud operations are not registered.
	// CredentialResourcePermissionJiraObjectReadV1 сохраняет exact project/issue scope и проверяет
	// видимость объекта запрошенным Server GET. Cloud operations не регистрируются.
	CredentialResourcePermissionJiraObjectReadV1 = "JIRA_SERVER_PROJECT_OBJECT_READ_V1"
	// CredentialResourcePermissionConfluenceObjectReadV1 requires the fixed Server page/search
	// adapter to check the configured space and project a successful current-user GET only.
	// CredentialResourcePermissionConfluenceObjectReadV1 требует проверки configured space штатным
	// Server page/search adapter и проекции лишь успешного GET текущего пользователя.
	CredentialResourcePermissionConfluenceObjectReadV1 = "CONFLUENCE_SERVER_SPACE_OBJECT_READ_V1"
)

// credentialObjectReadContract admits only the minimal typed object READs needed by the initial
// integration verticals. Inputless metadata proofs establish key readiness, never object rights;
// consumers must still execute the scoped adapter GET. Returned slices never share mutable state.
// credentialObjectReadContract допускает лишь минимальные typed object READ начальных вертикалей.
// Inputless metadata proofs подтверждают готовность ключа, не права на объект; consumer обязан
// выполнить scoped adapter GET. Возвращаемые срезы не разделяют изменяемое состояние.
func credentialObjectReadContract(definitionKey, capabilityKey string) (CredentialResourcePermissionContract, bool) {
	contract := CredentialResourcePermissionContract{Operation: capabilityKey, Risk: "READ", ApprovalPolicy: "NONE"}
	switch definitionKey {
	case "gitlab":
		switch capabilityKey {
		case "gitlab.repository.file.read", "gitlab.issue.read", "gitlab.merge_request.read", "gitlab.pipeline.read":
			contract.Profile, contract.ResourceKind = CredentialResourcePermissionGitLabObjectReadV1, "GITLAB_PROJECT"
			contract.ReadCapabilities = []string{"gitlab.project.metadata.read"}
		}
	case "jira-server-dc":
		switch capabilityKey {
		case "jira.issue.search", "jira.issue.read", "jira.issue.transition.list":
			contract.Profile, contract.ResourceKind = CredentialResourcePermissionJiraObjectReadV1, "JIRA_PROJECT"
			contract.ReadCapabilities = []string{"jira.project.read", "jira.identity.read"}
		}
	case "confluence-server-dc":
		switch capabilityKey {
		case "confluence.page.search", "confluence.page.read":
			contract.Profile, contract.ResourceKind = CredentialResourcePermissionConfluenceObjectReadV1, "CONFLUENCE_SPACE"
			contract.ReadCapabilities = []string{"confluence.space.read", "confluence.identity.read"}
		}
	}
	return contract, contract.Profile != ""
}
