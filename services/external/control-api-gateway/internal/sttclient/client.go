// Package sttclient подключает control API к защищённому streaming STT RPC.
package sttclient

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
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type Config struct {
	Target, TLSServerName, CAFile, ClientCertificateFile, ClientPrivateKeyFile string
	ExpectedIssuerUID, ExpectedIssuerGID                                       uint32
	DialTimeout                                                                time.Duration
	Proofs                                                                     authorityclient.ProofProvider
}

type Client struct {
	Speech     sttv1.SpeechToTextServiceClient
	connection *grpc.ClientConn
	issuer     *authorityclient.LocalConnection
}

type operationSet map[string]string

func (operations operationSet) OperationID(method string) (string, bool) {
	operation, ok := operations[method]
	return operation, ok
}

func Dial(ctx context.Context, config Config) (*Client, error) {
	if config.Target == "" || config.TLSServerName == "" || !filepath.IsAbs(config.CAFile) ||
		!filepath.IsAbs(config.ClientCertificateFile) || !filepath.IsAbs(config.ClientPrivateKeyFile) ||
		config.ExpectedIssuerUID == 0 || config.ExpectedIssuerGID == 0 || config.Proofs == nil ||
		config.DialTimeout < 100*time.Millisecond || config.DialTimeout > 5*time.Second {
		return nil, errors.New("STT client configuration is invalid")
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
	operations := make(operationSet, len(controlplaneclient.STTGatewayOperations()))
	for operation, method := range controlplaneclient.STTGatewayOperations() {
		operations[method] = operation
	}
	connection, err := grpc.NewClient(
		config.Target,
		grpc.WithTransportCredentials(transport),
		grpc.WithChainUnaryInterceptor(authorityclient.IssuerUnaryClientInterceptor(issuer.Issuer(), operations, config.Proofs)),
		grpc.WithChainStreamInterceptor(authorityclient.IssuerStreamClientInterceptor(issuer.Issuer(), operations, config.Proofs)),
		grpc.WithDefaultCallOptions(grpc.MaxCallSendMsgSize(65<<10), grpc.MaxCallRecvMsgSize(1<<20)),
	)
	if err != nil {
		_ = issuer.Close()
		return nil, errors.New("create STT connection")
	}
	return &Client{Speech: sttv1.NewSpeechToTextServiceClient(connection), connection: connection, issuer: issuer}, nil
}

func (client *Client) Close() error {
	if client == nil {
		return nil
	}
	return errors.Join(client.connection.Close(), client.issuer.Close())
}

func transportCredentials(config Config) (credentials.TransportCredentials, error) {
	caPEM, err := os.ReadFile(config.CAFile)
	if err != nil {
		return nil, errors.New("read STT CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("parse STT CA")
	}
	certificate, err := tls.LoadX509KeyPair(config.ClientCertificateFile, config.ClientPrivateKeyFile)
	if err != nil {
		return nil, errors.New("load STT client certificate")
	}
	return credentials.NewTLS(&tls.Config{
		MinVersion: tls.VersionTLS13, ServerName: config.TLSServerName,
		RootCAs: roots, Certificates: []tls.Certificate{certificate},
	}), nil
}
