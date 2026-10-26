package integrationpackage

import "errors"

// AdapterKey selects one registered executable adapter without allowing a caller-defined implementation.
// AdapterKey выбирает зарегистрированный исполняемый адаптер без реализации, заданной вызывающим кодом.
// Закрытые значения package-контракта. Строковое представление сохраняется
// намеренно: оно является wire-форматом YAML/JSON и не содержит секретов.
type AdapterKey string

// FieldType limits package fields to scalar values understood by the adapter boundary.
// FieldType ограничивает поля пакета скалярными значениями, понятными границе адаптера.
type FieldType string

// FieldFormat narrows validation of a scalar field without changing its wire type.
// FieldFormat сужает проверку скалярного поля без изменения его wire-типа.
type FieldFormat string

// Risk classifies an operation before its approval and execution rules are selected.
// Risk классифицирует операцию до выбора правил подтверждения и исполнения.
type Risk string

// ApprovalPolicy names the human gate required for an effectful capability.
// ApprovalPolicy задаёт человеческий шлюз для возможности с внешним эффектом.
type ApprovalPolicy string

// ResourceKind confines capability scope to a registered external resource shape.
// ResourceKind ограничивает область возможности зарегистрированной формой внешнего ресурса.
type ResourceKind string

// IdempotencyMode records the exact retry guarantee offered by an adapter operation.
// IdempotencyMode хранит точную гарантию повтора операции адаптера.
type IdempotencyMode string

// AdapterOwner identifies the deployable allowed to execute a registered adapter.
// AdapterOwner определяет компонент, которому разрешено исполнять зарегистрированный адаптер.
type AdapterOwner string

// ExecutionRoute distinguishes managed MCP commands from external interaction events.
// ExecutionRoute отличает управляемые MCP-команды от внешних событий взаимодействия.
type ExecutionRoute string

// AdapterReadiness records whether a registered adapter is eligible for execution.
// AdapterReadiness указывает, допущен ли зарегистрированный адаптер к исполнению.
type AdapterReadiness string

const (
	// AdapterSyntheticHTTP selects the bounded synthetic HTTP adapter used by controlled scenarios.
	// AdapterSyntheticHTTP выбирает ограниченный синтетический HTTP-адаптер для контролируемых сценариев.
	AdapterSyntheticHTTP AdapterKey = "SYNTHETIC_HTTP"
	// AdapterGitHub selects the registered GitHub adapter rather than an arbitrary HTTP command.
	// AdapterGitHub выбирает зарегистрированный адаптер GitHub, а не произвольную HTTP-команду.
	AdapterGitHub AdapterKey = "GITHUB"
	// AdapterGitLab selects the registered GitLab adapter with its own capability contract.
	// AdapterGitLab выбирает зарегистрированный адаптер GitLab с отдельным контрактом возможностей.
	AdapterGitLab AdapterKey = "GITLAB"
	// AdapterJira selects the Jira adapter and its approved resource operations.
	// AdapterJira выбирает адаптер Jira и допущенные операции над ресурсами.
	AdapterJira AdapterKey = "JIRA"
	// AdapterConfluence selects the Confluence adapter and its bounded content operations.
	// AdapterConfluence выбирает адаптер Confluence и ограниченные операции с содержимым.
	AdapterConfluence AdapterKey = "CONFLUENCE"
	// AdapterEmailHTTPS selects the managed email route rather than direct mailbox credentials in an agent.
	// AdapterEmailHTTPS выбирает управляемый почтовый маршрут вместо прямых реквизитов ящика у агента.
	AdapterEmailHTTPS AdapterKey = "EMAIL_HTTPS"
	// AdapterHTTPSJSONRead selects the read-only HTTPS JSON adapter.
	// AdapterHTTPSJSONRead выбирает HTTPS JSON-адаптер только для чтения.
	AdapterHTTPSJSONRead AdapterKey = "HTTPS_JSON_READ"
	// AdapterOpenAPIMCP selects typed operations imported from a separately reviewed OpenAPI draft.
	// AdapterOpenAPIMCP выбирает типизированные операции из отдельно проверенного черновика OpenAPI.
	AdapterOpenAPIMCP AdapterKey = "OPENAPI_MCP"
	// AdapterMattermost selects the interaction route that handles external events and decisions.
	// AdapterMattermost выбирает маршрут взаимодействия для внешних событий и решений.
	AdapterMattermost AdapterKey = "MATTERMOST_INTERACTION"
	// AdapterContext7 selects the registered documentation lookup adapter.
	// AdapterContext7 выбирает зарегистрированный адаптер поиска документации.
	AdapterContext7 AdapterKey = "CONTEXT7"
	// AdapterSSHCompose selects only owner-signed bounded Compose release operations.
	// AdapterSSHCompose выбирает только ограниченные Compose-операции с owner permit.
	AdapterSSHCompose AdapterKey = "SSH_COMPOSE"

	// OwnerIntegrationGateway names the deployable that owns managed integration commands.
	// OwnerIntegrationGateway обозначает компонент-владелец управляемых команд интеграций.
	OwnerIntegrationGateway AdapterOwner = "integration-gateway"
	// OwnerInteractionGateway names the deployable that owns external interaction handling.
	// OwnerInteractionGateway обозначает компонент-владелец обработки внешних взаимодействий.
	OwnerInteractionGateway AdapterOwner = "interaction-gateway"

	// RouteManagedMCP permits only the registered managed tool invocation path.
	// RouteManagedMCP допускает только зарегистрированный путь управляемого вызова инструмента.
	RouteManagedMCP ExecutionRoute = "MANAGED_MCP"
	// RouteInteraction keeps inbound events separate from agent-callable commands.
	// RouteInteraction отделяет входящие события от команд, доступных агенту.
	RouteInteraction ExecutionRoute = "INTERACTION"

	// ReadinessReady marks a descriptor executable when its owner and route also match.
	// ReadinessReady отмечает исполняемый дескриптор при совпадении владельца и маршрута.
	ReadinessReady AdapterReadiness = "READY"
	// ReadinessNotReady keeps a registered descriptor unavailable for execution.
	// ReadinessNotReady оставляет зарегистрированный дескриптор недоступным для исполнения.
	ReadinessNotReady AdapterReadiness = "NOT_READY"

	// FieldString admits textual configuration and input values subject to field constraints.
	// FieldString допускает текстовые значения конфигурации и ввода с ограничениями поля.
	FieldString FieldType = "STRING"
	// FieldInteger admits whole-number values without coercing them from arbitrary strings.
	// FieldInteger допускает целочисленные значения без приведения произвольных строк.
	FieldInteger FieldType = "INTEGER"
	// FieldBoolean admits explicit true or false values in typed package fields.
	// FieldBoolean допускает явные true или false в типизированных полях пакета.
	FieldBoolean FieldType = "BOOLEAN"

	// RiskRead identifies a capability that must not create an external write effect.
	// RiskRead обозначает возможность без внешнего эффекта записи.
	RiskRead Risk = "READ"
	// RiskWrite identifies an effectful operation requiring its configured human gate.
	// RiskWrite обозначает операцию с эффектом, требующую назначенного человеческого шлюза.
	RiskWrite Risk = "WRITE"
	// RiskSensitive identifies an operation with heightened disclosure or effect risk.
	// RiskSensitive обозначает операцию с повышенным риском раскрытия или эффекта.
	RiskSensitive Risk = "SENSITIVE"
	// RiskDestructive identifies an operation that can irreversibly change external state.
	// RiskDestructive обозначает операцию, способную необратимо изменить внешнее состояние.
	RiskDestructive Risk = "DESTRUCTIVE"

	// ApprovalNone permits only capabilities whose policy requires no owner decision.
	// ApprovalNone допускает только возможности, для которых решение владельца не требуется.
	ApprovalNone ApprovalPolicy = "NONE"
	// ApprovalHumanEachEffect requires a separate human decision for each effectful call.
	// ApprovalHumanEachEffect требует отдельного решения человека для каждого вызова с эффектом.
	ApprovalHumanEachEffect ApprovalPolicy = "HUMAN_EACH_EFFECT"
	// ApprovalHumanScoped limits a human decision to an explicitly bound effect scope.
	// ApprovalHumanScoped ограничивает решение человека явно привязанной областью эффекта.
	ApprovalHumanScoped ApprovalPolicy = "HUMAN_SCOPED"

	// IdempotencyReadOnly allows retries only for an operation with no write effect.
	// IdempotencyReadOnly разрешает повторы лишь операции без эффекта записи.
	IdempotencyReadOnly IdempotencyMode = "READ_ONLY"
	// IdempotencyEffectKey binds retries of a write to one stable external effect key.
	// IdempotencyEffectKey привязывает повторы записи к одному устойчивому ключу внешнего эффекта.
	IdempotencyEffectKey IdempotencyMode = "EFFECT_KEY"
	// IdempotencyProviderNative relies on an explicitly supported provider-native replay guard.
	// IdempotencyProviderNative опирается на явно поддержанный провайдером механизм защиты от повтора.
	IdempotencyProviderNative IdempotencyMode = "PROVIDER_NATIVE"
	// IdempotencyOneAttempt disallows automatic retry when the external effect is uncertain.
	// IdempotencyOneAttempt запрещает автоматический повтор при неопределённом внешнем эффекте.
	IdempotencyOneAttempt IdempotencyMode = "ONE_ATTEMPT"
)

// AdapterDescriptor pins the responsible deployable, execution route and readiness for one adapter.
// AdapterDescriptor закрепляет ответственный компонент, маршрут исполнения и готовность одного адаптера.
type AdapterDescriptor struct {
	Owner     AdapterOwner
	Route     ExecutionRoute
	Readiness AdapterReadiness
}

// adapterRegistry is the closed mapping of adapter keys to server-owned execution boundaries.
// adapterRegistry — закрытое соответствие ключей адаптеров серверным границам исполнения.
var adapterRegistry = map[AdapterKey]AdapterDescriptor{
	AdapterSyntheticHTTP: {Owner: OwnerIntegrationGateway, Route: RouteManagedMCP, Readiness: ReadinessReady},
	AdapterGitHub:        {Owner: OwnerIntegrationGateway, Route: RouteManagedMCP, Readiness: ReadinessReady},
	AdapterGitLab:        {Owner: OwnerIntegrationGateway, Route: RouteManagedMCP, Readiness: ReadinessReady},
	AdapterJira:          {Owner: OwnerIntegrationGateway, Route: RouteManagedMCP, Readiness: ReadinessReady},
	AdapterConfluence:    {Owner: OwnerIntegrationGateway, Route: RouteManagedMCP, Readiness: ReadinessReady},
	AdapterEmailHTTPS:    {Owner: OwnerIntegrationGateway, Route: RouteManagedMCP, Readiness: ReadinessReady},
	AdapterHTTPSJSONRead: {Owner: OwnerIntegrationGateway, Route: RouteManagedMCP, Readiness: ReadinessReady},
	AdapterOpenAPIMCP:    {Owner: OwnerIntegrationGateway, Route: RouteManagedMCP, Readiness: ReadinessReady},
	AdapterMattermost:    {Owner: OwnerInteractionGateway, Route: RouteInteraction, Readiness: ReadinessReady},
	AdapterContext7:      {Owner: OwnerIntegrationGateway, Route: RouteManagedMCP, Readiness: ReadinessReady},
	AdapterSSHCompose:    {Owner: OwnerIntegrationGateway, Route: RouteManagedMCP, Readiness: ReadinessReady},
}

// Adapter looks up one registered descriptor without inventing an owner or execution route for unknown keys.
// Adapter находит зарегистрированный дескриптор, не назначая владельца или маршрут неизвестному ключу.
func Adapter(key string) (AdapterDescriptor, bool) {
	descriptor, ok := adapterRegistry[AdapterKey(key)]
	return descriptor, ok
}

// ValidateAdapterBinding rejects package metadata that disagrees with the registry owner, route or readiness.
// ValidateAdapterBinding отклоняет метаданные пакета, расходящиеся с владельцем, маршрутом или готовностью реестра.
func ValidateAdapterBinding(definition Package) error {
	descriptor, ok := Adapter(definition.Spec.Adapter)
	if !ok || string(descriptor.Owner) != definition.Spec.AdapterOwner ||
		string(descriptor.Route) != definition.Spec.ExecutionRoute ||
		string(descriptor.Readiness) != definition.Spec.Readiness {
		return errors.New("integration adapter binding is invalid")
	}
	return nil
}

// ExecutableBy admits a package only for the registered ready owner and route; a shipped OpenAPI
// template remains non-executable until a separately managed revision is created.
// ExecutableBy допускает пакет лишь для зарегистрированных готовых владельца и маршрута;
// поставленный шаблон OpenAPI не исполняется до создания отдельной управляемой версии.
func (definition Package) ExecutableBy(owner AdapterOwner, route ExecutionRoute) bool {
	descriptor, ok := Adapter(definition.Spec.Adapter)
	return ok && descriptor.Owner == owner && descriptor.Route == route &&
		descriptor.Readiness == ReadinessReady && ValidateAdapterBinding(definition) == nil &&
		!(definition.Spec.Adapter == string(AdapterOpenAPIMCP) && definition.Metadata.Origin == Origin)
}

// CallableByAgent excludes inbound interaction and gate-decision subscriptions from MCP commands
// that an agent may invoke; a syntactically valid operation name alone does not grant execution.
// CallableByAgent отделяет пользовательскую команду MCP от подписки,
// которая принимает события и решения реального внешнего пользователя.
func (capability Capability) CallableByAgent() bool {
	return validKey(capability.Operation) && capability.Operation != "mattermost.inbound" && capability.Operation != "mattermost.gate_decisions"
}

var (
	// fieldFormats is the closed set of scalar validation formats understood by package fields.
	// fieldFormats — закрытый набор форматов проверки скалярных полей пакета.
	fieldFormats = map[FieldFormat]struct{}{
		"": {}, "PLAIN": {}, "HTTPS_ORIGIN": {}, "HTTPS_URL": {}, "HTTPS_PATH": {},
		"EMAIL": {}, "HOST": {}, "IDENTIFIER": {}, "PUBLIC_IP": {},
	}
	// resourceKinds lists the exact external resource scopes supported by published capabilities.
	// resourceKinds перечисляет точные области внешних ресурсов, поддержанные опубликованными возможностями.
	resourceKinds = map[ResourceKind]struct{}{
		"SYNTHETIC_JOURNAL": {}, "GITHUB_REPOSITORY": {}, "GITLAB_PROJECT": {},
		"JIRA_PROJECT": {}, "CONFLUENCE_SPACE": {}, "EMAIL_SENDER": {},
		"MATTERMOST_CHANNEL": {}, "HTTPS_RESOURCE": {}, "SSH_TARGET": {},
	}
)

// validAdapter accepts only a key present in the closed execution registry.
// validAdapter допускает только ключ из закрытого реестра исполнения.
func validAdapter(value string) bool {
	_, ok := adapterRegistry[AdapterKey(value)]
	return ok
}

// validFieldType rejects field types outside the three scalar wire representations.
// validFieldType отклоняет типы полей вне трёх скалярных wire-представлений.
func validFieldType(value string) bool {
	_, ok := map[FieldType]struct{}{FieldString: {}, FieldInteger: {}, FieldBoolean: {}}[FieldType(value)]
	return ok
}

// validFieldFormat accepts only registered scalar validators, including the plain empty format.
// validFieldFormat допускает только зарегистрированные скалярные проверки, включая пустой формат.
func validFieldFormat(value string) bool { _, ok := fieldFormats[FieldFormat(value)]; return ok }

// validRisk rejects operation risk labels outside the closed read/write/sensitive/destructive set.
// validRisk отклоняет метки риска вне закрытого набора чтения, записи, повышенного и разрушительного риска.
func validRisk(value string) bool {
	return value == string(RiskRead) || value == string(RiskWrite) || value == string(RiskSensitive) || value == string(RiskDestructive)
}

// validApprovalPolicy restricts packages to the three supported human-approval contracts.
// validApprovalPolicy ограничивает пакеты тремя поддержанными контрактами подтверждения человеком.
func validApprovalPolicy(value string) bool {
	return value == string(ApprovalNone) || value == string(ApprovalHumanEachEffect) || value == string(ApprovalHumanScoped)
}

// validResourceKind accepts only explicitly registered resource scopes.
// validResourceKind допускает только явно зарегистрированные области ресурсов.
func validResourceKind(value string) bool { _, ok := resourceKinds[ResourceKind(value)]; return ok }

// validIdempotency accepts only retry guarantees implemented by an adapter execution route.
// validIdempotency допускает только гарантии повтора, реализованные маршрутом исполнения адаптера.
func validIdempotency(value string) bool {
	return value == string(IdempotencyReadOnly) || value == string(IdempotencyEffectKey) ||
		value == string(IdempotencyProviderNative) || value == string(IdempotencyOneAttempt)
}
