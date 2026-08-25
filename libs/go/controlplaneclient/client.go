// Package controlplaneclient создаёт mTLS и signed-authority gRPC clients к
// специализированным web-first сервисам control-plane.
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
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

const maximumCredentialBytes = 16 << 10

type applicationGrantContextKey struct{}
type projectReferenceContextKey struct{}

func WithApplicationGrant(ctx context.Context, grant string) (context.Context, error) {
	if ctx == nil || grant == "" || len(grant) > maximumCredentialBytes || strings.TrimSpace(grant) != grant {
		return nil, errors.New("request application grant is invalid")
	}
	return context.WithValue(ctx, applicationGrantContextKey{}, grant), nil
}

func WithProjectReference(ctx context.Context, reference string) (context.Context, error) {
	if ctx == nil || !validOpaqueReference(reference, "prj") {
		return nil, errors.New("request project reference is invalid")
	}
	return context.WithValue(ctx, projectReferenceContextKey{}, reference), nil
}

func validOpaqueReference(reference, prefix string) bool {
	if len(reference) < len(prefix)+9 || len(reference) > 96 || !strings.HasPrefix(reference, prefix+"_") {
		return false
	}
	for _, character := range reference[len(prefix)+1:] {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

type Config struct {
	Target, TLSServerName, CAFile, ClientCertificateFile, ClientPrivateKeyFile string
	ApplicationGrantFile                                                       string
	ExpectedIssuerUID, ExpectedIssuerGID                                       uint32
	DialTimeout                                                                time.Duration
	Operations, ProofOperations                                                map[string]string
	ProjectRequiredOperations                                                  map[string]struct{}
	UnaryClientInterceptor                                                     grpc.UnaryClientInterceptor
}

type Client struct {
	Query           controlplanev1.PlatformQueryServiceClient
	Command         controlplanev1.PlatformCommandServiceClient
	Assistant       controlplanev1.SystemAssistantServiceClient
	Runtime         controlplanev1.RuntimeWorkServiceClient
	Interaction     controlplanev1.InteractionWorkServiceClient
	RoleImages      controlplanev1.RoleImageServiceClient
	resolver        internalrpcauthorityv1.AuthorityProofResolverServiceClient
	issuer          *authorityclient.LocalConnection
	raw, protected  *grpc.ClientConn
	proofOperations map[string]string
	projectRequired map[string]struct{}
	grantFile       string
}

type operationSet map[string]string

func (operations operationSet) OperationID(fullMethod string) (string, bool) {
	value, ok := operations[fullMethod]
	return value, ok
}

func Dial(ctx context.Context, config Config) (*Client, error) {
	if config.Target == "" || config.TLSServerName == "" || !filepath.IsAbs(config.CAFile) ||
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
	transport, err := transportCredentials(config)
	if err != nil {
		return nil, err
	}
	options := []grpc.DialOption{grpc.WithTransportCredentials(transport), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(17<<20), grpc.MaxCallSendMsgSize(17<<20))}
	raw, err := grpc.NewClient(config.Target, options...)
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
	protected, err := grpc.NewClient(config.Target, grpc.WithTransportCredentials(transport), grpc.WithChainUnaryInterceptor(interceptors...), grpc.WithChainStreamInterceptor(authorityclient.IssuerStreamClientInterceptor(issuer.Issuer(), operations, client)), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(17<<20), grpc.MaxCallSendMsgSize(17<<20)))
	if err != nil {
		_ = issuer.Close()
		_ = raw.Close()
		return nil, errors.New("create protected control-plane connection")
	}
	client.protected = protected
	client.Query = controlplanev1.NewPlatformQueryServiceClient(protected)
	client.Command = controlplanev1.NewPlatformCommandServiceClient(protected)
	client.Assistant = controlplanev1.NewSystemAssistantServiceClient(protected)
	client.Runtime = controlplanev1.NewRuntimeWorkServiceClient(protected)
	client.Interaction = controlplanev1.NewInteractionWorkServiceClient(protected)
	client.RoleImages = controlplanev1.NewRoleImageServiceClient(protected)
	return client, nil
}

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

func (client *Client) AuthorityProof(ctx context.Context, operationID, fullMethod string) (string, string, error) {
	if expected, ok := client.proofOperations[fullMethod]; !ok || expected != operationID {
		return "", "", errors.New("control-plane operation is not registered")
	}
	grant, _ := ctx.Value(applicationGrantContextKey{}).(string)
	if grant == "" {
		if client.grantFile == "" {
			return "", "", errors.New("request application credential is missing")
		}
		var err error
		grant, err = readCredential(client.grantFile)
		if err != nil {
			return "", "", err
		}
	}
	correlation := uuid.NewString()
	request := &internalrpcauthorityv1.ResolveAuthorityProofRequest{OperationId: operationID, IdempotencyKey: uuid.NewString(), CorrelationId: correlation}
	if _, required := client.projectRequired[operationID]; required {
		request.ProjectReference, _ = ctx.Value(projectReferenceContextKey{}).(string)
	}
	resolved, err := client.resolver.ResolveAuthorityProof(metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+grant), request)
	if err != nil {
		return "", correlation, err
	}
	if resolved.GetAuthorityProofCompactJws() == "" || resolved.GetProofRevision() == 0 || resolved.GetSignerGeneration() == 0 {
		return "", correlation, errors.New("control-plane authority proof is incomplete")
	}
	return resolved.GetAuthorityProofCompactJws(), correlation, nil
}

func (client *Client) Check(ctx context.Context) error {
	if err := client.CheckLocalAuthority(ctx); err != nil {
		return err
	}
	if _, err := client.resolver.CheckReadiness(ctx, &internalrpcauthorityv1.AuthorityProofResolverServiceCheckReadinessRequest{}); err != nil {
		return errors.New("control-plane proof resolver is not ready")
	}
	response, err := client.Query.GetBootstrapState(ctx, &controlplanev1.GetBootstrapStateRequest{})
	if err != nil || response.GetState() == nil {
		return errors.New("protected control-plane path is not ready")
	}
	return nil
}

// CheckLocalAuthority проверяет только sidecar текущего workload. Этот путь
// допустим для локальной readiness; соседний control-plane проверяется
// отдельной диагностикой и на рабочем запросе.
func (client *Client) CheckLocalAuthority(ctx context.Context) error {
	ready, err := client.issuer.Issuer().CheckReadiness(ctx, &internalrpcauthorityv1.AuthorizationIssuerServiceCheckReadinessRequest{})
	if err != nil || !ready.GetReady() {
		return errors.New("local authority issuer is not ready")
	}
	return nil
}

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

func transportCredentials(config Config) (credentials.TransportCredentials, error) {
	certificate, err := tls.LoadX509KeyPair(config.ClientCertificateFile, config.ClientPrivateKeyFile)
	if err != nil {
		return nil, errors.New("load control-plane client identity")
	}
	ca, err := os.ReadFile(config.CAFile)
	if err != nil || len(ca) == 0 || len(ca) > 1<<20 {
		return nil, errors.New("load control-plane CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("parse control-plane CA")
	}
	return credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, ServerName: config.TLSServerName, RootCAs: roots, Certificates: []tls.Certificate{certificate}}), nil
}
