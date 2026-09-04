// Package credentialprojection вызывает защищённую materialization boundary
// secret-broker и проверяет exact descriptor до создания Runtime Pod.
package credentialprojection

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	secretbrokerv1 "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const (
	materializeOperation = "platform.runtime.credentials.materialize"
	readinessOperation   = "platform.runtime.credentials.readiness.check"
	providerAuthKey      = "provider-auth.json"
)

type Config struct {
	Target, TLSServerName, CAFile, CertificateFile, PrivateKeyFile string
	ExpectedIssuerUID, ExpectedIssuerGID                           uint32
	DialTimeout                                                    time.Duration
	Proofs                                                         authorityclient.ProofProvider
}

type Projection struct {
	Namespace, SecretName, SecretUID, SecretResourceVersion, ContentSHA256 string
	ProviderAuthKey                                                        string
	RuntimeSecretKeys                                                      map[string]string
}

type Client struct {
	api        secretbrokerv1.RuntimeCredentialProjectionServiceClient
	issuer     *authorityclient.LocalConnection
	connection *grpc.ClientConn
}

type operationRegistry map[string]string

func (registry operationRegistry) OperationID(fullMethod string) (string, bool) {
	operation, ok := registry[fullMethod]
	return operation, ok
}

func Dial(ctx context.Context, config Config) (*Client, error) {
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

func (client *Client) Materialize(ctx context.Context, input runtimecontract.RunnerInput) (Projection, error) {
	if client == nil || client.api == nil || input.Mode != runtimecontract.RunnerModeTurn || input.Validate() != nil {
		return Projection{}, errors.New("runtime credential projection input is invalid")
	}
	projectContext, err := controlplaneclient.WithProjectReference(ctx, input.ProjectRef)
	if err != nil {
		return Projection{}, errors.New("runtime credential projection project is invalid")
	}
	response, err := client.api.MaterializeRuntimeCredentials(projectContext, materializeRequest(input))
	if err != nil {
		return Projection{}, errors.New("materialize runtime credential projection")
	}
	return projectionFromDescriptor(input, response.GetProjection())
}

func materializeRequest(input runtimecontract.RunnerInput) *secretbrokerv1.MaterializeRuntimeCredentialsRequest {
	return &secretbrokerv1.MaterializeRuntimeCredentialsRequest{
		WorkloadInstance: input.WorkloadInstance, LeaseRef: input.LeaseRef, Fence: input.LeaseFence,
		Generation: input.LeaseGeneration, RuntimeRevisionRef: input.RuntimeRevisionRef,
		RuntimeRevisionDigest: input.RuntimeRevisionDigest, SessionRef: input.SessionRef, TurnRef: input.TurnRef,
		Attempt: input.Attempt, InputDigest: input.InputDigest,
	}
}

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

func validDNSLabel(value string) bool {
	if len(value) == 0 || len(value) > 63 || value[0] < 'a' || value[0] > 'z' && (value[0] < '0' || value[0] > '9') {
		return false
	}
	for index, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' && index > 0 && index < len(value)-1 {
			continue
		}
		return false
	}
	return true
}
