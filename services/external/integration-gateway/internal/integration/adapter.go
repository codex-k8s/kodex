// Package integration validates owner-claimed integration operations and dispatches them through a closed registry of typed provider adapters.
// RU: Пакет integration проверяет назначенные владельцем операции интеграций и направляет их через закрытый реестр типизированных provider-адаптеров.
package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/credentialfs"
	emailapi "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/google/go-github/v74/github"
	"github.com/google/uuid"
)

const (
	// maximumResponseBytes bounds safe projections, generic provider bodies and serialized generic request payloads, not every raw SDK response.
	// RU: maximumResponseBytes ограничивает безопасные проекции, обычные provider-ответы и сериализованные запросы, но не все сырые ответы SDK.
	maximumResponseBytes = 64 << 10
	// githubAPIBaseURL fixes the production GitHub REST origin; tests replace the parsed adapter field with an isolated local endpoint.
	// RU: githubAPIBaseURL закрепляет production-адрес GitHub REST; тесты заменяют разобранное поле адаптера изолированным локальным адресом.
	githubAPIBaseURL = "https://api.github.com/"
	// syntheticServiceHost pins the only synthetic service accepted by the production constructor.
	// RU: syntheticServiceHost закрепляет единственный синтетический сервис, допустимый production-конструктором.
	syntheticServiceHost = "integration-synthetic.kodex-system.svc.cluster.local"
	// exactCredentialSecretPrefix confines credential references to the managed integration-credential secret namespace before key reads.
	// RU: exactCredentialSecretPrefix ограничивает ссылки управляемым secret-пространством credentials интеграций до чтения ключа.
	exactCredentialSecretPrefix = "kodex-system/kodex-integration-credentials#"
	// credentialReadRetryInterval spaces projected-file read retries while the caller's operation context limits the total wait.
	// RU: credentialReadRetryInterval задаёт паузу повторов чтения спроецированного файла, а контекст операции ограничивает общее ожидание.
	credentialReadRetryInterval = 250 * time.Millisecond
	// credentialProjectionPendingCode reports a transient credential read timeout only through the connection health-test path.
	// RU: credentialProjectionPendingCode сообщает временное истечение чтения credential только через путь проверки подключения.
	credentialProjectionPendingCode = "INTEGRATION_CREDENTIAL_PROJECTION_PENDING"
)

// Config supplies exact gateway proxies, mounted credential locations, optional local OpenAPI TLS and provider operation budgets, never raw secrets.
// RU: Config задаёт точные proxy шлюза, расположение credential-файлов, необязательный локальный OpenAPI TLS и бюджеты операций, но не сырые секреты.
// New validates these process settings; owner package revisions and operation policy are validated separately for each request.
// RU: New проверяет настройки процесса; owner-ревизии пакетов и политика операций проверяются отдельно для каждого запроса.
type Config struct {
	RPCProfile                                                       string
	CredentialDirectory, ProxyURL, OpenAPIProxyURL, SyntheticBaseURL string
	LocalOpenAPIBaseURL, LocalOpenAPICAFile                          string
	EmailCAFile, EmailCertificateFile, EmailPrivateKeyFile           string
	Timeout                                                          time.Duration
}

// CredentialRevision identifies the owner-selected credential revision and expected projected content without carrying its secret value.
// RU: CredentialRevision обозначает выбранную владельцем ревизию credential и ожидаемое спроецированное содержимое без самого секрета.
// readCredential validates metadata shape and the exact trimmed-content digest; these identifiers alone do not authorize an operation.
// RU: readCredential проверяет форму метаданных и точный хеш очищенного по краям содержимого; сами идентификаторы не разрешают операцию.
type CredentialRevision struct {
	Ref, SecretRef, SecretUID, SecretResourceVersion, ContentSHA256 string
	Revision                                                        int64
}

// Request carries the owner claim's definition, capability, scope, input, grant and credential pins to typed provider execution.
// RU: Request передаёт привязки определения, возможности, области, входа, grant и credential из owner claim в типизированное provider-исполнение.
// The private healthCheck flag is set only by Test; transported identifiers and digests are checked against the parsed executable package, not treated as authority.
// RU: Закрытый healthCheck задаёт только Test; переданные идентификаторы и хеши сверяются с исполняемым пакетом, а не считаются источником полномочий.
type Request struct {
	healthCheck                                                       bool
	DefinitionPackage                                                 []byte
	EmailExecution                                                    *emailapi.ExecutionBinding
	DefinitionKey, DefinitionVersion, DefinitionDigest, ConnectionRef string
	CapabilityKey, Operation, Risk, ApprovalPolicy                    string
	ResourceKind, ResourceScopeDigest, EffectKey, InputDigest         string
	Configuration, Input                                              map[string]any
	ResourceScope                                                     map[string]string
	Credential                                                        *CredentialRevision
	GrantRef                                                          string
	GrantVersion                                                      int64
}

// Receipt binds an execution projection to the claimed effect, canonical input and provider reference with a digest of the safe response bytes.
// RU: Receipt связывает проекцию исполнения с назначенным эффектом, каноническим входом и provider-ссылкой через хеш безопасных байтов ответа.
// It is evidence submitted to owner completion, not a replacement for owner validation or a new approval.
// RU: Это доказательство для owner completion, а не замена owner-проверки либо новое одобрение.
type Receipt struct {
	EffectKey, InputDigest, ProviderEffectRef, ResponseDigest string
}

// Result carries a provider projection, optional safe health detail and effect receipt; validateExecutionResult must check the summary before ordinary execution returns it.
// RU: Result передаёт provider-проекцию, необязательные безопасные сведения здоровья и квитанцию эффекта; validateExecutionResult проверяет сводку до возврата обычного исполнения.
type Result struct {
	HealthSummary string
	Summary       string
	Receipt       Receipt
}

// SafeError carries a classified caller-facing code, optional safe health summary and credential-read transient flag without wrapping provider diagnostics.
// RU: SafeError хранит классифицированный внешний код, необязательную безопасную сводку здоровья и признак временной ошибки чтения credential без исходной provider-диагностики.
type SafeError struct {
	Code          string
	HealthSummary string
	Transient     bool
}

// Error exposes only the safe code, keeping auxiliary health metadata and provider details out of ordinary error strings.
// RU: Error возвращает только безопасный код, не включая вспомогательные сведения здоровья и provider-данные в строку ошибки.
func (err *SafeError) Error() string { return err.Code }

// UnknownOutcomeError marks a potentially applied external mutation whose outcome cannot be proven; its private stage is diagnostic, not retry permission.
// RU: UnknownOutcomeError отмечает возможно выполненное внешнее изменение с недоказанным результатом; закрытый stage служит диагностикой, а не разрешением повтора.
type UnknownOutcomeError struct{ stage string }

// Error returns the same closed public code for every unknown stage, without disclosing transport or provider payloads.
// RU: Error возвращает один закрытый внешний код для любого неизвестного этапа, не раскрывая transport либо provider payload.
func (*UnknownOutcomeError) Error() string { return "INTEGRATION_OUTCOME_UNKNOWN" }

// UnknownOutcomeStage unwraps an unknown outcome into a closed diagnostic class, mapping an unrecognized stage to unclassified and other errors to not_unknown.
// RU: UnknownOutcomeStage извлекает закрытый диагностический класс, заменяя неизвестный stage на unclassified, а другую ошибку — на not_unknown.
// It never changes the public outcome code or permits URL, input or credential details in a log label.
// RU: Функция не меняет внешний код результата и не допускает URL, входные данные либо credentials в диагностическую метку.
func UnknownOutcomeStage(err error) string {
	var unknown *UnknownOutcomeError
	if !errors.As(err, &unknown) {
		return "not_unknown"
	}
	switch unknown.stage {
	case "transport", "response_body", "provider_status", "response_validation", "receipt_validation":
		return unknown.stage
	default:
		return "unclassified"
	}
}

// IsUnknownOutcome recognizes wrapped unknown-outcome errors without classifying unrelated failures as evidence of a completed or repeatable effect.
// RU: IsUnknownOutcome распознаёт обёрнутую ошибку неопределённости, не превращая другие ошибки в доказательство выполненного либо повторяемого эффекта.
func IsUnknownOutcome(err error) bool {
	var unknown *UnknownOutcomeError
	return errors.As(err, &unknown)
}

// Adapter owns shipped executable definitions, a projected credential reader and purpose-specific HTTP clients for typed provider operations.
// RU: Adapter владеет поставляемыми исполняемыми определениями, чтением спроецированных credentials и специализированными HTTP-клиентами типизированных операций.
// Request validation and effect receipts remain separate from transport; the adapter does not create owner grants or make HumanGate decisions.
// RU: Проверка запросов и квитанции эффектов отделены от транспорта; адаптер не создаёт owner grants и не принимает решения HumanGate.
type Adapter struct {
	proxyURL            string
	credentials         *credentialfs.Store
	definitions         map[string]integrationpackage.Package
	githubHTTPClient    *http.Client
	githubBaseURL       *url.URL
	providerHTTPClient  *http.Client
	openAPIHTTPClient   *http.Client
	context7HTTPClient  *http.Client
	localOpenAPIClient  *http.Client
	emailHTTPClient     *http.Client
	syntheticClient     *http.Client
	syntheticBaseURL    *url.URL
	localOpenAPIBaseURL *url.URL
	timeout             time.Duration
}

// New checks the local writeback runtime and exact proxy/synthetic endpoints, then loads shipped definitions and builds separate bounded provider clients.
// RU: New проверяет локальную среду записи и точные proxy/synthetic адреса, загружает поставляемые определения и создаёт отдельные ограниченные provider-клиенты.
// GitHub uses a fixed SNI and TLS 1.3; generic and OpenAPI transports retain distinct proxies, and all constructed HTTP clients reject redirects.
// RU: GitHub использует фиксированный SNI и TLS 1.3; обычный и OpenAPI transport сохраняют разные proxy, все создаваемые HTTP-клиенты запрещают redirects.
// Optional local OpenAPI, email TLS and Context7 transports are delegated to their own constructors; no provider operation is executed here.
// RU: Необязательные локальный OpenAPI, email TLS и Context7 transport передаются своим конструкторам; provider-операция здесь не выполняется.
func New(config Config) (*Adapter, error) {
	if err := checkWriteBackRuntime(); err != nil {
		return nil, err
	}
	proxy, err := url.Parse(config.ProxyURL)
	if err != nil || proxy.Scheme != "http" || proxy.Host != "egress-gateway.kodex-system.svc.cluster.local:8080" ||
		proxy.Path != "" || proxy.RawQuery != "" || proxy.User != nil {
		return nil, errors.New("integration adapter proxy is invalid")
	}
	openAPIProxy, err := url.Parse(config.OpenAPIProxyURL)
	if err != nil || openAPIProxy.Scheme != "http" || openAPIProxy.Host != "egress-gateway-openapi.kodex-system.svc.cluster.local:8083" ||
		openAPIProxy.Path != "" || openAPIProxy.RawQuery != "" || openAPIProxy.User != nil {
		return nil, errors.New("integration adapter OpenAPI proxy is invalid")
	}
	syntheticBase, err := url.Parse(config.SyntheticBaseURL)
	if err != nil || syntheticBase.Scheme != "http" || syntheticBase.Hostname() != syntheticServiceHost ||
		syntheticBase.Port() != "8080" || syntheticBase.Path != "" || syntheticBase.RawQuery != "" || syntheticBase.User != nil {
		return nil, errors.New("synthetic integration endpoint is invalid")
	}
	credentials, err := credentialfs.New(config.CredentialDirectory)
	if err != nil || config.Timeout < time.Second || config.Timeout > 2*time.Minute {
		return nil, errors.New("integration adapter configuration is invalid")
	}
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		return nil, errors.New("load shipped integration definitions")
	}
	githubTransport := &http.Transport{
		Proxy: http.ProxyURL(proxy), ForceAttemptHTTP2: true,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS13, ServerName: "api.github.com"},
		MaxIdleConns:          4,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: config.Timeout,
	}
	providerTransport := githubTransport.Clone()
	providerTransport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS13}
	openAPITransport := providerTransport.Clone()
	openAPITransport.Proxy = http.ProxyURL(openAPIProxy)
	localOpenAPIBaseURL, localOpenAPIClient, err := newLocalOpenAPIClient(config)
	if err != nil {
		return nil, err
	}
	emailClient, err := newEmailClient(config)
	if err != nil {
		return nil, err
	}
	context7Client, err := newContext7HTTPClient(config)
	if err != nil {
		return nil, err
	}
	return &Adapter{
		proxyURL:           config.ProxyURL,
		emailHTTPClient:    emailClient,
		context7HTTPClient: context7Client,
		credentials:        credentials, definitions: definitions,
		githubHTTPClient: &http.Client{Transport: githubTransport, Timeout: config.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("GitHub redirect is forbidden") }},
		githubBaseURL:    mustURL(githubAPIBaseURL),
		providerHTTPClient: &http.Client{
			Transport: providerTransport,
			Timeout:   config.Timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return errors.New("provider redirect is forbidden")
			},
		},
		openAPIHTTPClient: &http.Client{Transport: openAPITransport, Timeout: config.Timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("OpenAPI redirect is forbidden") }},
		localOpenAPIClient: localOpenAPIClient,
		syntheticClient: &http.Client{
			Timeout: config.Timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return errors.New("synthetic integration redirect is forbidden")
			},
		},
		syntheticBaseURL: syntheticBase, localOpenAPIBaseURL: localOpenAPIBaseURL, timeout: config.Timeout,
	}, nil
}

// RequestFromTest maps an owner connection-test claim, copying package bytes and carrying credential metadata and exact email lease binding without reading secrets.
// RU: RequestFromTest преобразует owner claim проверки подключения, копируя пакет и передавая credential-метаданные с точной email-арендой без чтения секретов.
// It does not set the private health bypass itself; Test validates and constructs the health capability before Execute.
// RU: Функция не задаёт закрытый обход health-проверки; Test проверяет и формирует health capability до Execute.
func RequestFromTest(claim *controlplanev1.IntegrationConnectionTestClaim) Request {
	configuration := map[string]any{}
	if claim.GetPublicConfiguration() != nil {
		configuration = claim.GetPublicConfiguration().AsMap()
	}
	return Request{
		DefinitionPackage: append([]byte(nil), claim.GetDefinitionPackage()...),
		EmailExecution:    emailExecutionBinding("", claim.GetTestRef(), claim.GetLease()),
		DefinitionKey:     claim.GetDefinitionKey(), DefinitionVersion: claim.GetDefinitionVersion(),
		DefinitionDigest: claim.GetDefinitionDigest(), ConnectionRef: claim.GetConnectionRef(),
		Configuration: configuration, Credential: credentialFromProto(claim.GetCredentialRevision()),
	}
}

// RequestFromInvocation maps owner-assigned capability, scope, input, effect, grant and email-execution pins while copying definition-package bytes.
// RU: RequestFromInvocation преобразует назначенные владельцем привязки capability, scope, input, effect, grant и email-исполнения, копируя байты пакета определения.
// It converts transport enums to canonical adapter names without authorizing the invocation; validateInvocation remains mandatory before dispatch.
// RU: Функция переводит transport enum в канонические имена адаптера, не авторизуя вызов; перед отправкой обязательна validateInvocation.
func RequestFromInvocation(claim *controlplanev1.IntegrationInvocationClaim) Request {
	configuration, input := map[string]any{}, map[string]any{}
	if claim.GetPublicConfiguration() != nil {
		configuration = claim.GetPublicConfiguration().AsMap()
	}
	if claim.GetBoundedInput() != nil {
		input = claim.GetBoundedInput().AsMap()
	}
	resourceScope := map[string]string{}
	resourceKind, resourceScopeDigest := "", ""
	if scope := claim.GetResourceScope(); scope != nil {
		resourceScope = scope.GetValues()
		resourceKind = strings.TrimPrefix(scope.GetKind().String(), "INTEGRATION_RESOURCE_KIND_")
		resourceScopeDigest = scope.GetDigest()
	}
	return Request{
		DefinitionPackage: append([]byte(nil), claim.GetDefinitionPackage()...),
		EmailExecution:    emailExecutionBinding(claim.GetInvocationRef(), "", claim.GetLease()),
		DefinitionKey:     claim.GetDefinitionKey(), DefinitionVersion: claim.GetDefinitionVersion(),
		DefinitionDigest: claim.GetDefinitionDigest(), ConnectionRef: claim.GetConnectionRef(),
		CapabilityKey: claim.GetCapabilityKey(), Operation: claim.GetOperation(),
		Risk:           strings.TrimPrefix(claim.GetRisk().String(), "INTEGRATION_RISK_"),
		ApprovalPolicy: strings.TrimPrefix(claim.GetApprovalPolicy().String(), "INTEGRATION_APPROVAL_POLICY_"),
		ResourceKind:   resourceKind, ResourceScope: resourceScope, ResourceScopeDigest: resourceScopeDigest,
		EffectKey: claim.GetEffectKey(), InputDigest: claim.GetInputDigest(), Configuration: configuration,
		Input: input, Credential: credentialFromProto(claim.GetCredentialRevision()),
		GrantRef: claim.GetGrantRef(), GrantVersion: claim.GetGrantVersion(),
	}
}

// credentialFromProto copies revision and projected-secret metadata, preserving absence as nil and never loading the credential value.
// RU: credentialFromProto копирует метаданные ревизии и спроецированного секрета, сохраняя отсутствие как nil и не загружая значение credential.
func credentialFromProto(value *controlplanev1.IntegrationCredentialRevision) *CredentialRevision {
	if value == nil {
		return nil
	}
	return &CredentialRevision{
		Ref: value.GetRef(), Revision: value.GetRevision(), SecretRef: value.GetSecretRef(),
		SecretUID: value.GetSecretUid(), SecretResourceVersion: value.GetSecretResourceVersion(),
		ContentSHA256: value.GetContentSha256(),
	}
}

// Outcome classifies success, wrapped safe errors and unknown effects into owner-completion fields, replacing all other diagnostics with INTEGRATION_UNAVAILABLE.
// RU: Outcome преобразует успех, обёрнутые безопасные ошибки и неопределённые эффекты в поля owner completion, заменяя прочую диагностику на INTEGRATION_UNAVAILABLE.
func Outcome(err error) (bool, string) {
	if err == nil {
		return true, ""
	}
	var safe *SafeError
	if errors.As(err, &safe) {
		return false, safe.Code
	}
	if IsUnknownOutcome(err) {
		return false, "INTEGRATION_OUTCOME_UNKNOWN"
	}
	return false, "INTEGRATION_UNAVAILABLE"
}

// Test validates the definition and configuration, selects only its READ/NONE health capability and derives exact empty-input and resource-scope digests.
// RU: Test проверяет определение и конфигурацию, выбирает только health capability READ/NONE и выводит точные хеши пустого входа и области ресурсов.
// Context7 has a dedicated probe; other providers run through Execute with reduced health budgets and a private test flag, never a general grant bypass.
// RU: Context7 имеет отдельную проверку; другие провайдеры проходят Execute с сокращёнными health-бюджетами и закрытым флагом теста, а не общим обходом grant.
// Only transient credential-read failure becomes projection-pending; email health may return its own safe summary alongside a not-ready error.
// RU: Только временная ошибка чтения credential становится projection-pending; email health может вернуть свою безопасную сводку вместе с not-ready ошибкой.
func (adapter *Adapter) Test(ctx context.Context, request Request) (string, error) {
	definition, err := adapter.validateDefinition(request)
	if err != nil {
		return "", err
	}
	configuration, err := normalizeStringMap(request.Configuration)
	if err != nil || definition.ValidateConfiguration(configuration) != nil {
		return "", &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	capability, ok := definition.CapabilityByOperation(definition.Spec.HealthCheck.Operation)
	if !ok || capability.Risk != "READ" || capability.ApprovalPolicy != "NONE" {
		return "", &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
	if definition.Spec.Adapter == "CONTEXT7" {
		summary, err := adapter.testContext7(ctx, request)
		return summary, healthCredentialError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(definition.Spec.HealthCheck.TimeoutSeconds)*time.Second)
	defer cancel()
	request.healthCheck = true
	request.CapabilityKey, request.Operation = capability.Key, capability.Operation
	request.Risk, request.ApprovalPolicy = capability.Risk, capability.ApprovalPolicy
	request.ResourceKind = capability.ResourceScope.Kind
	request.ResourceScope, err = capability.ResourceScopeValues(configuration)
	if err != nil {
		return "", &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	scopeJSON, _ := json.Marshal(request.ResourceScope)
	scopeDigest, inputDigest := sha256.Sum256(scopeJSON), sha256.Sum256([]byte("{}"))
	request.ResourceScopeDigest, request.InputDigest = hex.EncodeToString(scopeDigest[:]), hex.EncodeToString(inputDigest[:])
	request.Input, request.EffectKey = map[string]any{}, "health-check"
	result, err := adapter.Execute(ctx, request)
	var safe *SafeError
	err = healthCredentialError(err)
	if errors.As(err, &safe) && safe.Code == emailapi.HealthNotReadyCode {
		return safe.HealthSummary, err
	}
	if err == nil && definition.Spec.Adapter == "EMAIL_HTTPS" {
		if result.HealthSummary != "" {
			return result.HealthSummary, nil
		}
		return "i18n:INTEGRATION_TEST_SUCCEEDED", nil
	}
	return "i18n:INTEGRATION_TEST_SUCCEEDED", err
}

// healthCredentialError maps only a transient credential-unavailable error to the health-test pending code, preserving all other errors unchanged.
// RU: healthCredentialError заменяет только временную credential-unavailable ошибку на код ожидания health-теста, сохраняя остальные ошибки без изменений.
func healthCredentialError(err error) error {
	var safe *SafeError
	if errors.As(err, &safe) && safe.Code == "INTEGRATION_CREDENTIAL_UNAVAILABLE" && safe.Transient {
		// Только READ health-test: ожидание exact проекции не означает неверный
		// credential и не разрешает повтор обычного invocation/WRITE.
		return &SafeError{Code: credentialProjectionPendingCode}
	}
	return err
}

// Execute validates claim/package/capability bindings before dispatching one bounded operation through the closed provider registry and validating its result.
// RU: Execute проверяет привязки claim, пакета и capability до одной ограниченной операции закрытого provider-реестра и проверки её результата.
// Email receives only its managed execution binding, not the already-checked legacy credential metadata; unknown adapter kinds fail closed.
// RU: Email получает управляемую привязку исполнения, а не уже проверенные legacy credential-метаданные; неизвестный вид адаптера закрыто отклоняется.
func (adapter *Adapter) Execute(ctx context.Context, request Request) (Result, error) {
	definition, capability, canonicalInput, configuration, err := adapter.validateInvocation(request)
	if err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(capability.Execution.TimeoutSeconds)*time.Second)
	defer cancel()
	var result Result
	switch definition.Spec.Adapter {
	case "SYNTHETIC_HTTP":
		result, err = adapter.executeSynthetic(ctx, request, configuration, canonicalInput, false)
	case "GITHUB":
		result, err = adapter.executeGitHub(ctx, request, capability, configuration, canonicalInput)
	case "GITLAB":
		result, err = adapter.executeGitLab(ctx, request, capability, configuration, canonicalInput)
	case "JIRA":
		result, err = adapter.executeJira(ctx, request, capability, configuration, canonicalInput)
	case "CONFLUENCE":
		result, err = adapter.executeConfluence(ctx, request, capability, configuration, canonicalInput)
	case "EMAIL_HTTPS":
		// Legacy metadata уже проверена вместе с owner claim; в mail adapter она не передаётся.
		request.Credential = nil
		result, err = adapter.executeEmail(ctx, request, capability, configuration, canonicalInput)
	case "HTTPS_JSON_READ":
		result, err = adapter.executeHTTPSJSONRead(ctx, request, capability, configuration)
	case "OPENAPI_MCP":
		result, err = adapter.executeOpenAPI(ctx, request, capability, configuration, canonicalInput)
	case "CONTEXT7":
		result, err = adapter.executeContext7(ctx, request, capability, configuration, canonicalInput)
	default:
		err = &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
	return validateExecutionResult(capability, request, result, err)
}

// Recover validates the original invocation and performs only synthetic journal-write readback within the capability deadline.
// RU: Recover проверяет исходный вызов и выполняет только readback записи синтетического журнала в пределах срока capability.
// Every unsupported provider or operation remains UNKNOWN without receiving a repeated mutation; normal output and receipt validation still applies.
// RU: Любой неподдерживаемый провайдер либо операция остаётся UNKNOWN без повторного изменения; обычная проверка выхода и квитанции сохраняется.
func (adapter *Adapter) Recover(ctx context.Context, request Request) (Result, error) {
	definition, capability, canonicalInput, configuration, err := adapter.validateInvocation(request)
	if err != nil {
		return Result{}, err
	}
	if definition.Spec.Adapter != "SYNTHETIC_HTTP" || request.Operation != "synthetic.journal.write" {
		return Result{}, &UnknownOutcomeError{}
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(capability.Execution.TimeoutSeconds)*time.Second)
	defer cancel()
	result, err := adapter.executeSynthetic(ctx, request, configuration, canonicalInput, true)
	return validateExecutionResult(capability, request, result, err)
}

// validateExecutionResult canonicalizes output through the capability schema, rehashes it and verifies operation, effect, input and provider-reference bindings.
// RU: validateExecutionResult канонизирует выход схемой capability, пересчитывает хеш и сверяет привязки операции, эффекта, входа и provider-ссылки.
// Invalid READ output returns a safe error; invalid non-READ output or receipt, and selected unavailable errors, remain UNKNOWN rather than implying no effect.
// RU: Неверный READ-выход даёт безопасную ошибку; неверный non-READ выход либо квитанция и выбранные unavailable-ошибки остаются UNKNOWN, не означая отсутствия эффекта.
func validateExecutionResult(capability integrationpackage.Capability, request Request, result Result, err error) (Result, error) {
	if err != nil {
		var safe *SafeError
		if capability.Risk != "READ" && errors.As(err, &safe) &&
			(safe.Code == "INTEGRATION_UNAVAILABLE" || safe.Code == "INTEGRATION_RESPONSE_INVALID") {
			return Result{}, &UnknownOutcomeError{stage: "response_validation"}
		}
		return Result{}, err
	}
	canonicalOutput, err := capability.ValidateOutput([]byte(result.Summary))
	if err != nil {
		if capability.Risk != "READ" {
			return Result{}, &UnknownOutcomeError{stage: "response_validation"}
		}
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	result.Summary = string(canonicalOutput)
	responseDigest := sha256.Sum256(canonicalOutput)
	result.Receipt.ResponseDigest = hex.EncodeToString(responseDigest[:])
	if capability.Operation != request.Operation || result.Receipt.EffectKey != request.EffectKey ||
		result.Receipt.InputDigest != request.InputDigest || result.Receipt.ProviderEffectRef == "" ||
		len(result.Receipt.ResponseDigest) != sha256.Size*2 || result.Summary == "" || len(result.Summary) > maximumResponseBytes {
		if capability.Risk != "READ" {
			return Result{}, &UnknownOutcomeError{stage: "receipt_validation"}
		}
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return result, nil
}

// validateDefinition parses a bounded pinned package and compares its executable revision, identity and digest with the shipped definition.
// RU: validateDefinition разбирает ограниченный закреплённый пакет и сверяет исполняемую ревизию, идентичность и хеш с поставляемым определением.
// Required shipped destinations except github_git, managed-MCP gateway ownership and the credential-presence contract must match before execution.
// RU: До исполнения должны совпадать обязательные поставляемые назначения кроме github_git, принадлежность gateway маршрута managed-MCP и договор наличия credential.
func (adapter *Adapter) validateDefinition(request Request) (integrationpackage.Package, error) {
	shipped, exists := adapter.definitions[request.DefinitionKey]
	if !exists || len(request.DefinitionPackage) == 0 || len(request.DefinitionPackage) > 256<<10 {
		return integrationpackage.Package{}, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	definition, err := integrationpackage.Parse(request.DefinitionPackage)
	if err != nil || integrationpackage.ValidateExecutableRevision(definition, shipped) != nil || definition.Metadata.Key != request.DefinitionKey || definition.Metadata.Version != request.DefinitionVersion || definition.Digest != request.DefinitionDigest {
		return integrationpackage.Package{}, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	for _, destination := range shipped.Spec.NetworkDestinations {
		if destination.Key == "github_git" {
			continue
		}
		if !definition.HasNetworkDestination(destination) {
			return integrationpackage.Package{}, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
		}
	}
	if !definition.ExecutableBy(integrationpackage.OwnerIntegrationGateway, integrationpackage.RouteManagedMCP) {
		return integrationpackage.Package{}, &SafeError{Code: "INTEGRATION_ROUTE_NOT_OWNED"}
	}
	if definition.RequiresConnectionCredential() != (request.Credential != nil) {
		return integrationpackage.Package{}, &SafeError{Code: "INTEGRATION_CREDENTIAL_UNAVAILABLE"}
	}
	return definition, nil
}

// validateInvocation requires ordinary claim grant pins and exact capability, policy, resource-scope and canonical-input digests from the executable definition.
// RU: validateInvocation требует grant-привязки обычного claim и точные capability, policy, scope и хеш канонического входа из исполняемого определения.
// It rejects unsupported configuration and repository path/ref inputs, checks dynamic approval policy and workflow inputs, and caps private health-test budgets.
// RU: Функция отклоняет неподдерживаемую конфигурацию и repository path/ref, проверяет динамическую approval policy и workflow inputs, сокращает бюджеты закрытого health-теста.
// This is consistency checking of server-provided claim data, not independent authorization of arbitrary caller-supplied grant identifiers.
// RU: Это проверка согласованности серверного claim, а не самостоятельная авторизация произвольно переданных grant-идентификаторов.
func (adapter *Adapter) validateInvocation(request Request) (
	integrationpackage.Package,
	integrationpackage.Capability,
	[]byte,
	map[string]string,
	error,
) {
	if !request.healthCheck && (request.GrantRef == "" || request.GrantVersion < 1) {
		return integrationpackage.Package{}, integrationpackage.Capability{}, nil, nil, &SafeError{Code: "INTEGRATION_GRANT_INVALID"}
	}
	definition, err := adapter.validateDefinition(request)
	if err != nil {
		return integrationpackage.Package{}, integrationpackage.Capability{}, nil, nil, err
	}
	capability, exists := definition.Capability(request.CapabilityKey)
	if !exists || capability.Operation != request.Operation || capability.Risk != request.Risk ||
		!capability.AllowsApprovalPolicy(request.ApprovalPolicy) || capability.ResourceScope.Kind != request.ResourceKind ||
		request.EffectKey == "" || len(request.InputDigest) != sha256.Size*2 {
		return integrationpackage.Package{}, integrationpackage.Capability{}, nil, nil, &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
	configuration, err := normalizeStringMap(request.Configuration)
	if err != nil || definition.ValidateConfiguration(configuration) != nil {
		return integrationpackage.Package{}, integrationpackage.Capability{}, nil, nil, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	expectedScope, err := capability.ResourceScopeValues(configuration)
	encodedScope, encodeScopeErr := json.Marshal(expectedScope)
	scopeDigest := sha256.Sum256(encodedScope)
	if err != nil || encodeScopeErr != nil || !equalStringMap(expectedScope, request.ResourceScope) ||
		hex.EncodeToString(scopeDigest[:]) != request.ResourceScopeDigest {
		return integrationpackage.Package{}, integrationpackage.Capability{}, nil, nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	encodedInput, err := json.Marshal(request.Input)
	if err != nil {
		return integrationpackage.Package{}, integrationpackage.Capability{}, nil, nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	canonicalInput, err := capability.ValidateInput(encodedInput)
	inputDigest := sha256.Sum256(canonicalInput)
	if err != nil || hex.EncodeToString(inputDigest[:]) != request.InputDigest {
		return integrationpackage.Package{}, integrationpackage.Capability{}, nil, nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	if definition.ValidateInvocationApprovalPolicy(capability, request.ApprovalPolicy, canonicalInput) != nil {
		return integrationpackage.Package{}, integrationpackage.Capability{}, nil, nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	if strings.HasPrefix(request.Operation, "github.") || strings.HasPrefix(request.Operation, "gitlab.") {
		for _, field := range []string{"path", "file_path", "branch", "ref", "head", "base", "target_branch", "source_branch"} {
			if value, ok := request.Input[field].(string); ok && !validRepositoryPath(value, field == "path") {
				return integrationpackage.Package{}, integrationpackage.Capability{}, nil, nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
			}
		}
	}
	if request.Operation == "github.actions.workflow.dispatch" {
		if raw, ok := request.Input["workflow_inputs"].(string); ok {
			if _, err := githubWorkflowInputs(raw); err != nil {
				return integrationpackage.Package{}, integrationpackage.Capability{}, nil, nil, err
			}
		}
	}
	if request.healthCheck {
		capability.Execution.TimeoutSeconds = min(capability.Execution.TimeoutSeconds, definition.Spec.HealthCheck.TimeoutSeconds)
		capability.Execution.MaxAttempts = min(capability.Execution.MaxAttempts, definition.Spec.HealthCheck.MaxAttempts)
	}
	return definition, capability, canonicalInput, configuration, nil
}

// executeSynthetic reads or writes the configured journal and reconciles an uncertain write only by an effect-key-bound GET, never another POST.
// RU: executeSynthetic читает либо изменяет настроенный журнал и согласует неопределённую запись только GET с привязкой effect key, а не новым POST.
// It rejects unknown or trailing JSON and mismatched journal/effect identity, then projects the operation-specific summary and sequence reference.
// RU: Функция отклоняет неизвестный либо дополнительный JSON и несовпадающие journal/effect, затем формирует сводку операции и ссылку с номером записи.
func (adapter *Adapter) executeSynthetic(ctx context.Context, request Request, configuration map[string]string, canonicalInput []byte, recoverOnly bool) (Result, error) {
	journal := configuration["journal"]
	path := "/v1/journals/" + url.PathEscape(journal)
	method, effectKey, body := http.MethodGet, "", []byte(nil)
	if request.Operation == "synthetic.journal.write" {
		if recoverOnly {
			effectKey = request.EffectKey
		} else {
			method, path, effectKey, body = http.MethodPost, path+"/entries", request.EffectKey, canonicalInput
		}
	}
	response, err := adapter.callSynthetic(ctx, method, path, effectKey, body)
	if IsUnknownOutcome(err) && request.Operation == "synthetic.journal.write" {
		if reconciled, reconcileErr := adapter.callSynthetic(ctx, http.MethodGet, "/v1/journals/"+url.PathEscape(journal), request.EffectKey, nil); reconcileErr == nil {
			response, err = reconciled, nil
		}
	}
	if err != nil {
		return Result{}, err
	}
	var projection struct {
		Journal   string `json:"journal"`
		EffectKey string `json:"effect_key,omitempty"`
		Sequence  int64  `json:"sequence,omitempty"`
		Value     string `json:"value,omitempty"`
		Count     int64  `json:"count"`
	}
	decoder := json.NewDecoder(bytes.NewReader(response))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&projection) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		projection.Journal != journal || request.Operation == "synthetic.journal.write" && projection.EffectKey != request.EffectKey {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	var summary []byte
	if request.Operation == "synthetic.journal.read" {
		summary, err = json.Marshal(struct {
			Journal string `json:"journal"`
			Count   int64  `json:"count"`
		}{Journal: projection.Journal, Count: projection.Count})
	} else {
		summary, err = json.Marshal(projection)
	}
	if err != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	providerRef := "synthetic-journal:" + journal
	if projection.Sequence > 0 {
		providerRef += ":" + strconv.FormatInt(projection.Sequence, 10)
	}
	return successfulResult(string(summary), request, providerRef), nil
}

// callSynthetic sends one request to the fixed synthetic origin with optional effect and JSON headers and reads a bounded successful body.
// RU: callSynthetic отправляет один запрос фиксированному synthetic-адресу с необязательными effect/JSON заголовками и читает ограниченное успешное тело.
// Mutations disable transport replay and retain transport or server errors as unknown outcomes; this helper does not retry or perform reconciliation.
// RU: Изменения отключают transport replay и сохраняют transport либо server-ошибки как неопределённость; helper не повторяет и не согласует операцию.
func (adapter *Adapter) callSynthetic(ctx context.Context, method, path, effectKey string, body []byte) ([]byte, error) {
	endpoint := *adapter.syntheticBaseURL
	endpoint.Path = path
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	request.Header.Set("Accept", "application/json")
	if effectKey != "" {
		request.Header.Set("Idempotency-Key", effectKey)
	}
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet && method != http.MethodHead {
		request.GetBody = nil
		request.Body = io.NopCloser(bytes.NewReader(body))
		if len(body) == 0 {
			request.ContentLength = -1
		}
	}
	response, err := adapter.syntheticClient.Do(request)
	if err != nil {
		if method != http.MethodGet && method != http.MethodHead {
			return nil, &UnknownOutcomeError{}
		}
		return nil, &SafeError{Code: "INTEGRATION_UNAVAILABLE"}
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		if method != http.MethodGet && method != http.MethodHead && response.StatusCode >= http.StatusInternalServerError {
			return nil, &UnknownOutcomeError{}
		}
		return nil, statusError(response.StatusCode)
	}
	return readBoundedResponse(response.Body)
}

// executeGitHub opens a credential-bound SDK client, reads exact repository metadata or dispatches issue/catalog operations using the validated owner and repository.
// RU: executeGitHub открывает SDK-клиент с credential, читает точные метаданные репозитория либо направляет issue/catalog операции в проверенные owner и repository.
// Metadata must identify the selected repository; deferred cleanup clears the loaded credential byte slice after every dispatch outcome.
// RU: Метаданные обязаны обозначать выбранный репозиторий; отложенная очистка обнуляет загруженный срез credential при любом исходе.
func (adapter *Adapter) executeGitHub(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, canonicalInput []byte) (Result, error) {
	client, cleanup, err := adapter.githubClient(ctx, request.Credential)
	if err != nil {
		return Result{}, err
	}
	defer cleanup()
	owner, repository := configuration["owner"], configuration["repository"]
	switch request.Operation {
	case "github.repository.metadata.read":
		provider, err := githubRead(ctx, capability, func() (*github.Repository, *github.Response, error) {
			return client.Repositories.Get(ctx, owner, repository)
		})
		if err != nil {
			return Result{}, err
		}
		projection := struct {
			FullName      string `json:"full_name"`
			DefaultBranch string `json:"default_branch"`
			Visibility    string `json:"visibility"`
			Private       bool   `json:"private"`
			Archived      bool   `json:"archived"`
		}{provider.GetFullName(), provider.GetDefaultBranch(), provider.GetVisibility(), provider.GetPrivate(), provider.GetArchived()}
		summary, marshalErr := json.Marshal(projection)
		if marshalErr != nil || provider.GetID() == 0 || provider.GetFullName() != owner+"/"+repository {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return successfulResult(string(summary), request, "github-repository:"+strconv.FormatInt(provider.GetID(), 10)), nil
	case "github.issue.create":
		return adapter.createGitHubIssue(ctx, client, owner, repository, request, capability, canonicalInput)
	case "github.issue.list":
		return adapter.listGitHubIssues(ctx, client, owner, repository, request, capability, canonicalInput)
	case "github.issue.read":
		return adapter.readGitHubIssue(ctx, client, owner, repository, request, capability, canonicalInput)
	case "github.issue.comment.create":
		return adapter.createGitHubIssueComment(ctx, client, owner, repository, request, capability, canonicalInput)
	case "github.issue.update":
		return adapter.updateGitHubIssue(ctx, client, owner, repository, request, capability, canonicalInput)
	default:
		return adapter.executeGitHubCatalog(ctx, client, owner, repository, request, capability, canonicalInput)
	}
}

// createGitHubIssue first searches the repository for its exact effect marker, then creates at most once with the marker appended to the body.
// RU: createGitHubIssue сначала ищет точный effect-маркер в репозитории, затем создаёт не более одного issue с маркером в конце body.
// An uncertain creation receives read-only marker reconciliation, not another create; a found or newly created issue passes the same result projection.
// RU: Неопределённое создание получает read-only поиск маркера, а не новое создание; найденный либо созданный issue проходит одну и ту же проекцию результата.
func (adapter *Adapter) createGitHubIssue(ctx context.Context, client *github.Client, owner, repository string, request Request, capability integrationpackage.Capability, canonicalInput []byte) (Result, error) {
	var input struct{ Title, Body string }
	if json.Unmarshal(canonicalInput, &input) != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	marker := "<!-- kodex-effect:" + request.EffectKey + " -->"
	if existing, err := findGitHubIssueByMarker(ctx, client, owner, repository, capability, marker); err != nil {
		return Result{}, err
	} else if existing != nil {
		return githubIssueResult(existing, request, false)
	}
	body := strings.TrimSpace(input.Body)
	if body != "" {
		body += "\n\n"
	}
	body += marker
	issue, response, err := client.Issues.Create(ctx, owner, repository, &github.IssueRequest{Title: github.Ptr(input.Title), Body: github.Ptr(body)})
	if mutationErr := githubMutationError(response, err); mutationErr != nil {
		if IsUnknownOutcome(mutationErr) {
			if reconciled, reconcileErr := findGitHubIssueByMarker(ctx, client, owner, repository, capability, marker); reconcileErr == nil && reconciled != nil {
				return githubIssueResult(reconciled, request, false)
			}
		}
		return Result{}, mutationErr
	}
	return githubIssueResult(issue, request, false)
}

// updateGitHubIssue validates the issue number and requested nonempty fields, reads the selected issue and rejects pull requests before one edit attempt.
// RU: updateGitHubIssue проверяет номер issue и заданные непустые поля, читает выбранный issue и отклоняет pull request до единственной попытки изменения.
// An uncertain edit is accepted only after readback matches requested fields; there is no provider-version CAS or blind edit retry in this helper.
// RU: Неопределённое изменение принимается только после совпадения запрошенных полей при readback; provider-version CAS и слепого повтора здесь нет.
func (adapter *Adapter) updateGitHubIssue(ctx context.Context, client *github.Client, owner, repository string, request Request, capability integrationpackage.Capability, canonicalInput []byte) (Result, error) {
	var input struct {
		IssueNumber int64  `json:"issue_number"`
		Title       string `json:"title"`
		Body        string `json:"body"`
		State       string `json:"state"`
	}
	if json.Unmarshal(canonicalInput, &input) != nil || input.IssueNumber < 1 || input.IssueNumber > int64(^uint(0)>>1) ||
		input.Title == "" && input.Body == "" && input.State == "" || input.State != "" && input.State != "open" && input.State != "closed" {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	current, err := githubRead(ctx, capability, func() (*github.Issue, *github.Response, error) {
		return client.Issues.Get(ctx, owner, repository, int(input.IssueNumber))
	})
	if err != nil {
		return Result{}, err
	}
	if current.IsPullRequest() || current.GetNumber() != int(input.IssueNumber) {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	requestBody := &github.IssueRequest{}
	if input.Title != "" {
		requestBody.Title = github.Ptr(input.Title)
	}
	if input.Body != "" {
		requestBody.Body = github.Ptr(input.Body)
	}
	if input.State != "" {
		requestBody.State = github.Ptr(input.State)
	}
	issue, response, err := client.Issues.Edit(ctx, owner, repository, int(input.IssueNumber), requestBody)
	if mutationErr := githubMutationError(response, err); mutationErr != nil {
		if IsUnknownOutcome(mutationErr) {
			current, readErr := githubRead(ctx, capability, func() (*github.Issue, *github.Response, error) {
				return client.Issues.Get(ctx, owner, repository, int(input.IssueNumber))
			})
			if readErr == nil && githubIssueMatchesUpdate(current, input.Title, input.Body, input.State) {
				return githubIssueResult(current, request, false)
			}
		}
		return Result{}, mutationErr
	}
	return githubIssueResult(issue, request, false)
}

// githubIssueResult requires a real issue ID/number rather than a pull request and projects number, title, state and optionally nonempty body.
// RU: githubIssueResult требует настоящий ID/номер issue, а не pull request, и проецирует number, title, state и необязательно непустой body.
// The provider reference uses the issue number and is bound to the current effect/input by providerResult.
// RU: Provider-ссылка использует номер issue и связывается с текущими effect/input через providerResult.
func githubIssueResult(issue *github.Issue, request Request, includeBody bool) (Result, error) {
	if issue == nil || issue.GetNumber() < 1 || issue.GetID() == 0 || issue.IsPullRequest() {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	projection := map[string]any{"number": issue.GetNumber(), "title": issue.GetTitle(), "state": issue.GetState()}
	if includeBody && issue.GetBody() != "" {
		projection["body"] = issue.GetBody()
	}
	return providerResult(request, "github-issue:"+strconv.Itoa(issue.GetNumber()), projection)
}

// githubClient reads the pinned credential and copies the configured HTTP client, adding bounded SDK transport, token authentication and the adapter's API base.
// RU: githubClient читает закреплённый credential и копирует настроенный HTTP-клиент, добавляя ограниченный SDK transport, token-аутентификацию и API base адаптера.
// Its cleanup clears only the loaded byte slice, not token strings copied into SDK authentication state; callers must not claim stronger erasure.
// RU: Очистка обнуляет только загруженный срез байтов, а не строковые копии токена в SDK; вызывающий не должен заявлять более сильное стирание.
func (adapter *Adapter) githubClient(ctx context.Context, credential *CredentialRevision) (*github.Client, func(), error) {
	value, err := adapter.readCredential(ctx, credential)
	if err != nil {
		return nil, func() {}, err
	}
	httpClient := *adapter.githubHTTPClient
	transport := httpClient.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	httpClient.Transport = githubBoundedTransport{next: transport}
	client := github.NewClient(&httpClient).WithAuthToken(string(value))
	client.BaseURL = adapter.githubBaseURL
	return client, func() { clear(value) }, nil
}

// readCredential validates revision metadata and the managed secret prefix, then reads the projected key and requires its exact trimmed-content SHA-256.
// RU: readCredential проверяет метаданные ревизии и управляемый secret-prefix, затем читает спроецированный ключ и требует точного SHA-256 очищенного по краям содержимого.
// Empty or mismatched content fails immediately; file-read failures wait in bounded intervals until caller cancellation yields a transient safe error.
// RU: Пустое либо несовпавшее содержимое отклоняется сразу; ошибки чтения файла ожидаются с паузами до отмены вызывающего, дающей временную безопасную ошибку.
// Successful bytes remain caller-owned and require cleanup; UUID/resource-version shape checks are not a readback of Kubernetes Secret metadata.
// RU: Успешно прочитанные байты принадлежат вызывающему и требуют очистки; проверка формы UUID/resource-version не является чтением метаданных Kubernetes Secret.
func (adapter *Adapter) readCredential(ctx context.Context, credential *CredentialRevision) ([]byte, error) {
	if credential == nil || credential.Ref == "" || credential.Revision < 1 || uuid.Validate(credential.SecretUID) != nil ||
		credential.SecretResourceVersion == "" || len(credential.SecretResourceVersion) > 128 ||
		len(credential.ContentSHA256) != sha256.Size*2 || !strings.HasPrefix(credential.SecretRef, exactCredentialSecretPrefix) {
		return nil, &SafeError{Code: "INTEGRATION_CREDENTIAL_UNAVAILABLE"}
	}
	key := strings.TrimPrefix(credential.SecretRef, exactCredentialSecretPrefix)
	for {
		value, err := adapter.credentials.ReadKey(key)
		if err == nil {
			value = bytes.TrimSpace(value)
			digest := sha256.Sum256(value)
			if len(value) == 0 || hex.EncodeToString(digest[:]) != credential.ContentSHA256 {
				clear(value)
				return nil, &SafeError{Code: "INTEGRATION_CREDENTIAL_UNAVAILABLE"}
			}
			return value, nil
		}
		timer := time.NewTimer(credentialReadRetryInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, &SafeError{Code: "INTEGRATION_CREDENTIAL_UNAVAILABLE", Transient: true}
		case <-timer.C:
		}
	}
}

// successfulResult builds effect/input bindings and the exact summary-byte digest without performing output-schema or provider-reference validation itself.
// RU: successfulResult формирует привязки effect/input и хеш точных байтов summary, самостоятельно не проверяя output schema либо provider-ссылку.
func successfulResult(summary string, request Request, providerRef string) Result {
	digest := sha256.Sum256([]byte(summary))
	return Result{Summary: summary, Receipt: Receipt{
		EffectKey: request.EffectKey, InputDigest: request.InputDigest, ProviderEffectRef: providerRef,
		ResponseDigest: hex.EncodeToString(digest[:]),
	}}
}

// githubError preserves wrapped safe errors and maps failed SDK responses to closed status classes, hiding raw provider diagnostics.
// RU: githubError сохраняет обёрнутые безопасные ошибки и переводит ошибки SDK-ответов в закрытые классы статуса, скрывая сырую provider-диагностику.
func githubError(response *github.Response, err error) error {
	if err == nil {
		return nil
	}
	var safe *SafeError
	if errors.As(err, &safe) {
		return safe
	}
	if response == nil {
		return &SafeError{Code: "INTEGRATION_UNAVAILABLE"}
	}
	return statusError(response.StatusCode)
}

// statusError maps authentication, throttling and selected invalid-request HTTP statuses to safe integration codes, treating all remaining statuses as unavailable.
// RU: statusError переводит аутентификацию, лимиты и выбранные HTTP-ошибки запроса в безопасные integration-коды, считая остальные статусы недоступностью.
func statusError(code int) error {
	switch code {
	case http.StatusUnauthorized, http.StatusForbidden:
		return &SafeError{Code: "INTEGRATION_AUTH_REJECTED"}
	case http.StatusTooManyRequests:
		return &SafeError{Code: "INTEGRATION_RATE_LIMITED"}
	case http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity:
		return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	default:
		return &SafeError{Code: "INTEGRATION_UNAVAILABLE"}
	}
}

// readBoundedResponse reads at most one byte beyond the projection budget and rejects both I/O failure and oversize content without returning partial data.
// RU: readBoundedResponse читает не более одного байта сверх бюджета проекции и отклоняет I/O-ошибку либо превышение без возврата частичных данных.
func readBoundedResponse(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maximumResponseBytes+1))
	if err != nil || len(body) > maximumResponseBytes {
		return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return body, nil
}

// normalizeStringMap copies only actual string values into configuration storage, rejecting implicit conversions and returning an empty map for nil input.
// RU: normalizeStringMap копирует только настоящие строки в конфигурацию, отклоняя неявные преобразования и возвращая пустую map для nil-входа.
func normalizeStringMap(values map[string]any) (map[string]string, error) {
	result := make(map[string]string, len(values))
	for key, raw := range values {
		value, ok := raw.(string)
		if !ok {
			return nil, errors.New("configuration value is not a string")
		}
		result[key] = value
	}
	return result, nil
}

// equalStringMap compares cardinality and retrieved values for each left key; it does not separately distinguish an absent right key from an empty value.
// RU: equalStringMap сравнивает размеры и извлечённые значения для ключей слева, отдельно не отличая отсутствующий правый ключ от пустого значения.
func equalStringMap(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

// mustURL parses a compile-time adapter URL and panics on programmer error; it must not be used to validate untrusted provider input.
// RU: mustURL разбирает статический URL адаптера и паникует при ошибке программиста; его нельзя использовать для проверки недоверенного provider-входа.
func mustURL(raw string) *url.URL {
	parsed, err := url.Parse(raw)
	if err != nil {
		panic(fmt.Sprintf("parse static URL: %v", err))
	}
	return parsed
}
