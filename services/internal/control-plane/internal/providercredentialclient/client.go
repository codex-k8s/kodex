// Package providercredentialclient adapts protected secret-broker gRPC calls to the control-plane domain port.
// Results contain authorization observations and versioned credential metadata, not raw credential values.
// Пакет providercredentialclient адаптирует защищённые вызовы gRPC secret-broker к доменному порту control-plane.
// Результаты содержат наблюдения авторизации и версионированные метаданные, но не исходные значения учётных данных.
package providercredentialclient

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/google/uuid"
)

const (
	// maximumProviderCredentialRefBytes limits cleanup task and account references to 96 bytes, including their prefixes.
	// maximumProviderCredentialRefBytes ограничивает ссылки задачи очистки и аккаунта 96 байтами вместе с префиксами.
	maximumProviderCredentialRefBytes = 96
	// maximumProviderCredentialResourceVersionBytes caps an opaque Kubernetes resource version at 128 bytes.
	// maximumProviderCredentialResourceVersionBytes ограничивает непрозрачную версию ресурса Kubernetes 128 байтами.
	maximumProviderCredentialResourceVersionBytes = 128
	// maximumProviderCredentialTerminalReceiptBytes bounds returned terminal-receipt text at 512 bytes;
	// the size limit does not verify a receipt signature or schema.
	// maximumProviderCredentialTerminalReceiptBytes ограничивает текст итоговой квитанции 512 байтами;
	// ограничение размера не проверяет подпись или схему квитанции.
	maximumProviderCredentialTerminalReceiptBytes = 512
)

// Client adapts the configured protected provider-credential RPC client to the control-plane domain port.
// Transport identity and authorization remain responsibilities of the underlying client and owning service.
// Client адаптирует настроенный защищённый RPC-клиент учётных данных к доменному порту control-plane.
// Идентичность транспорта и авторизация остаются обязанностью нижележащего клиента и сервиса-владельца.
type Client struct {
	client *controlplaneclient.Client
}

// New rejects a missing shared client or provider-credential stub and otherwise wraps it without dialing.
// New отклоняет отсутствие общего клиента или заглушки RPC учётных данных и иначе оборачивает их без соединения.
func New(client *controlplaneclient.Client) (*Client, error) {
	if client == nil || client.ProviderCredentials == nil {
		return nil, errors.New("provider credential materializer client is required")
	}
	return &Client{client: client}, nil
}

// Check delegates readiness to the configured provider-credential materializer path.
// It does not initiate device authorization or materialize an API key.
// Check передаёт проверку готовности настроенному пути размещения учётных данных провайдера.
// Авторизация устройства и размещение ключа API не запускаются.
func (client *Client) Check(ctx context.Context) error {
	return client.client.CheckProviderCredentialMaterializer(ctx)
}

// StartDeviceAuthorization forwards attempt/account references and maps the broker's device-flow response.
// It requires a valid protobuf expiry timestamp, but does not independently require that expiry to be in the future.
// StartDeviceAuthorization передаёт ссылки попытки и аккаунта и преобразует ответ брокера об авторизации устройства.
// Требуется корректная временная метка protobuf, но её положение в будущем отдельно не проверяется.
func (client *Client) StartDeviceAuthorization(
	ctx context.Context,
	attemptRef, accountRef string,
) (platformservice.ProviderDeviceAuthorizationMaterialization, error) {
	response, err := client.client.ProviderCredentials.StartDeviceAuthorization(ctx,
		&controlplanev1.ProviderCredentialMaterializerServiceStartDeviceAuthorizationRequest{
			AttemptRef: attemptRef, AccountRef: accountRef,
		})
	if err != nil {
		return platformservice.ProviderDeviceAuthorizationMaterialization{}, err
	}
	if response.GetExpiresAt() == nil || response.GetExpiresAt().CheckValid() != nil {
		return platformservice.ProviderDeviceAuthorizationMaterialization{}, errors.New("provider device authorization expiry is invalid")
	}
	return platformservice.ProviderDeviceAuthorizationMaterialization{
		MaterializerAttemptRef: response.GetMaterializerAttemptRef(), VerificationURI: response.GetVerificationUri(),
		UserCode: response.GetUserCode(), ExpiresAt: response.GetExpiresAt().AsTime(),
		MaterializerAttemptUID:     response.GetMaterializerAttemptUid(),
		MaterializerAttemptVersion: response.GetMaterializerAttemptResourceVersion(),
	}, nil
}

// ObserveDeviceAuthorization requests POLL observation for one broker attempt and maps its state and optional descriptor.
// It forwards the broker's masked identity and safe failure code without fetching credential bytes.
// ObserveDeviceAuthorization запрашивает наблюдение POLL для одной попытки брокера и преобразует состояние и необязательный дескриптор.
// Маскированная идентичность и безопасный код ошибки передаются из ответа брокера без чтения байтов учётных данных.
func (client *Client) ObserveDeviceAuthorization(
	ctx context.Context,
	materializerAttemptRef string,
) (platformservice.ProviderAuthorizationObservation, error) {
	response, err := client.client.ProviderCredentials.ObserveDeviceAuthorization(ctx,
		&controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationRequest{
			MaterializerAttemptRef: materializerAttemptRef,
			Mode:                   controlplanev1.ProviderAuthorizationObservationMode_PROVIDER_AUTHORIZATION_OBSERVATION_MODE_POLL,
		})
	if err != nil {
		return platformservice.ProviderAuthorizationObservation{}, err
	}
	return platformservice.ProviderAuthorizationObservation{
		State: providerAuthorizationState(response.GetState()), Credential: credentialDescriptor(response.GetCredential()),
		ExternalAccountMasked: response.GetExternalAccountMasked(), SafeFailureCode: response.GetSafeFailureCode(),
	}, nil
}

// MaterializeAPIKey forwards the supplied key bytes with attempt/account references and requires a returned descriptor.
// It neither clones nor clears the caller's byte slice and does not independently validate every descriptor field here.
// MaterializeAPIKey передаёт байты ключа со ссылками попытки и аккаунта и требует возвращённый дескриптор.
// Срез вызывающего кода здесь не копируется и не очищается; каждое поле дескриптора отдельно не проверяется.
func (client *Client) MaterializeAPIKey(
	ctx context.Context,
	attemptRef, accountRef string,
	apiKey []byte,
) (entity.ProviderCredentialDescriptor, string, error) {
	response, err := client.client.ProviderCredentials.MaterializeAPIKey(ctx,
		&controlplanev1.ProviderCredentialMaterializerServiceMaterializeAPIKeyRequest{
			AttemptRef: attemptRef, AccountRef: accountRef, ApiKey: apiKey,
		})
	if err != nil {
		return entity.ProviderCredentialDescriptor{}, "", err
	}
	descriptor := credentialDescriptor(response.GetCredential())
	if descriptor == nil {
		return entity.ProviderCredentialDescriptor{}, "", errors.New("provider credential descriptor is missing")
	}
	return *descriptor, response.GetExternalAccountMasked(), nil
}

// Discard forwards the exact attempt/account and materializer version pins, including an optional credential descriptor.
// RPC errors or a response without the discarded flag fail the operation; this method does not implement retries.
// Discard передаёт точные ссылки попытки и аккаунта и привязки версии размещения, включая необязательный дескриптор учётных данных.
// Ошибка RPC или ответ без признака discarded означает неуспех; повторов этот метод не выполняет.
func (client *Client) Discard(ctx context.Context, target platformservice.ProviderMaterializationDiscard) error {
	request := &controlplanev1.ProviderCredentialMaterializerServiceDiscardMaterializationRequest{
		AttemptRef: target.AttemptRef, AccountRef: target.AccountRef,
		MaterializerAttemptRef:             target.MaterializerAttemptRef,
		MaterializerAttemptUid:             target.MaterializerAttemptUID,
		MaterializerAttemptResourceVersion: target.MaterializerAttemptVersion,
	}
	if target.Credential != nil {
		request.Credential = &controlplanev1.ProviderCredentialDescriptor{
			SecretName: target.Credential.SecretName, SecretUid: target.Credential.SecretUID,
			SecretResourceVersion: target.Credential.SecretResourceVersion,
			ContentSha256:         target.Credential.ContentSHA256,
		}
	}
	response, err := client.client.ProviderCredentials.DiscardProviderCredentialMaterialization(ctx, request)
	if err != nil {
		return err
	}
	if !response.GetDiscarded() {
		return errors.New("provider credential materialization was not discarded")
	}
	return nil
}

// CleanupProviderCredential validates cleanup references, generation and exact credential metadata before one RPC.
// An optional recovery identity is bound separately; returned receipt text and any produced descriptor are checked before exposing a result.
// CleanupProviderCredential проверяет ссылки очистки, поколение и точные метаданные учётных данных перед одним RPC.
// Необязательная идентичность восстановления привязывается отдельно; текст квитанции и возвращённый дескриптор проверяются до выдачи результата.
func (client *Client) CleanupProviderCredential(
	ctx context.Context,
	taskRef, accountRef string,
	leaseGeneration int64,
	credential entity.ProviderCredentialDescriptor,
	recovery ...*entity.ProviderCleanupRecoveryIdentity,
) (entity.ProviderAuthorizationCleanupResult, error) {
	var result entity.ProviderAuthorizationCleanupResult
	if !validProviderCredentialRef(taskRef, "pcct_") ||
		!validProviderCredentialRef(accountRef, "pacc_") ||
		leaseGeneration < 1 || !validProviderCredentialDescriptor(credential) {
		return result, errors.New("provider credential cleanup request is invalid")
	}
	request := &controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest{
		TaskRef: taskRef, AccountRef: accountRef, LeaseGeneration: leaseGeneration,
		TargetKind: controlplanev1.ProviderCredentialCleanupTargetKind_PROVIDER_CREDENTIAL_CLEANUP_TARGET_KIND_CREDENTIAL,
		Credential: &controlplanev1.ProviderCredentialDescriptor{
			SecretName: credential.SecretName, SecretUid: credential.SecretUID,
			SecretResourceVersion: credential.SecretResourceVersion,
			ContentSha256:         credential.ContentSHA256,
		},
	}
	if len(recovery) > 1 {
		return result, errors.New("provider cleanup recovery identity is invalid")
	}
	if len(recovery) == 1 {
		var err error
		request.RecoveryIdentity, err = cleanupRecoveryIdentity(recovery[0])
		if err != nil {
			return result, err
		}
	}
	response, err := client.client.ProviderCredentials.CleanupProviderCredential(ctx, request)
	if err != nil {
		return result, err
	}
	receipt := response.GetTerminalReceipt()
	result.TerminalReceipt, result.ProducedCredential = receipt, credentialDescriptor(response.GetProducedCredential())
	if !validBoundedSafeText(receipt, maximumProviderCredentialTerminalReceiptBytes) ||
		(result.ProducedCredential != nil && !validProviderCredentialDescriptor(*result.ProducedCredential)) {
		return entity.ProviderAuthorizationCleanupResult{}, errors.New("provider credential cleanup terminal receipt is invalid")
	}
	return result, nil
}

// credentialDescriptor copies the four opaque credential metadata fields, preserving a nil descriptor as nil.
// Conversion alone does not validate the descriptor or reveal credential bytes.
// credentialDescriptor копирует четыре непрозрачных поля метаданных учётных данных, сохраняя nil как nil.
// Само преобразование не проверяет дескриптор и не раскрывает байты учётных данных.
func credentialDescriptor(value *controlplanev1.ProviderCredentialDescriptor) *entity.ProviderCredentialDescriptor {
	if value == nil {
		return nil
	}
	return &entity.ProviderCredentialDescriptor{
		SecretName: value.GetSecretName(), SecretUID: value.GetSecretUid(),
		SecretResourceVersion: value.GetSecretResourceVersion(), ContentSHA256: value.GetContentSha256(),
	}
}

// providerAuthorizationState maps the four supported protobuf authorization states to domain strings;
// unknown or unspecified values produce an empty string rather than an invented state.
// providerAuthorizationState переводит четыре поддержанных состояния авторизации protobuf в доменные строки;
// неизвестные и незаданные значения дают пустую строку, а не придуманное состояние.
func providerAuthorizationState(value controlplanev1.ProviderAuthorizationState) string {
	switch value {
	case controlplanev1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_PENDING:
		return "PENDING"
	case controlplanev1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_AUTHORIZED:
		return "AUTHORIZED"
	case controlplanev1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_EXPIRED:
		return "EXPIRED"
	case controlplanev1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_FAILED:
		return "FAILED"
	default:
		return ""
	}
}

// validProviderCredentialRef requires the supplied prefix, at least eight suffix bytes and a bounded ASCII identifier.
// Letters of either case, digits, underscores and hyphens are accepted; this is not an ownership check.
// validProviderCredentialRef требует заданный префикс, минимум восемь байтов суффикса и ограниченный идентификатор ASCII.
// Принимаются буквы обоих регистров, цифры, подчёркивания и дефисы; владение не проверяется.
func validProviderCredentialRef(value, prefix string) bool {
	if len(value) < len(prefix)+8 || len(value) > maximumProviderCredentialRefBytes || !strings.HasPrefix(value, prefix) {
		return false
	}
	for _, character := range value[len(prefix):] {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') &&
			character != '_' && character != '-' {
			return false
		}
	}
	return true
}

// validProviderCredentialDescriptor checks the Secret label, canonical UUID, bounded resource version and lowercase content digest.
// It validates metadata syntax, not the existence, ownership or current contents of the Secret.
// validProviderCredentialDescriptor проверяет метку Secret, канонический UUID, ограниченную версию ресурса и строчный дайджест.
// Проверяется синтаксис метаданных, а не существование, владение или текущее содержимое Secret.
func validProviderCredentialDescriptor(value entity.ProviderCredentialDescriptor) bool {
	parsedUID, err := uuid.Parse(value.SecretUID)
	return validDNSLabel(value.SecretName) && err == nil && parsedUID.String() == value.SecretUID &&
		validBoundedSafeText(value.SecretResourceVersion, maximumProviderCredentialResourceVersionBytes) &&
		validLowerHexSHA256(value.ContentSHA256)
}

// validDNSLabel validates a returned provider Secret name with the shared bounded ASCII grammar;
// matching this syntax does not validate the descriptor's UID, revision or authority.
// validDNSLabel проверяет возвращённое имя Secret провайдера общей ограниченной ASCII-грамматикой;
// совпадение синтаксиса не проверяет UID, ревизию или полномочия дескриптора.
func validDNSLabel(value string) bool {
	return runtimecontract.ValidDNSLabel(value)
}

// validLowerHexSHA256 accepts exactly 64 lowercase hexadecimal bytes without reading or hashing any resource.
// validLowerHexSHA256 принимает ровно 64 строчных шестнадцатеричных байта без чтения или хеширования ресурса.
func validLowerHexSHA256(value string) bool {
	return runtimecontract.ValidSHA256Text(value)
}

// validBoundedSafeText requires nonempty, trimmed, valid UTF-8 within the byte limit and rejects Unicode control characters.
// The lexical check neither redacts secrets nor validates URL or receipt semantics.
// validBoundedSafeText требует непустой обрезанный корректный UTF-8 в пределах лимита байтов и запрещает управляющие символы Unicode.
// Лексическая проверка не скрывает секреты и не проверяет семантику URL или квитанции.
func validBoundedSafeText(value string, maximumBytes int) bool {
	if value == "" || len(value) > maximumBytes || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
