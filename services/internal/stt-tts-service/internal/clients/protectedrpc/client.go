// Package protectedrpc собирает exact mTLS и internal authorization context
// для зависимостей STT. Бизнесовые значения не участвуют в выборе профиля.
package protectedrpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

const (
	maximumGrantBytes            = 16 << 10
	invalidProjectReferenceError = "STT project reference is invalid"
)

type TargetConfig struct {
	Target, TLSServerName, CAFile string
}

type Config struct {
	Policy, Credential                   TargetConfig
	Resolver                             TargetConfig
	CertificateFile, PrivateKeyFile      string
	ApplicationGrantFile                 string
	ExpectedIssuerUID, ExpectedIssuerGID uint32
	DialTimeout                          time.Duration
}

type Client struct {
	Policy                  sttv1.TranscriptionPolicyProjectionServiceClient
	Credential              sttv1.TranscriptionCredentialProjectionServiceClient
	resolver                internalrpcauthorityv1.AuthorityProofResolverServiceClient
	issuer                  *authorityclient.LocalConnection
	raw, policy, credential *grpc.ClientConn
	grantFile               string
	operations              operationSet
}

type operationSet map[string]string

type projectReferenceContextKey struct{}

func WithProjectReference(ctx context.Context, reference string) (context.Context, error) {
	if ctx == nil || len(reference) < 12 || len(reference) > 96 || !strings.HasPrefix(reference, "prj_") || strings.TrimSpace(reference) != reference {
		return nil, errors.New(invalidProjectReferenceError)
	}
	for _, character := range reference[4:] {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return nil, errors.New(invalidProjectReferenceError)
	}
	return context.WithValue(ctx, projectReferenceContextKey{}, reference), nil
}

func (set operationSet) OperationID(method string) (string, bool) {
	operation, ok := set[method]
	return operation, ok
}

func Dial(ctx context.Context, config Config) (*Client, error) {
	if ctx == nil || config.DialTimeout < 100*time.Millisecond || config.DialTimeout > 5*time.Second ||
		config.ExpectedIssuerUID == 0 || config.ExpectedIssuerGID == 0 ||
		!filepath.IsAbs(config.CertificateFile) || !filepath.IsAbs(config.PrivateKeyFile) || !filepath.IsAbs(config.ApplicationGrantFile) {
		return nil, errors.New("STT protected RPC configuration is invalid")
	}
	resolverCredentials, err := transportCredentials(config.Resolver, config.CertificateFile, config.PrivateKeyFile)
	if err != nil {
		return nil, err
	}
	raw, err := grpc.NewClient(config.Resolver.Target, grpc.WithTransportCredentials(resolverCredentials))
	if err != nil {
		return nil, errors.New("create STT authority resolver connection")
	}
	issuer, err := authorityclient.DialLocal(ctx, authorityclient.LocalConfig{
		SocketPath: authorityclient.IssuerSocketPath, ExpectedServerUID: config.ExpectedIssuerUID,
		ExpectedServerGID: config.ExpectedIssuerGID, DialTimeout: config.DialTimeout,
	})
	if err != nil {
		_ = raw.Close()
		return nil, err
	}
	operations := operationSet{
		sttv1.TranscriptionPolicyProjectionService_ResolveTranscriptionPolicy_FullMethodName:         "platform.stt.policy.resolve",
		sttv1.TranscriptionPolicyProjectionService_CheckReadiness_FullMethodName:                     "platform.stt.policy.readiness.check",
		sttv1.TranscriptionCredentialProjectionService_ProjectTranscriptionCredential_FullMethodName: "platform.stt.credential.project",
		sttv1.TranscriptionCredentialProjectionService_CheckReadiness_FullMethodName:                 "platform.stt.credential.readiness.check",
	}
	client := &Client{resolver: internalrpcauthorityv1.NewAuthorityProofResolverServiceClient(raw), issuer: issuer, raw: raw, grantFile: config.ApplicationGrantFile, operations: operations}
	policyConnection, err := client.dialProtected(config.Policy, config, operations)
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	client.policy = policyConnection
	credentialConnection, err := client.dialProtected(config.Credential, config, operations)
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	client.credential = credentialConnection
	client.Policy = sttv1.NewTranscriptionPolicyProjectionServiceClient(policyConnection)
	client.Credential = sttv1.NewTranscriptionCredentialProjectionServiceClient(credentialConnection)
	return client, nil
}

func (client *Client) dialProtected(target TargetConfig, config Config, operations operationSet) (*grpc.ClientConn, error) {
	transport, err := transportCredentials(target, config.CertificateFile, config.PrivateKeyFile)
	if err != nil {
		return nil, err
	}
	connection, err := grpc.NewClient(target.Target,
		grpc.WithTransportCredentials(transport),
		grpc.WithChainUnaryInterceptor(authorityclient.IssuerUnaryClientInterceptor(client.issuer.Issuer(), operations, client)),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(1<<20), grpc.MaxCallSendMsgSize(1<<20)),
	)
	if err != nil {
		return nil, errors.New("create STT protected dependency connection")
	}
	return connection, nil
}

func (client *Client) AuthorityProof(ctx context.Context, operationID, fullMethod string) (string, string, error) {
	if expected, ok := client.operations[fullMethod]; !ok || expected != operationID {
		return "", "", errors.New("STT dependency operation is not registered")
	}
	raw, err := securefile.Read(client.grantFile, maximumGrantBytes)
	if err != nil {
		return "", "", errors.New("read STT application grant")
	}
	grant := strings.TrimSpace(string(raw))
	clear(raw)
	if grant == "" || strings.ContainsAny(grant, "\r\n") {
		return "", "", errors.New("STT application grant is invalid")
	}
	correlationID := uuid.NewString()
	response, err := client.resolver.ResolveAuthorityProof(
		metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+grant),
		&internalrpcauthorityv1.ResolveAuthorityProofRequest{
			OperationId: operationID, IdempotencyKey: uuid.NewString(), CorrelationId: correlationID,
			ProjectReference: projectReference(ctx),
		},
	)
	if err != nil {
		return "", correlationID, err
	}
	if response.GetAuthorityProofCompactJws() == "" || response.GetProofRevision() == 0 || response.GetSignerGeneration() == 0 {
		return "", correlationID, errors.New("STT authority proof is incomplete")
	}
	return response.GetAuthorityProofCompactJws(), correlationID, nil
}

func projectReference(ctx context.Context) string {
	reference, _ := ctx.Value(projectReferenceContextKey{}).(string)
	return reference
}

func (client *Client) CheckLocalAuthority(ctx context.Context) error {
	response, err := client.issuer.Issuer().CheckReadiness(ctx, &internalrpcauthorityv1.AuthorizationIssuerServiceCheckReadinessRequest{})
	if err != nil || !response.GetReady() {
		return errors.New("STT local authority issuer is not ready")
	}
	return nil
}

func (client *Client) Check(ctx context.Context) error {
	if err := client.CheckLocalAuthority(ctx); err != nil {
		return err
	}
	response, err := client.resolver.CheckReadiness(ctx, &internalrpcauthorityv1.AuthorityProofResolverServiceCheckReadinessRequest{})
	if err != nil || !response.GetReady() {
		return errors.New("STT authority proof resolver is not ready")
	}
	return nil
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
	if client.issuer != nil {
		result = errors.Join(result, client.issuer.Close())
	}
	if client.raw != nil {
		result = errors.Join(result, client.raw.Close())
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
