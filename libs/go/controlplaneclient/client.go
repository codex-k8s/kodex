// Package controlplaneclient собирает точное доказательство производителя,
// использует локальный issuer и устанавливает mTLS-соединение с control-plane.
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

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/authorityclient"
	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

const (
	maximumCredentialBytes     = 16 << 10
	maximumControlPlaneRPCSize = 9 << 20
)

type applicationGrantContextKey struct{}
type projectReferenceContextKey struct{}

// WithApplicationGrant связывает один уже полученный transport bearer с
// точным RPC. Значение не сохраняется в Client и не попадает в диагностику.
func WithApplicationGrant(ctx context.Context, grant string) (context.Context, error) {
	if ctx == nil || grant == "" || len(grant) > maximumCredentialBytes || strings.TrimSpace(grant) != grant {
		return nil, errors.New("request application grant is invalid")
	}
	return context.WithValue(ctx, applicationGrantContextKey{}, grant), nil
}

// WithProjectReference связывает выбранный project locator с одним запросом.
// Locator не является authority и повторно разрешается control-plane.
func WithProjectReference(ctx context.Context, projectReference string) (context.Context, error) {
	if ctx == nil || uuid.Validate(projectReference) != nil {
		return nil, errors.New("request project reference is invalid")
	}
	return context.WithValue(ctx, projectReferenceContextKey{}, projectReference), nil
}

type Config struct {
	Target                    string
	TLSServerName             string
	CAFile                    string
	ClientCertificateFile     string
	ClientPrivateKeyFile      string
	ApplicationGrantFile      string
	ExpectedIssuerUID         uint32
	ExpectedIssuerGID         uint32
	DialTimeout               time.Duration
	Operations                map[string]string
	ProofOperations           map[string]string
	ProjectRequiredOperations map[string]struct{}
	UnaryClientInterceptor    grpc.UnaryClientInterceptor
}

type Client struct {
	ControlPlane    controlplanev1.ControlPlaneServiceClient
	resolver        internalrpcauthorityv1.AuthorityProofResolverServiceClient
	issuer          *authorityclient.LocalConnection
	raw             *grpc.ClientConn
	protected       *grpc.ClientConn
	operations      operationSet
	proofOperations operationSet
	projectRequired map[string]struct{}
	grantFile       string
}

type operationSet map[string]string

func (operations operationSet) OperationID(fullMethod string) (string, bool) {
	value, ok := operations[fullMethod]
	return value, ok
}

func Dial(ctx context.Context, config Config) (*Client, error) {
	if config.Target == "" || config.TLSServerName == "" ||
		!filepath.IsAbs(config.CAFile) ||
		!filepath.IsAbs(config.ClientCertificateFile) ||
		!filepath.IsAbs(config.ClientPrivateKeyFile) ||
		!filepath.IsAbs(config.ApplicationGrantFile) ||
		config.ExpectedIssuerUID == 0 || config.ExpectedIssuerGID == 0 ||
		config.DialTimeout < 100*time.Millisecond ||
		config.DialTimeout > 5*time.Second || len(config.Operations) == 0 {
		return nil, errors.New("control-plane client configuration is invalid")
	}
	operations, err := validateOperations(config.Operations)
	if err != nil {
		return nil, err
	}
	proofConfig := config.ProofOperations
	if len(proofConfig) == 0 {
		proofConfig = config.Operations
	}
	proofOperations, err := validateOperations(proofConfig)
	if err != nil {
		return nil, err
	}
	projectRequired := make(map[string]struct{}, len(config.ProjectRequiredOperations))
	for operationID := range config.ProjectRequiredOperations {
		if _, registered := proofConfig[operationID]; !registered {
			return nil, errors.New("control-plane project operation is not registered")
		}
		projectRequired[operationID] = struct{}{}
	}
	transport, err := transportCredentials(config)
	if err != nil {
		return nil, err
	}
	// Защищённое соединение использует узкий набор control-plane RPC, а resolver
	// дополнительно принимает owner RPC interaction и integration gateways.
	rawOptions := []grpc.DialOption{
		grpc.WithTransportCredentials(transport),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(maximumControlPlaneRPCSize),
			grpc.MaxCallSendMsgSize(maximumControlPlaneRPCSize),
		),
	}
	if config.UnaryClientInterceptor != nil {
		rawOptions = append(rawOptions, grpc.WithUnaryInterceptor(config.UnaryClientInterceptor))
	}
	raw, err := grpc.NewClient(config.Target, rawOptions...)
	if err != nil {
		return nil, errors.New("create control-plane resolver connection")
	}
	issuer, err := authorityclient.DialLocal(ctx, authorityclient.LocalConfig{
		SocketPath:        authorityclient.IssuerSocketPath,
		ExpectedServerUID: config.ExpectedIssuerUID,
		ExpectedServerGID: config.ExpectedIssuerGID,
		DialTimeout:       config.DialTimeout,
	})
	if err != nil {
		_ = raw.Close()
		return nil, err
	}
	client := &Client{
		resolver:        internalrpcauthorityv1.NewAuthorityProofResolverServiceClient(raw),
		issuer:          issuer,
		raw:             raw,
		operations:      operations,
		proofOperations: proofOperations,
		projectRequired: projectRequired,
		grantFile:       config.ApplicationGrantFile,
	}
	interceptors := []grpc.UnaryClientInterceptor{authorityclient.IssuerUnaryClientInterceptor(
		issuer.Issuer(),
		operations,
		client,
	)}
	if config.UnaryClientInterceptor != nil {
		interceptors = append(interceptors, config.UnaryClientInterceptor)
	}
	protected, err := grpc.NewClient(
		config.Target,
		grpc.WithTransportCredentials(transport),
		grpc.WithChainUnaryInterceptor(interceptors...),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(maximumControlPlaneRPCSize),
			grpc.MaxCallSendMsgSize(maximumControlPlaneRPCSize),
		),
	)
	if err != nil {
		_ = issuer.Close()
		_ = raw.Close()
		return nil, errors.New("create protected control-plane connection")
	}
	client.protected = protected
	client.ControlPlane = controlplanev1.NewControlPlaneServiceClient(protected)
	return client, nil
}

func validateOperations(config map[string]string) (operationSet, error) {
	operations := make(operationSet, len(config))
	for operationID, fullMethod := range config {
		if operationID == "" || fullMethod == "" {
			return nil, errors.New("control-plane client operation is invalid")
		}
		if _, duplicate := operations[fullMethod]; duplicate {
			return nil, errors.New("control-plane client operation is duplicated")
		}
		operations[fullMethod] = operationID
	}
	return operations, nil
}

func (client *Client) AuthorityProof(
	ctx context.Context,
	operationID string,
	fullMethod string,
) (string, string, error) {
	expectedOperation, ok := client.proofOperations[fullMethod]
	if !ok || expectedOperation != operationID {
		return "", "", errors.New("control-plane operation is not registered")
	}
	grant, _ := ctx.Value(applicationGrantContextKey{}).(string)
	if grant == "" {
		var err error
		grant, err = readCredential(client.grantFile)
		if err != nil {
			return "", "", err
		}
	}
	correlationID := uuid.NewString()
	requestContext := metadata.AppendToOutgoingContext(
		ctx,
		"authorization",
		"Bearer "+grant,
	)
	request := &internalrpcauthorityv1.ResolveAuthorityProofRequest{
		OperationId:    operationID,
		IdempotencyKey: uuid.NewString(),
		CorrelationId:  correlationID,
	}
	if _, required := client.projectRequired[operationID]; required {
		request.ProjectReference, _ = ctx.Value(projectReferenceContextKey{}).(string)
	}
	resolved, err := client.resolver.ResolveAuthorityProof(
		requestContext,
		request,
	)
	if err != nil {
		return "", "", err
	}
	if resolved.GetAuthorityProofCompactJws() == "" ||
		resolved.GetProofRevision() == 0 ||
		resolved.GetSignerGeneration() == 0 {
		return "", "", errors.New("control-plane authority proof is incomplete")
	}
	return resolved.GetAuthorityProofCompactJws(), correlationID, nil
}

func (client *Client) Check(ctx context.Context) error {
	if _, err := client.resolver.CheckReadiness(
		ctx,
		&internalrpcauthorityv1.AuthorityProofResolverServiceCheckReadinessRequest{},
	); err != nil {
		return errors.New("control-plane proof resolver is not ready")
	}
	issued, err := client.issuer.Issuer().CheckReadiness(
		ctx,
		&internalrpcauthorityv1.AuthorizationIssuerServiceCheckReadinessRequest{},
	)
	if err != nil || !issued.GetReady() {
		return errors.New("local authority issuer is not ready")
	}
	checked, err := client.ControlPlane.CheckReadiness(
		ctx,
		&controlplanev1.CheckReadinessRequest{},
	)
	if err != nil || !checked.GetReady() || !checked.GetAuthorityReady() {
		return errors.New("protected control-plane path is not ready")
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
	if client.raw != nil {
		result = errors.Join(result, client.raw.Close())
	}
	if client.issuer != nil {
		result = errors.Join(result, client.issuer.Close())
	}
	return result
}

func transportCredentials(config Config) (credentials.TransportCredentials, error) {
	certificate, err := tls.LoadX509KeyPair(
		config.ClientCertificateFile,
		config.ClientPrivateKeyFile,
	)
	if err != nil {
		return nil, errors.New("load control-plane client identity")
	}
	caRaw, err := os.ReadFile(config.CAFile)
	if err != nil || len(caRaw) == 0 || len(caRaw) > 1<<20 {
		return nil, errors.New("read control-plane CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("parse control-plane CA")
	}
	return credentials.NewTLS(&tls.Config{
		MinVersion:   tls.VersionTLS13,
		ServerName:   config.TLSServerName,
		RootCAs:      roots,
		Certificates: []tls.Certificate{certificate},
	}), nil
}

func readCredential(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 ||
		info.Size() > maximumCredentialBytes || info.Mode().Perm()&0o007 != 0 {
		return "", errors.New("application grant file is unsafe")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", errors.New("read application grant")
	}
	grant := strings.TrimSpace(string(raw))
	if grant == "" || len(grant) > maximumCredentialBytes {
		return "", errors.New("application grant is invalid")
	}
	return grant, nil
}
