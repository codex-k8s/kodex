package integrationpackage

// CredentialResourcePermissionConfluencePageUpdateV1 identifies a mandatory per-page check,
// not a reusable WRITE permission or proof that a credential may update every page.
// CredentialResourcePermissionConfluencePageUpdateV1 обозначает обязательную проверку страницы,
// а не многоразовое WRITE-разрешение или подтверждение обновления любых страниц ключом.
const CredentialResourcePermissionConfluencePageUpdateV1 = "CONFLUENCE_SERVER_PAGE_UPDATE_V1"

const (
	// CredentialResourcePermissionGitLabIssueCreateV1 requires current token/project checks before
	// one approved issue POST, without converting metadata into a reusable global WRITE proof.
	// CredentialResourcePermissionGitLabIssueCreateV1 требует текущие token/project checks перед
	// одним одобренным issue POST, не превращая metadata в многоразовый global WRITE proof.
	CredentialResourcePermissionGitLabIssueCreateV1 = "GITLAB_PROJECT_ISSUE_CREATE_V1"
	// CredentialResourcePermissionGitLabBranchCreateV1 also rejects protected destination refs.
	// CredentialResourcePermissionGitLabBranchCreateV1 дополнительно запрещает protected destination refs.
	CredentialResourcePermissionGitLabBranchCreateV1 = "GITLAB_PROJECT_BRANCH_CREATE_V1"
	// CredentialResourcePermissionGitLabMergeRequestCreateV1 checks same-project branch visibility;
	// it grants no merge, branch protection or repository-update capability.
	// CredentialResourcePermissionGitLabMergeRequestCreateV1 проверяет ветки одного проекта;
	// merge, изменение защиты веток либо repository-update capability не выдаются.
	CredentialResourcePermissionGitLabMergeRequestCreateV1 = "GITLAB_PROJECT_MERGE_REQUEST_CREATE_V1"
	// CredentialResourcePermissionJiraIssueCreateV1 requires CREATE_ISSUES in the exact project.
	// CredentialResourcePermissionJiraIssueCreateV1 требует CREATE_ISSUES в точном проекте.
	CredentialResourcePermissionJiraIssueCreateV1 = "JIRA_SERVER_ISSUE_CREATE_V1"
	// CredentialResourcePermissionJiraCommentWriteV1 requires ADD_COMMENTS on the exact issue.
	// CredentialResourcePermissionJiraCommentWriteV1 требует ADD_COMMENTS для точной задачи.
	CredentialResourcePermissionJiraCommentWriteV1 = "JIRA_SERVER_COMMENT_WRITE_V1"
	// CredentialResourcePermissionJiraTransitionApplyV1 requires TRANSITION_ISSUES on the exact issue.
	// CredentialResourcePermissionJiraTransitionApplyV1 требует TRANSITION_ISSUES для точной задачи.
	CredentialResourcePermissionJiraTransitionApplyV1 = "JIRA_SERVER_TRANSITION_APPLY_V1"
)

// CredentialResourcePermissionContract describes one mandatory executable consumer check and its
// inputless READ prerequisites. It carries no identity, grant, key or live permission result.
// CredentialResourcePermissionContract описывает обязательную исполняемую проверку consumer и её
// inputless READ prerequisites. Identity, grant, key и результат живой проверки прав отсутствуют.
type CredentialResourcePermissionContract struct {
	Profile, Operation, ResourceKind, Risk, ApprovalPolicy string
	ReadCapabilities                                       []string
	RequiredObservationProfile                             string
}

// CredentialResourcePermissionContractFor resolves a closed definition/capability pair. Returned
// slices are fresh, so callers cannot mutate another consumer's operation registry.
// CredentialResourcePermissionContractFor разрешает закрытую пару definition/capability. Срезы
// создаются заново, поэтому caller не меняет реестр операций другого consumer.
func CredentialResourcePermissionContractFor(definitionKey, capabilityKey string) (CredentialResourcePermissionContract, bool) {
	if contract, exists := credentialObjectReadContract(definitionKey, capabilityKey); exists {
		return contract, true
	}
	if definitionKey == "gitlab" {
		profile, risk := "", "SENSITIVE"
		switch capabilityKey {
		case "gitlab.issue.create":
			profile, risk = CredentialResourcePermissionGitLabIssueCreateV1, "WRITE"
		case "gitlab.branch.create":
			profile = CredentialResourcePermissionGitLabBranchCreateV1
		case "gitlab.merge_request.create":
			profile = CredentialResourcePermissionGitLabMergeRequestCreateV1
		}
		if profile != "" {
			return CredentialResourcePermissionContract{Profile: profile, Operation: capabilityKey, ResourceKind: "GITLAB_PROJECT",
				Risk: risk, ApprovalPolicy: "HUMAN_EACH_EFFECT", ReadCapabilities: []string{"gitlab.project.metadata.read"}, RequiredObservationProfile: CredentialObservationGitLabProjectWriteScopeV1}, true
		}
	}
	if definitionKey == "confluence-server-dc" && capabilityKey == "confluence.page.update" {
		return CredentialResourcePermissionContract{Profile: CredentialResourcePermissionConfluencePageUpdateV1,
			Operation: capabilityKey, ResourceKind: "CONFLUENCE_SPACE", Risk: "WRITE", ApprovalPolicy: "HUMAN_EACH_EFFECT", ReadCapabilities: []string{"confluence.space.read", "confluence.identity.read"}}, true
	}
	if definitionKey == "jira-server-dc" {
		profile := ""
		switch capabilityKey {
		case "jira.issue.create":
			profile = CredentialResourcePermissionJiraIssueCreateV1
		case "jira.issue.comment.write":
			profile = CredentialResourcePermissionJiraCommentWriteV1
		case "jira.issue.transition.apply":
			profile = CredentialResourcePermissionJiraTransitionApplyV1
		}
		if profile != "" {
			return CredentialResourcePermissionContract{Profile: profile, Operation: capabilityKey,
				ResourceKind: "JIRA_PROJECT", Risk: "WRITE", ApprovalPolicy: "HUMAN_EACH_EFFECT", ReadCapabilities: []string{"jira.project.read", "jira.identity.read"}}, true
		}
	}
	return CredentialResourcePermissionContract{}, false
}

// CredentialResourcePermissionProfile selects only registered typed consumer checks from an
// executable package. The owner still resolves identity, grants, key pins and launch policy.
// CredentialResourcePermissionProfile выбирает только зарегистрированные проверки typed consumer
// из executable package. Identity, grants, key pins и launch policy по-прежнему разрешает owner.
func CredentialResourcePermissionProfile(definition Package, capability Capability) string {
	if definition.Metadata.Key == "gitlab" && definition.Spec.Adapter == string(AdapterGitLab) && capability.ResourceScope.Kind == "GITLAB_SCOPE" {
		contract, exists := CredentialResourcePermissionContractForProfile("gitlab", capability.Key, CredentialResourcePermissionGitLabScopedV2)
		if exists && capability.Operation == contract.Operation && capability.Risk == contract.Risk && (capability.ApprovalPolicy == contract.ApprovalPolicy || capability.ApprovalScope != nil && capability.AllowsApprovalPolicy(capability.ApprovalPolicy)) {
			return contract.Profile
		}
		return ""
	}
	contract, exists := CredentialResourcePermissionContractFor(definition.Metadata.Key, capability.Key)
	adapter := "CONFLUENCE"
	if definition.Metadata.Key == "jira-server-dc" {
		adapter = "JIRA"
	}
	if definition.Metadata.Key == "gitlab" {
		adapter = "GITLAB"
	}
	if exists && definition.Spec.Adapter == adapter && capability.Operation == contract.Operation &&
		capability.Risk == contract.Risk && (capability.ApprovalPolicy == contract.ApprovalPolicy || capability.ApprovalScope != nil && capability.AllowsApprovalPolicy(capability.ApprovalPolicy)) &&
		capability.ResourceScope.Kind == contract.ResourceKind {
		return contract.Profile
	}
	return ""
}
