// Package sttclient composes exact speech RPC routing with the selected transport
// and authority-interceptor profile; recognition remains in the speech service.
// Пакет sttclient связывает exact speech RPC routing с выбранными transport
// и authority-interceptor profile; распознавание остаётся в speech service.
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
	authorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Config supplies the speech-service TLS paths/name, resolver target, local issuer peer IDs, proof provider and dial budget for ordinary mode; the separate trusted constructor consumes only its selected profile fields.
// Config задаёт speech-service TLS paths и name, resolver target, local issuer peer IDs, proof provider и dial budget обычного режима; отдельный trusted constructor использует лишь поля выбранного profile.
type Config struct {
	RPCProfile                                                                 string
	Target, TLSServerName, CAFile, ClientCertificateFile, ClientPrivateKeyFile string
	ExpectedIssuerUID, ExpectedIssuerGID                                       uint32
	DialTimeout                                                                time.Duration
	Proofs                                                                     authorityclient.ProofProvider
}

// Client owns the generated speech client and its gRPC connection plus an optional local authorization issuer. It does not contain a model-provider credential or implement audio recognition itself.
// Client владеет generated speech client, его gRPC connection и необязательным local authorization issuer. Model-provider credential не хранится и audio recognition самостоятельно не выполняется.
type Client struct {
	Speech     sttv1.SpeechToTextServiceClient
	connection *grpc.ClientConn
	issuer     *authorityclient.LocalConnection
}

// operationSet names the exact full-method registry consumed by speech authorization interceptors, without introducing wildcard resolution or model-specific authority.
// operationSet обозначает exact full-method registry для speech authorization interceptors без wildcard resolution и model-specific authority.
type operationSet = authorityclient.MethodOperations

// Dial selects trusted-cluster explicitly or validates ordinary paths/issuer/proofs/budget, loads transport credentials, authenticates the local issuer and creates protected unary/stream speech clients. Connection construction is lazy for the remote target; it does not prove speech readiness or perform transcription.
// Dial явно выбирает trusted-cluster либо проверяет обычные paths, issuer, proofs и budget, загружает transport credentials, аутентифицирует local issuer и создаёт protected unary и stream speech clients. Remote target подключается лениво; speech readiness не доказывается и transcription не выполняется.
func Dial(ctx context.Context, config Config) (*Client, error) {
	if config.RPCProfile != "" {
		return dialTrustedCluster(config)
	}
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
	connection, err := protectedConnection(config.Target, transport, issuer.Issuer(), config.Proofs)
	if err != nil {
		_ = issuer.Close()
		return nil, err
	}
	return &Client{Speech: sttv1.NewSpeechToTextServiceClient(connection), connection: connection, issuer: issuer}, nil
}

// protectedConnection requires transport, issuer and proof dependencies, reverses the exact speech registry and installs both shared unary/stream issuance interceptors with unchanged 65-KiB send and one-MiB receive limits. It opens no local issuer socket itself.
// protectedConnection требует transport, issuer и proof dependencies, обращает exact speech registry и устанавливает общие unary и stream issuance interceptors с прежними send limit 65 КиБ и receive limit один МиБ. Local issuer socket здесь не открывается.
func protectedConnection(target string, transport credentials.TransportCredentials, issuer authorityv1.AuthorizationIssuerServiceClient, proofs authorityclient.ProofProvider) (*grpc.ClientConn, error) {
	if issuer == nil || proofs == nil || transport == nil {
		return nil, errors.New("STT client authority dependencies are missing")
	}
	operations := make(operationSet, len(controlplaneclient.STTGatewayOperations()))
	for operation, method := range controlplaneclient.STTGatewayOperations() {
		operations[method] = operation
	}
	connection, err := grpc.NewClient(
		target,
		grpc.WithTransportCredentials(transport),
		grpc.WithChainUnaryInterceptor(authorityclient.IssuerUnaryClientInterceptor(issuer, operations, proofs)),
		grpc.WithChainStreamInterceptor(authorityclient.IssuerStreamClientInterceptor(issuer, operations, proofs)),
		grpc.WithDefaultCallOptions(grpc.MaxCallSendMsgSize(65<<10), grpc.MaxCallRecvMsgSize(1<<20)),
	)
	if err != nil {
		return nil, errors.New("create STT connection")
	}
	return connection, nil
}

// Close tolerates a nil receiver, otherwise closes the speech connection before an optional issuer and joins errors. A nonnil client is expected to own a nonnil connection; repeated-close behavior is delegated to gRPC.
// Close допускает nil receiver, иначе закрывает speech connection до необязательного issuer и объединяет ошибки. Ненулевой client должен владеть ненулевым connection; repeated-close behavior передаётся gRPC.
func (client *Client) Close() error {
	if client == nil {
		return nil
	}
	result := client.connection.Close()
	if client.issuer != nil {
		result = errors.Join(result, client.issuer.Close())
	}
	return result
}

// transportCredentials reads the CA, parses roots and loads the client certificate/key before constructing exact-name TLS credentials with TLS 1.3 minimum. It does not enforce a CA byte limit, key-file ownership or remote readiness itself.
// transportCredentials читает CA, разбирает roots и загружает client certificate и key до создания exact-name TLS credentials с минимумом TLS 1.3. Собственные CA byte limit, key-file ownership и remote readiness не проверяются.
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
