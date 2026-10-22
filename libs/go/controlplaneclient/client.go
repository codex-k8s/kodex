// Package controlplaneclient composes generated control-plane clients with the
// selected transport, operation registry and authority/service-identity boundary.
// Domain ownership and command semantics remain with the remote services.
// Пакет controlplaneclient связывает generated control-plane clients с выбранными
// transport, operation registry и authority/service-identity boundary.
// Доменное владение и семантика команд остаются у удалённых сервисов.
package controlplaneclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/opaqueref"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

// maximumCredentialBytes bounds an application grant read from a file or request.
// maximumCredentialBytes ограничивает размер гранта приложения из файла или запроса.
const maximumCredentialBytes = 16 << 10

// maximumProtectedResponseBytes bounds responses on the protected connection.
// maximumProtectedResponseBytes ограничивает ответы защищённого соединения.
const maximumProtectedResponseBytes = 33 << 20

// applicationGrantContextKey isolates the request grant from unrelated context values.
// applicationGrantContextKey отделяет грант запроса от иных значений контекста.
type applicationGrantContextKey struct{}

// projectReferenceContextKey isolates the validated project reference in context.
// projectReferenceContextKey отделяет проверенную ссылку на проект в контексте.
type projectReferenceContextKey struct{}

// WithApplicationGrant attaches a bounded grant to one outgoing request context.
// WithApplicationGrant добавляет ограниченный по размеру грант в контекст одного запроса.
func WithApplicationGrant(ctx context.Context, grant string) (context.Context, error) {
	if ctx == nil || grant == "" || len(grant) > maximumCredentialBytes || strings.TrimSpace(grant) != grant {
		return nil, errors.New("request application grant is invalid")
	}
	return context.WithValue(ctx, applicationGrantContextKey{}, grant), nil
}

// WithProjectReference attaches a syntactically valid project reference to a request.
// WithProjectReference добавляет синтаксически допустимую ссылку на проект к запросу.
func WithProjectReference(ctx context.Context, reference string) (context.Context, error) {
	if ctx == nil || !validOpaqueReference(reference, "prj") {
		return nil, errors.New("request project reference is invalid")
	}
	return context.WithValue(ctx, projectReferenceContextKey{}, reference), nil
}

// validOpaqueReference checks the bounded opaque identifier syntax for a prefix.
// validOpaqueReference проверяет ограниченный формат непрозрачного идентификатора.
func validOpaqueReference(reference, prefix string) bool {
	return opaqueref.Valid(reference, prefix)
}

// Config defines the protected connection, proof resolver, and operation mapping.
// Config задаёт защищённое соединение, разрешение доказательств и карту операций.
type Config struct {
	RPCProfile, CallerWorkload                                                 string
	ServiceIdentity                                                            bool
	Target, TLSServerName, CAFile, ClientCertificateFile, ClientPrivateKeyFile string
	ResolverTarget, ResolverTLSServerName, ResolverCAFile                      string
	ApplicationGrantFile                                                       string
	ExpectedIssuerUID, ExpectedIssuerGID                                       uint32
	DialTimeout                                                                time.Duration
	Operations, ProofOperations                                                map[string]string
	ProjectRequiredOperations                                                  map[string]struct{}
	UnaryClientInterceptor                                                     grpc.UnaryClientInterceptor
}

// Client owns generated service clients and their authority-bearing connections.
// Client владеет типизированными клиентами сервисов и их полномочными соединениями.
type Client struct {
	trustedCluster          bool
	serviceIdentity         bool
	Query                   controlplanev1.PlatformQueryServiceClient
	Command                 controlplanev1.PlatformCommandServiceClient
	Assistant               controlplanev1.SystemAssistantServiceClient
	Runtime                 controlplanev1.RuntimeWorkServiceClient
	RuntimeSecrets          controlplanev1.RuntimeSecretWorkServiceClient
	RuntimeSecretDrafts     controlplanev1.RuntimeSecretDraftWorkServiceClient
	ConfigurationSources    controlplanev1.ManagedConfigurationSourceWorkServiceClient
	ConfigurationWriteBacks controlplanev1.ManagedConfigurationGitWriteBackWorkServiceClient
	SessionArchive          controlplanev1.SessionArchiveWorkServiceClient
	Interaction             controlplanev1.InteractionWorkServiceClient
	RoleImages              controlplanev1.RoleImageServiceClient
	Access                  controlplanev1.AccessServiceClient
	ProviderCredentials     controlplanev1.ProviderCredentialMaterializerServiceClient
	ProviderRenewals        controlplanev1.ProviderCredentialRenewalWorkServiceClient
	resolver                internalrpcauthorityv1.AuthorityProofResolverServiceClient
	issuer                  *authorityclient.LocalConnection
	raw, protected          *grpc.ClientConn
	proofOperations         map[string]string
	projectRequired         map[string]struct{}
	grantFile               string
}

// operationSet names the shared exact-method map in this client's signatures;
// validateOperations still owns nonempty/duplicate registry checks locally.
// operationSet обозначает общую exact-method карту в сигнатурах этого client;
// проверки nonempty и duplicate registry остаются у local validateOperations.
type operationSet = authorityclient.MethodOperations

// Dial builds the configured protected control-plane client and proof path.
// Dial создаёт настроенный защищённый клиент control-plane и путь доказательства.
func Dial(ctx context.Context, config Config) (*Client, error) {
	if config.RPCProfile != "" {
		return dialTrustedCluster(ctx, config)
	}
	if config.ServiceIdentity {
		return DialServiceIdentity(ctx, config)
	}
	if config.ResolverTarget == "" {
		config.ResolverTarget = config.Target
	}
	if config.ResolverTLSServerName == "" {
		config.ResolverTLSServerName = config.TLSServerName
	}
	if config.ResolverCAFile == "" {
		config.ResolverCAFile = config.CAFile
	}
	if config.Target == "" || config.TLSServerName == "" || !filepath.IsAbs(config.CAFile) ||
		config.ResolverTarget == "" || config.ResolverTLSServerName == "" || !filepath.IsAbs(config.ResolverCAFile) ||
		!filepath.IsAbs(config.ClientCertificateFile) || !filepath.IsAbs(config.ClientPrivateKeyFile) ||
		config.ApplicationGrantFile != "" && !filepath.IsAbs(config.ApplicationGrantFile) ||
		config.ExpectedIssuerUID == 0 || config.ExpectedIssuerGID == 0 ||
		config.DialTimeout < 100*time.Millisecond || config.DialTimeout > 5*time.Second || len(config.Operations) == 0 {
		return nil, errors.New("control-plane client configuration is invalid")
	}
	operations, err := validateOperations(config.Operations)
	if err != nil {
		return nil, err
	}
	proofSource := config.ProofOperations
	if len(proofSource) == 0 {
		proofSource = config.Operations
	}
	proofOperations, err := validateOperations(proofSource)
	if err != nil {
		return nil, err
	}
	transport, err := transportCredentials(config.TLSServerName, config.CAFile, config.ClientCertificateFile, config.ClientPrivateKeyFile)
	if err != nil {
		return nil, err
	}
	resolverTransport, err := transportCredentials(config.ResolverTLSServerName, config.ResolverCAFile, config.ClientCertificateFile, config.ClientPrivateKeyFile)
	if err != nil {
		return nil, err
	}
	options := []grpc.DialOption{grpc.WithTransportCredentials(resolverTransport), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(17<<20), grpc.MaxCallSendMsgSize(17<<20))}
	raw, err := grpc.NewClient(config.ResolverTarget, options...)
	if err != nil {
		return nil, errors.New("create control-plane resolver connection")
	}
	issuer, err := authorityclient.DialLocal(ctx, authorityclient.LocalConfig{SocketPath: authorityclient.IssuerSocketPath, ExpectedServerUID: config.ExpectedIssuerUID, ExpectedServerGID: config.ExpectedIssuerGID, DialTimeout: config.DialTimeout})
	if err != nil {
		_ = raw.Close()
		return nil, err
	}
	client := &Client{resolver: internalrpcauthorityv1.NewAuthorityProofResolverServiceClient(raw), issuer: issuer, raw: raw, proofOperations: proofOperations, projectRequired: config.ProjectRequiredOperations, grantFile: config.ApplicationGrantFile}
	interceptors := []grpc.UnaryClientInterceptor{authorityclient.IssuerUnaryClientInterceptor(issuer.Issuer(), operations, client)}
	if config.UnaryClientInterceptor != nil {
		interceptors = append(interceptors, config.UnaryClientInterceptor)
	}
	protected, err := grpc.NewClient(config.Target, grpc.WithTransportCredentials(transport), grpc.WithChainUnaryInterceptor(interceptors...), grpc.WithChainStreamInterceptor(authorityclient.IssuerStreamClientInterceptor(issuer.Issuer(), operations, client, controlplanev1.RuntimeWorkService_StreamExecutionArtifact_FullMethodName)), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maximumProtectedResponseBytes), grpc.MaxCallSendMsgSize(17<<20)))
	if err != nil {
		_ = issuer.Close()
		_ = raw.Close()
		return nil, errors.New("create protected control-plane connection")
	}
	client.bindServices(protected)
	return client, nil
}

// bindServices binds each distinct generated service client to the same configured
// protected connection. Explicit typed constructors preserve the separate service
// APIs and avoid an untyped registry or a second connection for one capability.
// bindServices привязывает отдельные generated-клиенты к одному настроенному
// protected соединению. Явные конструкторы сохраняют типы разных сервисов,
// не вводя нетипизированный реестр или второе соединение для одной capability.
func (client *Client) bindServices(protected *grpc.ClientConn) {
	client.protected = protected
	// jscpd:ignore-start
	// Intentional typed constructor wiring: each assignment exposes a different
	// generated service interface, so these calls are not interchangeable clones.
	// Намеренно явные конструкторы разных generated-интерфейсов; эти вызовы
	// не взаимозаменяемы и не являются повтором одной исполняемой операции.
	client.Query = controlplanev1.NewPlatformQueryServiceClient(protected)
	client.Command = controlplanev1.NewPlatformCommandServiceClient(protected)
	client.Assistant = controlplanev1.NewSystemAssistantServiceClient(protected)
	client.Runtime = controlplanev1.NewRuntimeWorkServiceClient(protected)
	client.RuntimeSecrets = controlplanev1.NewRuntimeSecretWorkServiceClient(protected)
	client.RuntimeSecretDrafts = controlplanev1.NewRuntimeSecretDraftWorkServiceClient(protected)
	client.ConfigurationSources = controlplanev1.NewManagedConfigurationSourceWorkServiceClient(protected)
	client.ConfigurationWriteBacks = controlplanev1.NewManagedConfigurationGitWriteBackWorkServiceClient(protected)
	client.SessionArchive = controlplanev1.NewSessionArchiveWorkServiceClient(protected)
	client.Interaction = controlplanev1.NewInteractionWorkServiceClient(protected)
	client.RoleImages = controlplanev1.NewRoleImageServiceClient(protected)
	client.Access = controlplanev1.NewAccessServiceClient(protected)
	client.ProviderCredentials = controlplanev1.NewProviderCredentialMaterializerServiceClient(protected)
	client.ProviderRenewals = controlplanev1.NewProviderCredentialRenewalWorkServiceClient(protected)
	// jscpd:ignore-end
}

// validateOperations rejects empty or duplicate method registrations.
// validateOperations отклоняет пустые и повторно зарегистрированные методы.
func validateOperations(source map[string]string) (operationSet, error) {
	result := make(operationSet, len(source))
	for operation, method := range source {
		if operation == "" || method == "" {
			return nil, errors.New("control-plane client operation is invalid")
		}
		if _, exists := result[method]; exists {
			return nil, errors.New("control-plane client operation is duplicated")
		}
		result[method] = operation
	}
	return result, nil
}

// AuthorityProof resolves a signed proof for the exact registered operation and request.
// AuthorityProof получает подписанное доказательство для точной операции и запроса.
func (client *Client) AuthorityProof(ctx context.Context, operationID, fullMethod string) (string, string, error) {
	if client.trustedCluster {
		return "", "", errors.New("authority proof is unavailable in trusted cluster profile")
	}
	if expected, ok := client.proofOperations[fullMethod]; !ok || expected != operationID {
		return "", "", authorityclient.NewProofFailure(authorityclient.StageProofOperation, errors.New("control-plane operation is not registered"))
	}
	grant, _ := ctx.Value(applicationGrantContextKey{}).(string)
	if grant == "" {
		if client.grantFile == "" {
			return "", "", authorityclient.NewProofFailure(authorityclient.StageGrantMissing, errors.New("request application credential is missing"))
		}
		var err error
		grant, err = readCredential(client.grantFile)
		if err != nil {
			return "", "", authorityclient.NewProofFailure(authorityclient.StageGrantRead, err)
		}
	}
	correlation := uuid.NewString()
	request := &internalrpcauthorityv1.ResolveAuthorityProofRequest{OperationId: operationID, IdempotencyKey: uuid.NewString(), CorrelationId: correlation}
	request.RequestDigestSha256, _ = authorityclient.RequestDigest(ctx)
	if _, required := client.projectRequired[operationID]; required {
		request.ProjectReference, _ = ctx.Value(projectReferenceContextKey{}).(string)
	}
	resolved, err := client.resolver.ResolveAuthorityProof(metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+grant), request)
	if err != nil {
		return "", correlation, authorityclient.NewProofFailure(authorityclient.StageProofResolve, err)
	}
	if resolved.GetAuthorityProofCompactJws() == "" || resolved.GetProofRevision() == 0 || resolved.GetSignerGeneration() == 0 {
		return "", correlation, authorityclient.NewProofFailure(authorityclient.StageProofResponse, errors.New("control-plane authority proof is incomplete"))
	}
	return resolved.GetAuthorityProofCompactJws(), correlation, nil
}

// Check verifies the local authority path and an actual protected bootstrap read.
// Check проверяет локальный путь полномочий и реальное защищённое чтение bootstrap.
func (client *Client) Check(ctx context.Context) error {
	if !client.serviceIdentity {
		if err := client.CheckLocalAuthority(ctx); err != nil {
			return err
		}
		if _, err := client.resolver.CheckReadiness(ctx, &internalrpcauthorityv1.AuthorityProofResolverServiceCheckReadinessRequest{}); err != nil {
			return errors.New("control-plane proof resolver is not ready")
		}
	}
	response, err := client.Query.GetBootstrapState(ctx, &controlplanev1.GetBootstrapStateRequest{})
	if err != nil || response.GetState() == nil {
		return errors.New("protected control-plane path is not ready")
	}
	return nil
}

// CheckProviderCredentialMaterializer checks the credential materializer work path.
// CheckProviderCredentialMaterializer проверяет рабочий путь материализации учётных данных.
func (client *Client) CheckProviderCredentialMaterializer(ctx context.Context) error {
	if err := client.CheckLocalAuthority(ctx); err != nil {
		return err
	}
	if !client.trustedCluster {
		ready, err := client.resolver.CheckReadiness(ctx, &internalrpcauthorityv1.AuthorityProofResolverServiceCheckReadinessRequest{})
		if err != nil || !ready.GetReady() {
			return errors.New("control-plane proof resolver is not ready")
		}
	}
	response, err := client.ProviderCredentials.CheckProviderCredentialMaterializerReadiness(
		ctx,
		&controlplanev1.CheckProviderCredentialMaterializerReadinessRequest{},
	)
	if err != nil || !response.GetReady() {
		return errors.New("provider credential materializer is not ready")
	}
	return nil
}

// CheckLocalAuthority checks this workload's issuer sidecar, not remote services.
// CheckLocalAuthority проверяет sidecar текущей нагрузки, но не соседние сервисы.
func (client *Client) CheckLocalAuthority(ctx context.Context) error {
	if client.serviceIdentity {
		return nil
	}
	ready, err := client.issuer.Issuer().CheckReadiness(ctx, &internalrpcauthorityv1.AuthorizationIssuerServiceCheckReadinessRequest{})
	if err != nil || !ready.GetReady() {
		return errors.New("local authority issuer is not ready")
	}
	return nil
}

// Close releases each connection owned by the client and combines close errors.
// Close освобождает все соединения клиента и объединяет ошибки закрытия.
func (client *Client) Close() error {
	if client == nil {
		return nil
	}
	var result error
	if client.protected != nil {
		result = errors.Join(result, client.protected.Close())
	}
	if client.issuer != nil {
		result = errors.Join(result, client.issuer.Close())
	}
	if client.raw != nil {
		result = errors.Join(result, client.raw.Close())
	}
	return result
}

// readCredential reads one bounded file credential and rejects blank or multiline data.
// readCredential читает ограниченный файловый грант и отвергает пустые и многострочные данные.
func readCredential(path string) (string, error) {
	value, err := os.ReadFile(path)
	if err != nil || len(value) == 0 || len(value) > maximumCredentialBytes {
		return "", errors.New("read application credential")
	}
	result := strings.TrimSpace(string(value))
	for index := range value {
		value[index] = 0
	}
	if result == "" || strings.ContainsAny(result, "\r\n") {
		return "", errors.New("application credential is invalid")
	}
	return result, nil
}

// transportCredentials loads a client identity and pins TLS to the specified CA and name.
// transportCredentials загружает удостоверение клиента и привязывает TLS к CA и имени.
func transportCredentials(serverName, caFile, certificateFile, privateKeyFile string) (credentials.TransportCredentials, error) {
	certificate, err := tls.LoadX509KeyPair(certificateFile, privateKeyFile)
	if err != nil {
		return nil, errors.New("load control-plane client identity")
	}
	ca, err := os.ReadFile(caFile)
	if err != nil || len(ca) == 0 || len(ca) > 1<<20 {
		return nil, errors.New("load control-plane CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("parse control-plane CA")
	}
	return credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, ServerName: serverName, RootCAs: roots, Certificates: []tls.Certificate{certificate}}), nil
}
