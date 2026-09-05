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

type Config struct {
	Target, TLSServerName, CAFile, ClientCertificateFile, ClientPrivateKeyFile string
	DialTimeout, RequestTimeout                                                time.Duration
	ExpectedIssuerUID, ExpectedIssuerGID                                       uint32
	Proofs                                                                     authorityclient.ProofProvider
}

type Client struct {
	SecretBroker secretbrokerv1.SecretBrokerServiceClient
	Drafts       secretbrokerv1.SecretBrokerServiceClient
	connection   *grpc.ClientConn
	protected    *grpc.ClientConn
	issuer       *authorityclient.LocalConnection
	timeout      time.Duration
}

func Dial(ctx context.Context, config Config) (*Client, error) {
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

type operationSet map[string]string

func draftInterceptor(issuer authorityv1.AuthorizationIssuerServiceClient, proofs authorityclient.ProofProvider) grpc.UnaryClientInterceptor {
	operations := operationSet{}
	for operation, method := range controlplaneclient.SecretDraftGatewayOperations() {
		operations[method] = operation
	}
	return authorityclient.IssuerUnaryClientInterceptor(issuer, operations, proofs)
}

func (operations operationSet) OperationID(method string) (string, bool) {
	operation, ok := operations[method]
	return operation, ok
}

func (client *Client) Check(ctx context.Context) error {
	check, cancel := context.WithTimeout(ctx, client.timeout)
	defer cancel()
	result, err := client.SecretBroker.CheckReadiness(check, &secretbrokerv1.CheckReadinessRequest{})
	if err != nil || !result.GetReady() {
		return errors.New("secret broker is unavailable")
	}
	return nil
}

func (client *Client) Close() error {
	if client == nil || client.connection == nil {
		return nil
	}
	return errors.Join(client.connection.Close(), client.protected.Close(), client.issuer.Close())
}
