package mattermost

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/google/uuid"
	"github.com/mattermost/mattermost/server/public/model"
	"google.golang.org/protobuf/types/known/structpb"
)

// maximumInvocationOutput caps the validated canonical result summary in bytes before an effect receipt is accepted.
// RU: maximumInvocationOutput ограничивает проверенное каноническое резюме результата в байтах до принятия квитанции эффекта.
const maximumInvocationOutput = 64 << 10

// credentialSecretPrefix confines descriptors to the configured shared credential Secret; ReadKey independently validates the selected key path.
// RU: credentialSecretPrefix ограничивает descriptor заданным общим Secret реквизитов; ReadKey независимо проверяет путь выбранного ключа.
const credentialSecretPrefix = "kodex-system/kodex-integration-credentials#"

// errInvocation rejects a capability, bounded input or scope inconsistent with the selected executable package.
// RU: errInvocation отклоняет capability, ограниченный ввод или область, несовместимые с выбранным исполняемым пакетом.
var errInvocation = errors.New("mattermost invocation is invalid")

// unknownInvocationError records that an attempted external mutation has no trustworthy final receipt and must not be blindly repeated.
// RU: unknownInvocationError обозначает попытку внешнего изменения без достоверной итоговой квитанции, которую нельзя слепо повторять.
type unknownInvocationError struct{}

// Error returns a fixed diagnostic without embedding provider payload, credential or raw transport error.
// RU: Error возвращает фиксированную диагностику без payload провайдера, реквизитов или сырой транспортной ошибки.
func (*unknownInvocationError) Error() string { return "mattermost invocation outcome is unknown" }

// InvocationOutcome maps adapter errors to success, a closed safe code and an explicit unknown-effect bit, never raw provider text.
// RU: InvocationOutcome отображает ошибки адаптера в успех, закрытый безопасный код и явный признак неизвестного эффекта, не выдавая сырой текст провайдера.
func InvocationOutcome(err error) (bool, string, bool) {
	if err == nil {
		return true, "", false
	}
	var unknown *unknownInvocationError
	if errors.As(err, &unknown) {
		return false, "INTEGRATION_OUTCOME_UNKNOWN", true
	}
	switch {
	case errors.Is(err, errConfiguration):
		return false, "INTEGRATION_CONFIGURATION_INVALID", false
	case errors.Is(err, errCredential):
		return false, "INTEGRATION_CREDENTIAL_UNAVAILABLE", false
	case errors.Is(err, errForbidden):
		return false, "INTEGRATION_AUTH_REJECTED", false
	case errors.Is(err, errRateLimited):
		return false, "INTEGRATION_RATE_LIMITED", false
	case errors.Is(err, errResponse):
		return false, "INTEGRATION_RESPONSE_INVALID", false
	case errors.Is(err, errInvocation):
		return false, "INTEGRATION_REQUEST_REJECTED", false
	default:
		return false, "INTEGRATION_UNAVAILABLE", false
	}
}

// Execute validates the exact managed claim, creates a credential-bound client and runs its operation within the selected capability deadline.
// RU: Execute проверяет точный управляемый claim, создаёт привязанный к реквизитам клиент и выполняет операцию в срок выбранной capability.
func (adapter *Adapter) Execute(ctx context.Context, claim *controlplanev1.IntegrationInvocationClaim) (*controlplanev1.IntegrationEffectReceipt, error) {
	capability, configuration, input, err := adapter.validateInvocation(claim)
	if err != nil {
		return nil, err
	}
	operation, cancel := context.WithTimeout(ctx, time.Duration(capability.Execution.TimeoutSeconds)*time.Second)
	defer cancel()
	client, channel, cleanup, err := adapter.invocationClient(operation, configuration, claim.GetCredentialRevision())
	if err != nil {
		return nil, err
	}
	defer cleanup()
	return executeClaim(operation, client, channel, claim, capability, input)
}

// executeClaim executes typed provider work, validates bounded canonical output and hashes that exact output into an intent-bound receipt.
// RU: executeClaim выполняет типизированную работу провайдера, проверяет ограниченный канонический результат и хеширует его точные байты в квитанцию с привязкой к intent.
// Attempted mutations with unproven failure or invalid output remain unknown; a read failure is not converted into a write effect.
// RU: Попытки изменения с недоказанным отказом или неверным результатом остаются неизвестными; отказ чтения не превращается в эффект записи.
func executeClaim(ctx context.Context, client *model.Client4, channel *model.Channel, claim *controlplanev1.IntegrationInvocationClaim, capability integrationpackage.Capability, input operationInput) (*controlplanev1.IntegrationEffectReceipt, error) {
	output, providerRef, attempted, err := executeOperation(ctx, client, channel, claim.GetOperation(), input)
	if err != nil {
		if attempted && !ConfirmedNoEffect(err) {
			return nil, &unknownInvocationError{}
		}
		return nil, err
	}
	body, err := json.Marshal(output)
	if err == nil {
		body, err = capability.ValidateOutput(body)
	}
	if err != nil || len(body) > maximumInvocationOutput || providerRef == "" || len(providerRef) > 256 {
		if attempted {
			return nil, &unknownInvocationError{}
		}
		return nil, errResponse
	}
	digest := sha256.Sum256(body)
	return &controlplanev1.IntegrationEffectReceipt{EffectKey: claim.GetEffectKey(), InputDigest: claim.GetInputDigest(), ProviderEffectRef: providerRef, ResponseDigest: hex.EncodeToString(digest[:]), ResultSummary: string(body)}, nil
}

// TestConnection permits only the pinned package's READ/NONE health operation and validates its output within the smaller health/capability budget.
// RU: TestConnection допускает только health-операцию READ/NONE закреплённого пакета и проверяет результат в меньшем из бюджетов health и capability.
// It uses the working credential/channel path and returns a catalog key rather than provider response content on success.
// RU: Функция использует рабочий путь реквизитов и канала и при успехе возвращает ключ каталога вместо содержимого ответа провайдера.
func (adapter *Adapter) TestConnection(ctx context.Context, claim *controlplanev1.IntegrationConnectionTestClaim) (string, error) {
	definition, err := adapter.claimDefinition(claim.GetDefinitionPackage(), claim.GetDefinitionKey(), claim.GetDefinitionVersion(), claim.GetDefinitionDigest())
	if err != nil {
		return "", errConfiguration
	}
	configuration, err := configurationStrings(claim.GetPublicConfiguration())
	if err != nil || definition.ValidateConfiguration(configuration) != nil {
		return "", errConfiguration
	}
	capability, ok := definition.Capability(definition.Spec.HealthCheck.Operation)
	if !ok || capability.Risk != "READ" || capability.ApprovalPolicy != "NONE" {
		return "", errInvocation
	}
	if _, err := capability.ValidateInput([]byte("{}")); err != nil {
		return "", errInvocation
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(min(capability.Execution.TimeoutSeconds, definition.Spec.HealthCheck.TimeoutSeconds))*time.Second)
	defer cancel()
	client, channel, cleanup, err := adapter.invocationClient(ctx, configuration, claim.GetCredentialRevision())
	if err != nil {
		return "", err
	}
	defer cleanup()
	output, _, _, err := executeOperation(ctx, client, channel, capability.Operation, operationInput{})
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(output)
	if err != nil || len(raw) > maximumInvocationOutput {
		return "", errResponse
	}
	if _, err := capability.ValidateOutput(raw); err != nil {
		return "", errResponse
	}
	return "i18n:INTEGRATION_TEST_SUCCEEDED", nil
}

// claimDefinition validates a bounded Mattermost package against the shipped executable baseline, exact key/version/digest and executor route.
// RU: claimDefinition сверяет ограниченный пакет Mattermost с поставленной исполняемой основой, точными key/version/digest и маршрутом исполнителя.
// A managed revision is returned per claim without replacing the adapter's shared baseline.
// RU: Управляемая ревизия возвращается для конкретного claim без замены общей основы адаптера.
func (adapter *Adapter) claimDefinition(raw []byte, key, version, digest string) (integrationpackage.Package, error) {
	if key != "mattermost" || len(raw) == 0 || len(raw) > 256<<10 {
		return integrationpackage.Package{}, errConfiguration
	}
	definition, err := integrationpackage.Parse(raw)
	if err != nil || integrationpackage.ValidateExecutableRevision(definition, adapter.definition) != nil ||
		definition.Metadata.Key != key || definition.Metadata.Version != version || definition.Digest != digest ||
		!definition.ExecutableBy(integrationpackage.OwnerInteractionGateway, integrationpackage.RouteInteraction) {
		return integrationpackage.Package{}, errConfiguration
	}
	return definition, nil
}

// validateInvocation binds callable operation, risk, approval, effect key, configuration, exact resource scope and canonical input digest to the managed package.
// RU: validateInvocation связывает вызываемую операцию, риск, подтверждение, ключ эффекта, настройки, точную область ресурса и дайджест канонического ввода с управляемым пакетом.
// It validates local claim consistency only; owner authorization and human decisions are not inferred from caller fields.
// RU: Функция проверяет только локальную согласованность claim; owner-полномочия и человеческие решения не выводятся из полей вызывающей стороны.
func (adapter *Adapter) validateInvocation(claim *controlplanev1.IntegrationInvocationClaim) (integrationpackage.Capability, map[string]string, operationInput, error) {
	invalid := func(err error) (integrationpackage.Capability, map[string]string, operationInput, error) {
		return integrationpackage.Capability{}, nil, operationInput{}, err
	}
	definition, err := adapter.claimDefinition(claim.GetDefinitionPackage(), claim.GetDefinitionKey(), claim.GetDefinitionVersion(), claim.GetDefinitionDigest())
	if err != nil {
		return invalid(errConfiguration)
	}
	capability, ok := definition.Capability(claim.GetCapabilityKey())
	if !ok || !capability.CallableByAgent() || capability.Operation != claim.GetOperation() || "INTEGRATION_RISK_"+capability.Risk != claim.GetRisk().String() || "INTEGRATION_APPROVAL_POLICY_"+capability.ApprovalPolicy != claim.GetApprovalPolicy().String() || claim.GetResourceScope().GetKind() != controlplanev1.IntegrationResourceKind_INTEGRATION_RESOURCE_KIND_MATTERMOST_CHANNEL || !boundedReference(claim.GetEffectKey()) {
		return invalid(errInvocation)
	}
	configuration, err := configurationStrings(claim.GetPublicConfiguration())
	if err != nil || definition.ValidateConfiguration(configuration) != nil {
		return invalid(errConfiguration)
	}
	expected, err := capability.ResourceScopeValues(configuration)
	encodedScope, marshalErr := json.Marshal(expected)
	digest := sha256.Sum256(encodedScope)
	if err != nil || marshalErr != nil || !maps.Equal(expected, claim.GetResourceScope().GetValues()) || hex.EncodeToString(digest[:]) != claim.GetResourceScope().GetDigest() {
		return invalid(errInvocation)
	}
	raw, err := json.Marshal(claim.GetBoundedInput().AsMap())
	if err != nil {
		return invalid(errInvocation)
	}
	canonical, err := capability.ValidateInput(raw)
	inputDigest := sha256.Sum256(canonical)
	if err != nil || hex.EncodeToString(inputDigest[:]) != claim.GetInputDigest() {
		return invalid(errInvocation)
	}
	var input operationInput
	if json.Unmarshal(canonical, &input) != nil {
		return invalid(errInvocation)
	}
	return capability, configuration, input, nil
}

// configurationStrings copies only string-valued protobuf configuration fields into a new map, rejecting numbers, nulls and compound values.
// RU: configurationStrings копирует только строковые поля protobuf-настроек в новую карту, отклоняя числа, null и составные значения.
func configurationStrings(value *structpb.Struct) (map[string]string, error) {
	result := map[string]string{}
	for key, raw := range value.GetFields() {
		text, ok := raw.GetKind().(*structpb.Value_StringValue)
		if !ok {
			return nil, errConfiguration
		}
		result[key] = text.StringValue
	}
	return result, nil
}

// invocationClient derives the exact origin/team/channel from validated configuration and opens a scoped client using digest-checked projected credentials.
// RU: invocationClient выводит точные адрес/команду/канал из проверенной конфигурации и создаёт ограниченный клиент со спроецированными реквизитами, проверенными по дайджесту.
// Temporary credential bytes are cleared; the returned cleanup releases the SDK client's token field and idle connections.
// RU: Временные байты реквизитов очищаются; возвращаемый cleanup освобождает поле токена SDK клиента и неактивные соединения.
func (adapter *Adapter) invocationClient(ctx context.Context, configuration map[string]string, credential *controlplanev1.IntegrationCredentialRevision) (*model.Client4, *model.Channel, func(), error) {
	source := &controlplanev1.InteractionSource{BaseUrl: configuration["base_url"], TeamName: configuration["team_name"], ChannelName: configuration["channel_name"]}
	base, err := adapter.baseURL(source.GetBaseUrl())
	if err != nil {
		return nil, nil, func() {}, err
	}
	value, err := adapter.readInvocationCredential(ctx, credential)
	if err != nil {
		return nil, nil, func() {}, err
	}
	defer clear(value)
	client, _, channel, cleanup, err := adapter.authenticatedClient(ctx, source, base, string(value))
	return client, channel, cleanup, err
}

// readInvocationCredential checks descriptor shape and Secret namespace, then waits within the adapter deadline for a safe projected key file.
// RU: readInvocationCredential проверяет форму descriptor и пространство Secret, затем в срок адаптера ждёт безопасный спроецированный файл ключа.
// Returned trimmed bytes must match the declared digest and be bounded and single-line; callers own clearing them after use.
// RU: Возвращаемые обрезанные байты должны совпадать с заявленным дайджестом, быть ограниченными и однострочными; их очисткой после использования владеет вызывающая сторона.
// UID/resource-version fields are shape-checked here, not resolved by a Kubernetes API lookup.
// RU: Поля UID/resource-version здесь проверяются по форме, а не разрешаются запросом Kubernetes API.
func (adapter *Adapter) readInvocationCredential(ctx context.Context, credential *controlplanev1.IntegrationCredentialRevision) ([]byte, error) {
	if credential == nil || credential.GetRef() == "" || credential.GetRevision() < 1 || uuid.Validate(credential.GetSecretUid()) != nil || credential.GetSecretResourceVersion() == "" || len(credential.GetSecretResourceVersion()) > 128 || len(credential.GetContentSha256()) != 64 || !strings.HasPrefix(credential.GetSecretRef(), credentialSecretPrefix) {
		return nil, errCredential
	}
	ctx, cancel := context.WithTimeout(ctx, adapter.timeout)
	defer cancel()
	key := strings.TrimPrefix(credential.GetSecretRef(), credentialSecretPrefix)
	for {
		if ctx.Err() != nil {
			return nil, errCredential
		}
		value, err := adapter.credentials.ReadKey(key)
		if err == nil {
			trimmed := bytes.TrimSpace(value)
			digest := sha256.Sum256(trimmed)
			if len(trimmed) == 0 || len(trimmed) > 16<<10 || bytes.ContainsAny(trimmed, "\r\n") || hex.EncodeToString(digest[:]) != credential.GetContentSha256() {
				clear(value)
				return nil, errCredential
			}
			result := bytes.Clone(trimmed)
			clear(value)
			return result, nil
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, errCredential
		case <-timer.C:
		}
	}
}
