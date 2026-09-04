package integrationpackage

// Закрытые значения package-контракта. Строковое представление сохраняется
// намеренно: оно является wire-форматом YAML/JSON и не содержит секретов.
type AdapterKey string
type FieldType string
type FieldFormat string
type Risk string
type ApprovalPolicy string
type ResourceKind string
type IdempotencyMode string

const (
	AdapterSyntheticHTTP AdapterKey = "SYNTHETIC_HTTP"
	AdapterGitHub        AdapterKey = "GITHUB"
	AdapterGitLab        AdapterKey = "GITLAB"
	AdapterJira          AdapterKey = "JIRA"
	AdapterConfluence    AdapterKey = "CONFLUENCE"
	AdapterEmailHTTPS    AdapterKey = "EMAIL_HTTPS"
	AdapterMattermost    AdapterKey = "MATTERMOST_INTERACTION"

	FieldString  FieldType = "STRING"
	FieldInteger FieldType = "INTEGER"
	FieldBoolean FieldType = "BOOLEAN"

	RiskRead        Risk = "READ"
	RiskWrite       Risk = "WRITE"
	RiskSensitive   Risk = "SENSITIVE"
	RiskDestructive Risk = "DESTRUCTIVE"

	ApprovalNone            ApprovalPolicy = "NONE"
	ApprovalHumanEachEffect ApprovalPolicy = "HUMAN_EACH_EFFECT"

	IdempotencyReadOnly       IdempotencyMode = "READ_ONLY"
	IdempotencyEffectKey      IdempotencyMode = "EFFECT_KEY"
	IdempotencyProviderNative IdempotencyMode = "PROVIDER_NATIVE"
)

var (
	fieldFormats = map[FieldFormat]struct{}{
		"": {}, "PLAIN": {}, "HTTPS_ORIGIN": {}, "HTTPS_URL": {},
		"EMAIL": {}, "HOST": {}, "IDENTIFIER": {},
	}
	resourceKinds = map[ResourceKind]struct{}{
		"SYNTHETIC_JOURNAL": {}, "GITHUB_REPOSITORY": {}, "GITLAB_PROJECT": {},
		"JIRA_PROJECT": {}, "CONFLUENCE_SPACE": {}, "EMAIL_SENDER": {},
		"MATTERMOST_CHANNEL": {},
	}
)

func validAdapter(value string) bool {
	_, ok := map[AdapterKey]struct{}{AdapterSyntheticHTTP: {}, AdapterGitHub: {}, AdapterGitLab: {}, AdapterJira: {}, AdapterConfluence: {}, AdapterEmailHTTPS: {}, AdapterMattermost: {}}[AdapterKey(value)]
	return ok
}

func validFieldType(value string) bool {
	_, ok := map[FieldType]struct{}{FieldString: {}, FieldInteger: {}, FieldBoolean: {}}[FieldType(value)]
	return ok
}

func validFieldFormat(value string) bool { _, ok := fieldFormats[FieldFormat(value)]; return ok }
func validRisk(value string) bool {
	return value == string(RiskRead) || value == string(RiskWrite) || value == string(RiskSensitive) || value == string(RiskDestructive)
}
func validApprovalPolicy(value string) bool {
	return value == string(ApprovalNone) || value == string(ApprovalHumanEachEffect)
}
func validResourceKind(value string) bool { _, ok := resourceKinds[ResourceKind(value)]; return ok }
func validIdempotency(value string) bool {
	return value == string(IdempotencyReadOnly) || value == string(IdempotencyEffectKey) || value == string(IdempotencyProviderNative)
}
