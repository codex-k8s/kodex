package integrationpackage

import "errors"

// Закрытые значения package-контракта. Строковое представление сохраняется
// намеренно: оно является wire-форматом YAML/JSON и не содержит секретов.
type AdapterKey string
type FieldType string
type FieldFormat string
type Risk string
type ApprovalPolicy string
type ResourceKind string
type IdempotencyMode string
type AdapterOwner string
type ExecutionRoute string
type AdapterReadiness string

const (
	AdapterSyntheticHTTP AdapterKey = "SYNTHETIC_HTTP"
	AdapterGitHub        AdapterKey = "GITHUB"
	AdapterGitLab        AdapterKey = "GITLAB"
	AdapterJira          AdapterKey = "JIRA"
	AdapterConfluence    AdapterKey = "CONFLUENCE"
	AdapterEmailHTTPS    AdapterKey = "EMAIL_HTTPS"
	AdapterMattermost    AdapterKey = "MATTERMOST_INTERACTION"

	OwnerIntegrationGateway AdapterOwner = "integration-gateway"
	OwnerInteractionGateway AdapterOwner = "interaction-gateway"

	RouteManagedMCP  ExecutionRoute = "MANAGED_MCP"
	RouteInteraction ExecutionRoute = "INTERACTION"

	ReadinessReady    AdapterReadiness = "READY"
	ReadinessNotReady AdapterReadiness = "NOT_READY"

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

type AdapterDescriptor struct {
	Owner     AdapterOwner
	Route     ExecutionRoute
	Readiness AdapterReadiness
}

var adapterRegistry = map[AdapterKey]AdapterDescriptor{
	AdapterSyntheticHTTP: {Owner: OwnerIntegrationGateway, Route: RouteManagedMCP, Readiness: ReadinessReady},
	AdapterGitHub:        {Owner: OwnerIntegrationGateway, Route: RouteManagedMCP, Readiness: ReadinessReady},
	AdapterGitLab:        {Owner: OwnerIntegrationGateway, Route: RouteManagedMCP, Readiness: ReadinessReady},
	AdapterJira:          {Owner: OwnerIntegrationGateway, Route: RouteManagedMCP, Readiness: ReadinessReady},
	AdapterConfluence:    {Owner: OwnerIntegrationGateway, Route: RouteManagedMCP, Readiness: ReadinessReady},
	AdapterEmailHTTPS:    {Owner: OwnerIntegrationGateway, Route: RouteManagedMCP, Readiness: ReadinessReady},
	AdapterMattermost:    {Owner: OwnerInteractionGateway, Route: RouteInteraction, Readiness: ReadinessNotReady},
}

func Adapter(key string) (AdapterDescriptor, bool) {
	descriptor, ok := adapterRegistry[AdapterKey(key)]
	return descriptor, ok
}

func ValidateAdapterBinding(definition Package) error {
	descriptor, ok := Adapter(definition.Spec.Adapter)
	if !ok || string(descriptor.Owner) != definition.Spec.AdapterOwner ||
		string(descriptor.Route) != definition.Spec.ExecutionRoute ||
		string(descriptor.Readiness) != definition.Spec.Readiness {
		return errors.New("integration adapter binding is invalid")
	}
	return nil
}

func (definition Package) ExecutableBy(owner AdapterOwner, route ExecutionRoute) bool {
	descriptor, ok := Adapter(definition.Spec.Adapter)
	return ok && descriptor.Owner == owner && descriptor.Route == route &&
		descriptor.Readiness == ReadinessReady && ValidateAdapterBinding(definition) == nil
}

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
	_, ok := adapterRegistry[AdapterKey(value)]
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
