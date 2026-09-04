// Package protectedrpc создаёт exact mTLS-соединения и server-owned
// continuation для зависимостей STT.
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
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type TargetConfig struct {
	Target, TLSServerName, CAFile string
}

type Config struct {
	Policy, Credential              TargetConfig
	CertificateFile, PrivateKeyFile string
	DialTimeout                     time.Duration
	Issuer                          internalrpcauthorityv1.AuthorizationIssuerServiceClient
}

type Client struct {
	Policy             sttv1.TranscriptionPolicyProjectionServiceClient
	Credential         sttv1.TranscriptionCredentialProjectionServiceClient
	issuer             internalrpcauthorityv1.AuthorizationIssuerServiceClient
	policy, credential *grpc.ClientConn
}

func Dial(_ context.Context, config Config) (*Client, error) {
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

// BindDelegated связывает child RPC только с принятым входным STT context.
func (client *Client) BindDelegated(
	ctx context.Context,
	principal value.Principal,
	requestID, correlationID, fullMethod, operation string,
) (context.Context, error) {
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

type operationRegistry map[string]string

func (registry operationRegistry) OperationID(fullMethod string) (string, bool) {
	operation, ok := registry[fullMethod]
	return operation, ok
}

func samePrincipal(authority *internalrpcauthorityv1.CallerAuthority, principal value.Principal) bool {
	return authority != nil && authority.GetActorKind() != internalrpcauthorityv1.ActorKind_ACTOR_KIND_UNSPECIFIED &&
		sameIdentity(authority.GetActor(), principal.ActorID, principal.Actor) &&
		sameIdentity(authority.GetTenant(), principal.TenantID, principal.Tenant) &&
		sameIdentity(authority.GetProject(), principal.ProjectID, principal.Project)
}

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

// Check подтверждает, что issuer обслуживает ту же ABI, которую использует клиент.
func (client *Client) Check(ctx context.Context) error {
	if client == nil || client.issuer == nil {
		return errors.New("STT continuation issuer is unavailable")
	}
	response, err := client.issuer.CheckReadiness(ctx, &internalrpcauthorityv1.AuthorizationIssuerServiceCheckReadinessRequest{})
	if err != nil || !response.GetReady() || response.GetAuthorityAbiVersion() != internalrpcauth.AuthorityABIVersion {
		return errors.New("STT continuation issuer is not ready")
	}
	return nil
}

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
