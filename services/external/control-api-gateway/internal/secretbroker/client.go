package secretbroker

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"path/filepath"
	"time"

	secretbrokerv1 "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type Config struct {
	Target, TLSServerName, CAFile, ClientCertificateFile, ClientPrivateKeyFile string
	DialTimeout, RequestTimeout                                                time.Duration
}

type Client struct {
	SecretBroker secretbrokerv1.SecretBrokerServiceClient
	connection   *grpc.ClientConn
	timeout      time.Duration
}

func Dial(ctx context.Context, config Config) (*Client, error) {
	if config.Target == "" || config.TLSServerName == "" || !filepath.IsAbs(config.CAFile) ||
		!filepath.IsAbs(config.ClientCertificateFile) || !filepath.IsAbs(config.ClientPrivateKeyFile) ||
		config.DialTimeout < time.Second || config.DialTimeout > 10*time.Second ||
		config.RequestTimeout < time.Second || config.RequestTimeout > 10*time.Second {
		return nil, errors.New("secret broker client configuration is invalid")
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
	return &Client{SecretBroker: secretbrokerv1.NewSecretBrokerServiceClient(connection), connection: connection, timeout: config.RequestTimeout}, nil
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
	return client.connection.Close()
}
