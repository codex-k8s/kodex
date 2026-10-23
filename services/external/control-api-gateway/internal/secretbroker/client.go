// Package secretbroker composes control API access to raw secret and protected
// draft RPC clients; secret storage and domain eligibility remain in the broker.
// Пакет secretbroker связывает control API с raw secret и protected draft RPC
// clients; хранение секретов и domain eligibility остаются у broker.
package secretbroker

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
	authorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	secretbrokerv1 "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Config supplies explicit secret-broker transport pins, local issuer peer IDs, proof provider and dial/request budgets. Selecting trusted-cluster follows a separate constructor and does not validate or use every ordinary TLS field.
// Config задаёт явные secret-broker transport pins, local issuer peer IDs, proof provider и dial и request budgets. Выбор trusted-cluster использует отдельный constructor, не проверяющий и не использующий все обычные TLS fields.
type Config struct {
	RPCProfile                                                                 string
	Target, TLSServerName, CAFile, ClientCertificateFile, ClientPrivateKeyFile string
	DialTimeout, RequestTimeout                                                time.Duration
	ExpectedIssuerUID, ExpectedIssuerGID                                       uint32
	Proofs                                                                     authorityclient.ProofProvider
}

// Client owns separate raw secret and protected draft connections plus an optional local issuer; trusted-cluster deliberately shares one connection for both generated clients. Readiness uses the raw client with the configured timeout.
// Client владеет отдельными raw secret и protected draft connections и необязательным local issuer; trusted-cluster намеренно использует одно соединение обоих generated clients. Readiness вызывает raw client с настроенным timeout.
type Client struct {
	SecretBroker secretbrokerv1.SecretBrokerServiceClient
	Drafts       secretbrokerv1.SecretBrokerServiceClient
	connection   *grpc.ClientConn
	protected    *grpc.ClientConn
	issuer       *authorityclient.LocalConnection
	timeout      time.Duration
}

// Dial selects the explicit trusted profile or validates ordinary paths/budgets, loads TLS identity and bounded CA, opens raw and authority-protected draft clients and authenticates the local issuer peer. Failure closes already-created connections at the existing points; remote service readiness is not probed by this factory.
// Dial выбирает явный trusted profile либо проверяет обычные paths и budgets, загружает TLS identity и ограниченный CA, создаёт raw и authority-protected draft clients и аутентифицирует local issuer peer. При отказе созданные соединения закрываются в прежних точках; remote service readiness factory не проверяет.
func Dial(ctx context.Context, config Config) (*Client, error) {
	if config.RPCProfile != "" {
		return dialTrustedCluster(config)
	}
	if config.Target == "" || config.TLSServerName == "" || !filepath.IsAbs(config.CAFile) ||
		!filepath.IsAbs(config.ClientCertificateFile) || !filepath.IsAbs(config.ClientPrivateKeyFile) ||
		config.DialTimeout < time.Second || config.DialTimeout > 5*time.Second ||
		config.RequestTimeout < time.Second || config.RequestTimeout > 10*time.Second {
		return nil, errors.New("secret broker client configuration is invalid")
	}
	if config.Proofs == nil || config.ExpectedIssuerUID == 0 || config.ExpectedIssuerGID == 0 {
		return nil, errors.New("secret draft authority configuration is invalid")
	}
	certificate, err := tls.LoadX509KeyPair(config.ClientCertificateFile, config.ClientPrivateKeyFile)
	if err != nil {
		return nil, errors.New("load secret broker client identity")
	}
	rawCA, err := os.ReadFile(config.CAFile)
	if err != nil || len(rawCA) == 0 || len(rawCA) > 1<<20 {
		return nil, errors.New("load secret broker CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(rawCA) {
		return nil, errors.New("parse secret broker CA")
	}
	connection, err := grpc.NewClient(config.Target,
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, ServerName: config.TLSServerName,
			RootCAs: roots, Certificates: []tls.Certificate{certificate},
		})),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(1<<20), grpc.MaxCallSendMsgSize(1<<20)),
	)
	if err != nil {
		return nil, errors.New("create secret broker connection")
	}
	issuer, err := authorityclient.DialLocal(ctx, authorityclient.LocalConfig{SocketPath: authorityclient.IssuerSocketPath, ExpectedServerUID: config.ExpectedIssuerUID, ExpectedServerGID: config.ExpectedIssuerGID, DialTimeout: config.DialTimeout})
	if err != nil {
		_ = connection.Close()
		return nil, err
	}
	protected, err := grpc.NewClient(config.Target,
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, ServerName: config.TLSServerName, RootCAs: roots, Certificates: []tls.Certificate{certificate}})),
		grpc.WithChainUnaryInterceptor(draftInterceptor(issuer.Issuer(), config.Proofs)),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(1<<20), grpc.MaxCallSendMsgSize(1<<20)),
	)
	if err != nil {
		_ = connection.Close()
		_ = issuer.Close()
		return nil, errors.New("create protected secret draft connection")
	}
	return &Client{SecretBroker: secretbrokerv1.NewSecretBrokerServiceClient(connection), Drafts: secretbrokerv1.NewSecretBrokerServiceClient(protected), connection: connection, protected: protected, issuer: issuer, timeout: config.RequestTimeout}, nil
}

// operationSet names this client's exact method-to-operation map; registration validation and authority decisions belong to the surrounding client/proof path, not map lookup.
// operationSet обозначает карту точного method к operation данного client; registration validation и решения о полномочиях принадлежат окружающему client и proof path, а не map lookup.
type operationSet = authorityclient.MethodOperations

// draftInterceptor reverses the closed secret-draft operation registry into exact method lookup and delegates deterministic request binding and context issuance to the shared unary interceptor. Privileged non-draft methods are not registered on this connection.
// draftInterceptor обращает закрытый secret-draft operation registry в exact method lookup и передаёт deterministic request binding и context issuance общему unary interceptor. Привилегированные non-draft methods на этом соединении не регистрируются.
func draftInterceptor(issuer authorityv1.AuthorizationIssuerServiceClient, proofs authorityclient.ProofProvider) grpc.UnaryClientInterceptor {
	operations := operationSet{}
	for operation, method := range controlplaneclient.SecretDraftGatewayOperations() {
		operations[method] = operation
	}
	return authorityclient.IssuerUnaryClientInterceptor(issuer, operations, proofs)
}

// Check calls raw broker CheckReadiness with a child request timeout and converts transport refusal or false readiness to the fixed unavailable error. It does not validate a draft or probe the protected draft connection.
// Check вызывает raw broker CheckReadiness с дочерним request timeout и переводит transport refusal либо false readiness в фиксированную unavailable error. Draft и protected draft connection не проверяются.
func (client *Client) Check(ctx context.Context) error {
	check, cancel := context.WithTimeout(ctx, client.timeout)
	defer cancel()
	result, err := client.SecretBroker.CheckReadiness(check, &secretbrokerv1.CheckReadinessRequest{})
	if err != nil || !result.GetReady() {
		return errors.New("secret broker is unavailable")
	}
	return nil
}

// Close is a no-op for nil/uninitialized primary connection, otherwise closes the raw connection, optional protected connection and issuer in order and joins errors. Trusted-cluster has no separate protected or issuer resource.
// Close ничего не делает при nil либо неинициализированном primary connection, иначе последовательно закрывает raw connection, необязательные protected connection и issuer, объединяя ошибки. Trusted-cluster не имеет отдельных protected и issuer resources.
func (client *Client) Close() error {
	if client == nil || client.connection == nil {
		return nil
	}
	result := client.connection.Close()
	if client.protected != nil {
		result = errors.Join(result, client.protected.Close())
	}
	if client.issuer != nil {
		result = errors.Join(result, client.issuer.Close())
	}
	return result
}
