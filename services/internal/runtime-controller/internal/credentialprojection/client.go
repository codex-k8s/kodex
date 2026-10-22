// Package credentialprojection calls the protected secret-broker materialization boundary
// and checks exact execution-bound projection metadata before runtime Pod construction.
// Пакет credentialprojection вызывает защищённую границу размещения secret-broker
// и проверяет метаданные проекции, привязанные к точному выполнению, перед созданием Pod.
package credentialprojection

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	secretbrokerv1 "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/proto"
)

const (
	// materializeOperation binds ordinary runtime materialization RPCs to their exact application operation.
	// materializeOperation привязывает обычные RPC размещения учётных данных выполнения к точной прикладной операции.
	materializeOperation = "platform.runtime.credentials.materialize"
	// assistantMaterializeOperation keeps system-assistant credential materialization under a distinct application operation.
	// assistantMaterializeOperation отделяет размещение учётных данных системного помощника в самостоятельную прикладную операцию.
	assistantMaterializeOperation = "platform.runtime.credentials.system-assistant.materialize"
	// readinessOperation identifies the protected credential-projection readiness check, not materialization itself.
	// readinessOperation обозначает защищённую проверку готовности проекции учётных данных, а не само размещение.
	readinessOperation = "platform.runtime.credentials.readiness.check"
	// providerAuthKey requires the broker's provider credential to use the runner's fixed authentication filename.
	// providerAuthKey требует фиксированного имени файла авторизации исполнителя для учётных данных провайдера от брокера.
	providerAuthKey = "provider-auth.json"
)

// Config supplies the RPC profile, TLS paths, expected local issuer credentials, timeout and proof provider.
// Paths and expected identities configure a connection; they are not credential contents or a grant of project access.
// Config задаёт профиль RPC, пути TLS, ожидаемые реквизиты локального издателя, таймаут и поставщика доказательств.
// Пути и ожидаемые идентичности настраивают подключение, но не являются содержимым учётных данных или разрешением доступа к проекту.
type Config struct {
	RPCProfile                                                     string
	Target, TLSServerName, CAFile, CertificateFile, PrivateKeyFile string
	ExpectedIssuerUID, ExpectedIssuerGID                           uint32
	DialTimeout                                                    time.Duration
	Proofs                                                         authorityclient.ProofProvider
}

// Projection records the validated Secret location, UID/version/content pins and exact runtime-key mapping.
// It contains metadata only, leaving credential bytes in the broker-managed Secret.
// Projection хранит проверенные размещение Secret, привязки UID/версии/содержимого и точное соответствие ключей выполнения.
// В нём только метаданные; байты учётных данных остаются в Secret под управлением брокера.
type Projection struct {
	Namespace, SecretName, SecretUID, SecretResourceVersion, ContentSHA256 string
	ProviderAuthKey                                                        string
	RuntimeSecretKeys                                                      map[string]string
}

// Client owns the broker RPC stub and, for the ordinary profile, its local issuer and gRPC connections.
// Close releases owned connections; this client does not issue runtime authority itself.
// Client владеет RPC-клиентом брокера и, для обычного профиля, подключениями локального издателя и gRPC.
// Close освобождает принадлежащие соединения; сам клиент не выдаёт полномочия выполнения.
type Client struct {
	api        secretbrokerv1.RuntimeCredentialProjectionServiceClient
	issuer     *authorityclient.LocalConnection
	connection *grpc.ClientConn
}

// operationRegistry maps exact generated full-method names to the closed set of application operations.
// Unknown methods have no fallback operation.
// operationRegistry сопоставляет точные полные имена сгенерированных методов закрытому набору прикладных операций.
// Для неизвестных методов запасной операции нет.
type operationRegistry map[string]string

// OperationID returns the registered application operation and presence flag for an exact method name.
// It performs no prefix matching or authorization decision.
// OperationID возвращает зарегистрированную прикладную операцию и признак наличия для точного имени метода.
// Сравнение по префиксу и решение об авторизации не выполняются.
func (registry operationRegistry) OperationID(fullMethod string) (string, bool) {
	operation, ok := registry[fullMethod]
	return operation, ok
}

// Dial selects the trusted-cluster adapter for a nonempty profile; otherwise it validates settings, loads TLS,
// opens the verified local issuer and constructs a size-bounded broker client with the proof interceptor. It closes the issuer if client construction fails.
// Dial выбирает адаптер доверенного кластера для непустого профиля; иначе проверяет настройки, загружает TLS,
// подключает проверенного локального издателя и создаёт ограниченный по размеру клиент брокера с перехватчиком доказательств. При ошибке создания клиента издатель закрывается.
func Dial(ctx context.Context, config Config) (*Client, error) {
	if config.RPCProfile != "" {
		return dialTrustedCluster(ctx, config)
	}
	if config.Target == "" || config.TLSServerName == "" || !filepath.IsAbs(config.CAFile) ||
		!filepath.IsAbs(config.CertificateFile) || !filepath.IsAbs(config.PrivateKeyFile) || config.Proofs == nil ||
		config.ExpectedIssuerUID == 0 || config.ExpectedIssuerGID == 0 ||
		config.DialTimeout < 100*time.Millisecond || config.DialTimeout > 5*time.Second {
		return nil, errors.New("runtime credential projection client configuration is invalid")
	}
	transport, err := transportCredentials(config)
	if err != nil {
		return nil, err
	}
	issuer, err := authorityclient.DialLocal(ctx, authorityclient.LocalConfig{
		SocketPath: authorityclient.IssuerSocketPath, ExpectedServerUID: config.ExpectedIssuerUID,
		ExpectedServerGID: config.ExpectedIssuerGID, DialTimeout: config.DialTimeout,
	})
	if err != nil {
		return nil, err
	}
	operations := operationRegistry{
		secretbrokerv1.RuntimeCredentialProjectionService_MaterializeRuntimeCredentials_FullMethodName:             materializeOperation,
		secretbrokerv1.RuntimeCredentialProjectionService_MaterializeSystemAssistantCredentials_FullMethodName:     assistantMaterializeOperation,
		secretbrokerv1.RuntimeCredentialProjectionService_CheckRuntimeCredentialProjectionReadiness_FullMethodName: readinessOperation,
	}
	connection, err := grpc.NewClient(config.Target,
		grpc.WithTransportCredentials(transport),
		grpc.WithChainUnaryInterceptor(authorityclient.IssuerUnaryClientInterceptor(issuer.Issuer(), operations, config.Proofs)),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(1<<20), grpc.MaxCallSendMsgSize(1<<20)),
	)
	if err != nil {
		_ = issuer.Close()
		return nil, errors.New("create runtime credential projection connection")
	}
	return &Client{api: secretbrokerv1.NewRuntimeCredentialProjectionServiceClient(connection), issuer: issuer, connection: connection}, nil
}

// Materialize validates turn input and selects a distinct system-assistant RPC or the project-scoped runtime RPC.
// It preserves upstream failure classification and accepts only a descriptor bound to the exact execution; no secret bytes are returned.
// Materialize проверяет вход хода и выбирает отдельный RPC системного помощника либо RPC выполнения в контексте проекта.
// Классификация ошибки вышестоящего сервиса сохраняется; принимается только дескриптор точного выполнения, без возврата байтов секретов.
func (client *Client) Materialize(ctx context.Context, input runtimecontract.RunnerInput) (Projection, error) {
	if client == nil || client.api == nil || input.Mode != runtimecontract.RunnerModeTurn || input.Validate() != nil {
		return Projection{}, errors.New("runtime credential projection input is invalid")
	}
	if input.IsSystemAssistant() {
		response, err := client.api.MaterializeSystemAssistantCredentials(ctx, &secretbrokerv1.MaterializeSystemAssistantCredentialsRequest{Execution: materializeRequest(input)})
		if err != nil {
			return Projection{}, materializationRPCError("materialize assistant credential projection", err)
		}
		return projectionFromDescriptor(input, response.GetProjection())
	}
	projectContext, err := controlplaneclient.WithProjectReference(ctx, input.ProjectRef)
	if err != nil {
		return Projection{}, errors.New("runtime credential projection project is invalid")
	}
	response, err := client.api.MaterializeRuntimeCredentials(projectContext, materializeRequest(input))
	if err != nil {
		return Projection{}, materializationRPCError("materialize runtime credential projection", err)
	}
	return projectionFromDescriptor(input, response.GetProjection())
}

// materializationRPCError adds operation context while wrapping the original cause for errors.Unwrap and gRPC classification.
// It neither retries the request nor replaces the upstream status.
// materializationRPCError добавляет контекст операции, оборачивая исходную причину для errors.Unwrap и классификации gRPC.
// Запрос не повторяется, статус вышестоящего сервиса не подменяется.
func materializationRPCError(operation string, cause error) error {
	return fmt.Errorf("%s: %w", operation, cause)
}

// materializeRequest copies only the workload, lease/fence/generation and immutable revision/session/turn/attempt/input pins
// into the execution locator; project authorization is carried separately, not inferred from this payload.
// materializeRequest копирует только привязки нагрузки, аренды/ограждения/поколения и неизменяемых ревизии/сессии/хода/попытки/входа
// в указатель выполнения; авторизация проекта передаётся отдельно, а не выводится из этого содержимого.
func materializeRequest(input runtimecontract.RunnerInput) *secretbrokerv1.MaterializeRuntimeCredentialsRequest {
	return &secretbrokerv1.MaterializeRuntimeCredentialsRequest{
		WorkloadInstance: input.WorkloadInstance, LeaseRef: input.LeaseRef, Fence: input.LeaseFence,
		Generation: input.LeaseGeneration, RuntimeRevisionRef: input.RuntimeRevisionRef,
		RuntimeRevisionDigest: input.RuntimeRevisionDigest, SessionRef: input.SessionRef, TurnRef: input.TurnRef,
		Attempt: input.Attempt, InputDigest: input.InputDigest,
	}
}

// MaterializationRequestDigest deterministically marshals the exact request envelope and returns its lowercase SHA-256.
// System-assistant input includes its distinct wrapper; the digest does not log contents or independently authorize the request.
// MaterializationRequestDigest детерминированно сериализует точную оболочку запроса и возвращает её строчный SHA-256.
// Вход системного помощника включает отдельную оболочку; дайджест не выводит содержимое в журнал и сам не авторизует запрос.
func MaterializationRequestDigest(input runtimecontract.RunnerInput) (string, error) {
	execution := materializeRequest(input)
	var request proto.Message = execution
	if input.IsSystemAssistant() {
		request = &secretbrokerv1.MaterializeSystemAssistantCredentialsRequest{Execution: execution}
	}
	raw, err := (proto.MarshalOptions{Deterministic: true}).Marshal(request)
	if err != nil {
		return "", errors.New("encode materialization request digest")
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// projectionFromDescriptor rejects missing, expired or cross-execution broker metadata and requires the exact runtime key set.
// It copies accepted keys into a new map and returns only the pinned Secret descriptor, without reading Secret contents.
// projectionFromDescriptor отклоняет отсутствующие, просроченные или относящиеся к другому выполнению метаданные брокера и требует точного набора ключей.
// Принятые ключи копируются в новую карту; возвращается только привязанный дескриптор Secret без чтения его содержимого.
func projectionFromDescriptor(input runtimecontract.RunnerInput, descriptor *secretbrokerv1.RuntimeCredentialProjectionDescriptor) (Projection, error) {
	if descriptor == nil || descriptor.GetNamespace() != "kodex-runtime" || !validDNSLabel(descriptor.GetSecretName()) ||
		descriptor.GetSecretUid() == "" || descriptor.GetSecretResourceVersion() == "" ||
		!validSHA256(descriptor.GetContentSha256()) || descriptor.GetProviderAuthKey() != providerAuthKey ||
		descriptor.GetLeaseRef() != input.LeaseRef || descriptor.GetGeneration() != input.LeaseGeneration ||
		descriptor.GetRuntimeRevisionRef() != input.RuntimeRevisionRef || descriptor.GetRuntimeRevisionDigest() != input.RuntimeRevisionDigest ||
		descriptor.GetSessionRef() != input.SessionRef || descriptor.GetTurnRef() != input.TurnRef ||
		descriptor.GetAttempt() != input.Attempt || descriptor.GetInputDigest() != input.InputDigest ||
		descriptor.GetExpiresAt() == nil || descriptor.GetExpiresAt().CheckValid() != nil || !descriptor.GetExpiresAt().AsTime().After(time.Now()) {
		return Projection{}, errors.New("runtime credential projection descriptor is invalid")
	}
	keys := make(map[string]string, len(descriptor.GetRuntimeSecretKeys()))
	for _, item := range descriptor.GetRuntimeSecretKeys() {
		if item == nil || item.GetName() == "" || item.GetSecretKey() != item.GetName() {
			return Projection{}, errors.New("runtime credential projection key is invalid")
		}
		if _, duplicate := keys[item.GetName()]; duplicate {
			return Projection{}, errors.New("runtime credential projection key is duplicated")
		}
		keys[item.GetName()] = item.GetSecretKey()
	}
	if len(keys) != len(input.SecretProjections) {
		return Projection{}, errors.New("runtime credential projection key set mismatch")
	}
	for _, item := range input.SecretProjections {
		if keys[item.Name] != item.Name {
			return Projection{}, errors.New("runtime credential projection key set mismatch")
		}
	}
	return Projection{
		Namespace: descriptor.GetNamespace(), SecretName: descriptor.GetSecretName(), SecretUID: descriptor.GetSecretUid(),
		SecretResourceVersion: descriptor.GetSecretResourceVersion(), ContentSHA256: descriptor.GetContentSha256(),
		ProviderAuthKey: descriptor.GetProviderAuthKey(), RuntimeSecretKeys: keys,
	}, nil
}

// CheckForProject attaches a validated project reference and requires a successful protected broker readiness response.
// It returns a bounded error for an unavailable client, invalid project, RPC failure or a false ready flag.
// CheckForProject прикрепляет проверенную ссылку проекта и требует успешного защищённого ответа готовности брокера.
// Для отсутствующего клиента, неверного проекта, ошибки RPC или ложного признака готовности возвращается ограниченная ошибка.
func (client *Client) CheckForProject(ctx context.Context, projectRef string) error {
	if client == nil || client.api == nil {
		return errors.New("runtime credential projection client is unavailable")
	}
	projectContext, err := controlplaneclient.WithProjectReference(ctx, projectRef)
	if err != nil {
		return errors.New("runtime credential projection project is invalid")
	}
	response, err := client.api.CheckRuntimeCredentialProjectionReadiness(projectContext, &secretbrokerv1.CheckRuntimeCredentialProjectionReadinessRequest{})
	if err != nil || !response.GetReady() {
		return errors.New("runtime credential projection path is not ready")
	}
	return nil
}

// Close tolerates a nil client, closes any owned gRPC and local issuer connections, and joins their errors.
// It does not revoke a materialized Secret or claim that repeated closes are successful.
// Close допускает nil-клиент, закрывает принадлежащие соединения gRPC и локального издателя и объединяет ошибки.
// Размещённый Secret не отзывается; успешность повторных закрытий не обещается.
func (client *Client) Close() error {
	if client == nil {
		return nil
	}
	var result error
	if client.connection != nil {
		result = errors.Join(result, client.connection.Close())
	}
	if client.issuer != nil {
		result = errors.Join(result, client.issuer.Close())
	}
	return result
}

// transportCredentials loads the client certificate/key and a nonempty CA file capped at one MiB.
// It builds TLS 1.3-only credentials with the exact server name and explicit roots, returning bounded errors without file contents.
// transportCredentials загружает сертификат/ключ клиента и непустой файл CA размером не более одного МиБ.
// Создаются реквизиты только TLS 1.3 с точным именем сервера и явными корнями; ошибки ограничены и не содержат файлы.
func transportCredentials(config Config) (credentials.TransportCredentials, error) {
	certificate, err := tls.LoadX509KeyPair(config.CertificateFile, config.PrivateKeyFile)
	if err != nil {
		return nil, errors.New("load runtime credential projection client identity")
	}
	ca, err := os.ReadFile(config.CAFile)
	if err != nil || len(ca) == 0 || len(ca) > 1<<20 {
		return nil, errors.New("load runtime credential projection CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("parse runtime credential projection CA")
	}
	return credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13,
		ServerName: config.TLSServerName, RootCAs: roots, Certificates: []tls.Certificate{certificate}}), nil
}

// validSHA256 accepts only a 64-byte lowercase hexadecimal digest representation.
// The lexical check does not hash Secret bytes or prove that metadata matches stored content.
// validSHA256 принимает только 64-байтовую строчную шестнадцатеричную запись дайджеста.
// Лексическая проверка не хеширует байты Secret и не доказывает соответствие метаданных сохранённому содержимому.
func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return false
		}
	}
	return true
}

// validDNSLabel preserves this projection client's existing letter-first restriction in addition
// to the shared resource-label grammar; adopting the common parser must not admit leading digits here.
// validDNSLabel сохраняет прежнее требование первой буквы клиента проекции поверх общей грамматики
// меток ресурсов; переход на общий разбор не должен разрешать здесь первую цифру.
func validDNSLabel(value string) bool {
	return value != "" && value[0] >= 'a' && value[0] <= 'z' && runtimecontract.ValidDNSLabel(value)
}
