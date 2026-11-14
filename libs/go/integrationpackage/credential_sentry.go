package integrationpackage

const (
	// CredentialResourcePermissionSentryReadV1 requires an exact current project/object GET, never a reusable grant over all Sentry objects.
	// CredentialResourcePermissionSentryReadV1 требует текущий GET проекта/объекта, а не многоразовый допуск ко всем объектам Sentry.
	CredentialResourcePermissionSentryReadV1 = "SENTRY_PROJECT_OBJECT_READ_V1"
	// CredentialResourcePermissionSentryStatusV1 requires fresh token scopes and issue ownership before the single approved status PUT.
	// CredentialResourcePermissionSentryStatusV1 требует текущие token scopes и принадлежность ошибки перед единственным одобренным PUT статуса.
	CredentialResourcePermissionSentryStatusV1 = "SENTRY_ISSUE_STATUS_WRITE_V1"
	// CredentialObservationSentryScopesV1 records bounded auth.scopes metadata without claiming provider permission over a particular issue.
	// CredentialObservationSentryScopesV1 фиксирует ограниченные auth.scopes без утверждения о правах на конкретную ошибку.
	CredentialObservationSentryScopesV1 = "SENTRY_TOKEN_SCOPES_V1"
	// CredentialResourcePermissionSentryMutationV1 pins supported comment and assignment effects to exact current project/issue checks.
	// CredentialResourcePermissionSentryMutationV1 закрепляет поддержанные comment/assignment effects за текущими проверками проекта/ошибки.
	CredentialResourcePermissionSentryMutationV1 = "SENTRY_PROJECT_MUTATION_V1"
)

// sentryCredentialContract keeps Cloud and self-hosted operations inside the same closed typed project contract; unsupported operations receive no conditional authority.
// sentryCredentialContract сохраняет операции Cloud и self-hosted внутри закрытого типизированного контракта проекта; неизвестные операции не получают условных полномочий.
func sentryCredentialContract(definitionKey, capabilityKey string) (CredentialResourcePermissionContract, bool) {
	if definitionKey == "sentry-organization" || definitionKey == "sentry-self-hosted-organization" {
		return sentryOrganizationCredentialContract(capabilityKey)
	}
	if definitionKey != "sentry" && definitionKey != "sentry-self-hosted" {
		return CredentialResourcePermissionContract{}, false
	}
	contract := CredentialResourcePermissionContract{Operation: capabilityKey, ResourceKind: "HTTPS_RESOURCE", Risk: "READ", ApprovalPolicy: "NONE", Profile: CredentialResourcePermissionSentryReadV1, ReadCapabilities: []string{"sentry.project.read"}}
	switch capabilityKey {
	case "sentry.project.read", "sentry.issue.list", "sentry.issue.read", "sentry.event.read",
		"sentry.issue.event.list", "sentry.issue.comment.list", "sentry.release.list", "sentry.release.read", "sentry.alert.list", "sentry.alert.read":
		return contract, true
	case "sentry.issue.status.update":
		contract.Risk, contract.ApprovalPolicy, contract.Profile = "WRITE", "HUMAN_EACH_EFFECT", CredentialResourcePermissionSentryStatusV1
		contract.RequiredObservationProfile = CredentialObservationSentryScopesV1
		return contract, true
	case "sentry.issue.comment.create", "sentry.issue.assignment.update":
		contract.Risk, contract.ApprovalPolicy, contract.Profile = "WRITE", "HUMAN_EACH_EFFECT", CredentialResourcePermissionSentryMutationV1
		contract.RequiredObservationProfile = CredentialObservationSentryScopesV1
		return contract, true
	default:
		return CredentialResourcePermissionContract{}, false
	}
}

// CredentialResourcePermissionSentryOrganizationReadV1 requires an explicitly organization-wide connection and current scoped object GETs.
// CredentialResourcePermissionSentryOrganizationReadV1 требует явно organization-wide connection и текущие scoped object GET.
const CredentialResourcePermissionSentryOrganizationReadV1 = "SENTRY_ORGANIZATION_OBJECT_READ_V1"

// CredentialResourcePermissionSentryOrganizationMutationV1 excludes project connections and requires full-organization grants plus a separate EACH decision for every shared-object effect.
// CredentialResourcePermissionSentryOrganizationMutationV1 исключает project connections и требует full-organization grants и отдельное EACH решение для каждого shared-object effect.
const CredentialResourcePermissionSentryOrganizationMutationV1 = "SENTRY_ORGANIZATION_MUTATION_V1"

// sentryOrganizationCredentialContract closes the organization operation registry without upgrading existing project grants or permitting arbitrary organization administration.
// sentryOrganizationCredentialContract замыкает реестр organization operations без расширения project grants и допуска произвольного organization administration.
func sentryOrganizationCredentialContract(operation string) (CredentialResourcePermissionContract, bool) {
	contract := CredentialResourcePermissionContract{Operation: operation, ResourceKind: "HTTPS_RESOURCE", Risk: "READ", ApprovalPolicy: "NONE", Profile: CredentialResourcePermissionSentryOrganizationReadV1, ReadCapabilities: []string{"sentry.organization.read"}}
	switch operation {
	case "sentry.organization.read", "sentry.organization.project.list", "sentry.organization.release.list", "sentry.organization.release.read", "sentry.organization.alert.list", "sentry.organization.alert.read", "sentry.organization.detector.list", "sentry.organization.detector.read":
		return contract, true
	case "sentry.organization.release.create", "sentry.organization.release.update", "sentry.organization.alert.create", "sentry.organization.alert.update", "sentry.organization.alert.delete":
		contract.Risk = "WRITE"
		if operation == "sentry.organization.alert.delete" {
			contract.Risk = "DESTRUCTIVE"
		}
		contract.ApprovalPolicy = "HUMAN_EACH_EFFECT"
		contract.Profile = CredentialResourcePermissionSentryOrganizationMutationV1
		contract.RequiredObservationProfile = CredentialObservationSentryScopesV1
		return contract, true
	default:
		return CredentialResourcePermissionContract{}, false
	}
}
