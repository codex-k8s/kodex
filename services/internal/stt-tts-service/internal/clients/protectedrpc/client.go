// Package protectedrpc constructs STT dependency channels and binds owner-authorized continuations under the explicit protected or trusted deployment profile.
// RU: Пакет protectedrpc создаёт каналы зависимостей STT и привязывает разрешённые владельцем continuation в явном защищённом или trusted профиле развёртывания.
package protectedrpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// TargetConfig supplies one dependency target and its protected-profile TLS hostname/CA path, not user-selected request routing.
// RU: TargetConfig задаёт адрес одной зависимости и её TLS hostname/CA путь защищённого профиля, а не выбираемый пользователем маршрут запроса.
type TargetConfig struct {
	Target, TLSServerName, CAFile string
}

// Config selects the closed RPC profile, dependency identities, workload key files and protected continuation issuer without carrying provider credentials.
// RU: Config выбирает закрытый RPC профиль, идентичности зависимостей, workload-файлы ключей и защищённый continuation issuer без реквизитов провайдера.
type Config struct {
	RPCProfile                      string
	Policy, Credential              TargetConfig
	CertificateFile, PrivateKeyFile string
	DialTimeout                     time.Duration
	Issuer                          internalrpcauthorityv1.AuthorizationIssuerServiceClient
}

// Client owns policy/credential channels and typed ports, including owner authority resolution only in the explicit trusted-cluster profile.
// RU: Client владеет каналами политики/реквизитов и типизированными портами, включая разрешение полномочий владельцем только в явном trusted-cluster профиле.
type Client struct {
	Authority          sttv1.TranscriptionAuthorityServiceClient
	trustedCluster     bool
	Policy             sttv1.TranscriptionPolicyProjectionServiceClient
	Credential         sttv1.TranscriptionCredentialProjectionServiceClient
	issuer             internalrpcauthorityv1.AuthorizationIssuerServiceClient
	policy, credential *grpc.ClientConn
}

// Dial validates the selected profile and constructs dependency channels, closing a previously created policy channel if credential setup fails.
// RU: Dial проверяет выбранный профиль и создаёт каналы зависимостей, закрывая созданный канал политики при отказе подготовки реквизитов.
// NewClient is lazy, so successful construction is not proof of a live RPC or user authority; connection checks and calls occur separately.
// RU: NewClient создаётся лениво, поэтому успешное создание не доказывает живой RPC или полномочия пользователя; проверки связи и вызовы выполняются отдельно.
func Dial(ctx context.Context, config Config) (*Client, error) {
	if config.RPCProfile == transportprofile.TrustedCluster {
		return dialTrustedCluster(ctx, config)
	}
	if config.RPCProfile != "" {
		return nil, errors.New("STT RPC profile is unsupported")
	}
	if config.DialTimeout < 100*time.Millisecond || config.DialTimeout > 5*time.Second ||
		!filepath.IsAbs(config.CertificateFile) || !filepath.IsAbs(config.PrivateKeyFile) || config.Issuer == nil {
		return nil, errors.New("STT protected RPC configuration is invalid")
	}
	operations := operationRegistry{
		sttv1.TranscriptionPolicyProjectionService_ResolveTranscriptionPolicy_FullMethodName:         "platform.stt.policy.resolve",
		sttv1.TranscriptionCredentialProjectionService_ProjectTranscriptionCredential_FullMethodName: "platform.stt.credential.project",
	}
	policy, err := dial(config.Policy, config, operations)
	if err != nil {
		return nil, err
	}
	credential, err := dial(config.Credential, config, operations)
	if err != nil {
		_ = policy.Close()
		return nil, err
	}
	return &Client{
		Policy:     sttv1.NewTranscriptionPolicyProjectionServiceClient(policy),
		Credential: sttv1.NewTranscriptionCredentialProjectionServiceClient(credential),
		issuer:     config.Issuer,
		policy:     policy, credential: credential,
	}, nil
}

// dial creates a lazy exact-TLS channel with the registered continuation interceptor and one-MiB message limits, without a blocking connectivity probe.
// RU: dial создаёт ленивый канал точного TLS с зарегистрированным continuation-перехватчиком и лимитами сообщений один MiB без блокирующей проверки связи.
func dial(target TargetConfig, config Config, operations operationRegistry) (*grpc.ClientConn, error) {
	transport, err := transportCredentials(target, config.CertificateFile, config.PrivateKeyFile)
	if err != nil {
		return nil, err
	}
	connection, err := grpc.NewClient(target.Target,
		grpc.WithTransportCredentials(transport),
		grpc.WithChainUnaryInterceptor(authorityclient.ContinuationUnaryClientInterceptor(config.Issuer, operations)),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(1<<20), grpc.MaxCallSendMsgSize(1<<20)),
	)
	if err != nil {
		return nil, errors.New("create STT protected dependency connection")
	}
	return connection, nil
}

// BindDelegated requires the accepted inbound transcription context, exact principal/source pins and a registered child operation before binding continuation.
// RU: BindDelegated требует принятый входящий context распознавания, точные principal/source координаты и зарегистрированную дочернюю операцию до привязки continuation.
// The trusted branch uses private owner-resolved identity and the original incoming credential rather than inventing a protected proof from payload fields.
// RU: Trusted-ветвь использует приватную идентичность, разрешённую владельцем, и исходный входящий реквизит, не создавая защищённый proof из полей payload.
func (client *Client) BindDelegated(
	ctx context.Context,
	principal value.Principal,
	requestID, correlationID, fullMethod, operation string,
) (context.Context, error) {
	if client != nil && client.trustedCluster {
		return bindTrustedDelegated(ctx, principal, requestID, correlationID, fullMethod, operation)
	}
	verified, ok := authorityclient.VerifiedAuthorizationContext(ctx)
	if client == nil || ctx == nil || requestID == "" || correlationID == "" || operationFor(fullMethod) != operation ||
		!ok || verified.GetAuthorityAbiVersion() != internalrpcauth.AuthorityABIVersion ||
		verified.GetOperationId() != "platform.stt.transcribe" ||
		verified.GetFullMethod() != sttv1.SpeechToTextService_Transcribe_FullMethodName ||
		verified.GetRequestBindingMode() != internalrpcauth.RequestBindingStream ||
		verified.GetJti() != requestID || verified.GetPermission() != value.TransportPermissionTranscribe || principal.Permission != value.PermissionTranscribe ||
		verified.GetSourceRevision() != principal.AuthorityRevision ||
		verified.GetSourceDigestSha256() != principal.AuthorityDigestSHA256 ||
		!samePrincipal(verified.GetAuthority(), principal) {
		return nil, errors.New("STT delegated operation is invalid")
	}
	return authorityclient.BindContinuation(ctx, operation, fullMethod, requestID, correlationID)
}

// operationRegistry is the closed method-to-operation mapping consumed by the protected continuation client interceptor.
// RU: operationRegistry задаёт закрытое отображение метода в операцию для защищённого continuation client-перехватчика.
type operationRegistry map[string]string

// OperationID returns only a registered operation and its presence flag; unknown methods receive no fallback capability.
// RU: OperationID возвращает только зарегистрированную операцию и признак наличия; неизвестные методы не получают запасную capability.
func (registry operationRegistry) OperationID(fullMethod string) (string, bool) {
	operation, ok := registry[fullMethod]
	return operation, ok
}

// samePrincipal compares actor, tenant and optional project identity/provenance with the accepted authority, requiring a specified authority actor kind.
// RU: samePrincipal сравнивает идентичность и provenance actor, tenant и необязательного проекта с принятой authority, требуя заданный вид её автора.
func samePrincipal(authority *internalrpcauthorityv1.CallerAuthority, principal value.Principal) bool {
	return authority != nil && authority.GetActorKind() != internalrpcauthorityv1.ActorKind_ACTOR_KIND_UNSPECIFIED &&
		sameIdentity(authority.GetActor(), principal.ActorID, principal.Actor) &&
		sameIdentity(authority.GetTenant(), principal.TenantID, principal.Tenant) &&
		sameIdentity(authority.GetProject(), principal.ProjectID, principal.Project)
}

// sameIdentity requires matching absence or exact identifier plus complete provenance fields without trimming or reading owner state.
// RU: sameIdentity требует совпадающего отсутствия либо точного идентификатора и всех полей происхождения без обрезки строк или чтения состояния владельца.
func sameIdentity(actual *internalrpcauthorityv1.AuthorityIdentity, id string, provenance value.AuthorityProvenance) bool {
	if actual == nil {
		return id == "" && provenance == (value.AuthorityProvenance{})
	}
	return actual != nil && actual.GetProvenance() != nil && actual.GetId() == id &&
		int32(actual.GetProvenance().GetSource()) == provenance.Source &&
		actual.GetProvenance().GetReference() == provenance.Reference &&
		actual.GetProvenance().GetRevision() == provenance.Revision &&
		actual.GetProvenance().GetDigestSha256() == provenance.DigestSHA256
}

// Check verifies protected issuer readiness and the exact authority ABI, or trusted channel connectivity, without proving provider or user-policy availability.
// RU: Check проверяет готовность защищённого issuer и точную authority ABI либо связность trusted-каналов без доказательства доступности провайдера или пользовательской политики.
func (client *Client) Check(ctx context.Context) error {
	if client != nil && client.trustedCluster {
		return client.checkTrustedConnections(ctx)
	}
	if client == nil || client.issuer == nil {
		return errors.New("STT continuation issuer is unavailable")
	}
	response, err := client.issuer.CheckReadiness(ctx, &internalrpcauthorityv1.AuthorizationIssuerServiceCheckReadinessRequest{})
	if err != nil || !response.GetReady() || response.GetAuthorityAbiVersion() != internalrpcauth.AuthorityABIVersion {
		return errors.New("STT continuation issuer is not ready")
	}
	return nil
}

// operationFor resolves only policy and credential projection methods to their exact operation IDs, returning empty for every other method.
// RU: operationFor разрешает только методы проекций политики и реквизитов в точные operation ID, возвращая пустую строку для остальных методов.
func operationFor(fullMethod string) string {
	switch fullMethod {
	case sttv1.TranscriptionPolicyProjectionService_ResolveTranscriptionPolicy_FullMethodName:
		return "platform.stt.policy.resolve"
	case sttv1.TranscriptionCredentialProjectionService_ProjectTranscriptionCredential_FullMethodName:
		return "platform.stt.credential.project"
	default:
		return ""
	}
}

// Close releases credential and policy channels independently and joins their errors; nil or partially constructed clients are safe cleanup inputs.
// RU: Close независимо освобождает каналы реквизитов и политики и объединяет ошибки; nil или частично созданный клиент допустим при очистке.
func (client *Client) Close() error {
	if client == nil {
		return nil
	}
	var result error
	if client.credential != nil {
		result = errors.Join(result, client.credential.Close())
	}
	if client.policy != nil {
		result = errors.Join(result, client.policy.Close())
	}
	return result
}

// transportCredentials loads the workload key pair and accepted CA bytes into TLS 1.3-only credentials with the configured exact server name.
// RU: transportCredentials загружает workload-пару ключей и принимаемые байты CA в TLS 1.3-only реквизиты с заданным точным именем сервера.
// CA size is checked after reading; target syntax here does not independently grant permission or prove network reachability.
// RU: Размер CA проверяется после чтения; синтаксис адреса здесь сам по себе не выдаёт permission и не доказывает сетевую доступность.
func transportCredentials(target TargetConfig, certificateFile, privateKeyFile string) (credentials.TransportCredentials, error) {
	if target.Target == "" || target.TLSServerName == "" || !filepath.IsAbs(target.CAFile) {
		return nil, errors.New("STT dependency target is invalid")
	}
	certificate, err := tls.LoadX509KeyPair(certificateFile, privateKeyFile)
	if err != nil {
		return nil, errors.New("load STT dependency client identity")
	}
	ca, err := os.ReadFile(target.CAFile)
	if err != nil || len(ca) == 0 || len(ca) > 1<<20 {
		return nil, errors.New("load STT dependency CA")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return nil, errors.New("parse STT dependency CA")
	}
	return credentials.NewTLS(&tls.Config{
		MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, ServerName: target.TLSServerName,
		RootCAs: pool, Certificates: []tls.Certificate{certificate},
	}), nil
}
