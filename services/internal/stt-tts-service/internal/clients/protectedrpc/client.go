// Package protectedrpc создаёт только exact mTLS-соединения к зависимостям.
// До материализации server-owned continuation primitive в #1023 каждый
// projection-вызов закрыто отклоняется до обращения к сети.
package protectedrpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"path/filepath"
	"time"

	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/errs"
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
}

type Client struct {
	Policy             sttv1.TranscriptionPolicyProjectionServiceClient
	Credential         sttv1.TranscriptionCredentialProjectionServiceClient
	policy, credential *grpc.ClientConn
}

func Dial(_ context.Context, config Config) (*Client, error) {
	if config.DialTimeout < 100*time.Millisecond || config.DialTimeout > 5*time.Second ||
		!filepath.IsAbs(config.CertificateFile) || !filepath.IsAbs(config.PrivateKeyFile) {
		return nil, errors.New("STT protected RPC configuration is invalid")
	}
	policy, err := dial(config.Policy, config)
	if err != nil {
		return nil, err
	}
	credential, err := dial(config.Credential, config)
	if err != nil {
		_ = policy.Close()
		return nil, err
	}
	return &Client{
		Policy:     sttv1.NewTranscriptionPolicyProjectionServiceClient(policy),
		Credential: sttv1.NewTranscriptionCredentialProjectionServiceClient(credential),
		policy:     policy, credential: credential,
	}, nil
}

func dial(target TargetConfig, config Config) (*grpc.ClientConn, error) {
	transport, err := transportCredentials(target, config.CertificateFile, config.PrivateKeyFile)
	if err != nil {
		return nil, err
	}
	connection, err := grpc.NewClient(target.Target,
		grpc.WithTransportCredentials(transport),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(1<<20), grpc.MaxCallSendMsgSize(1<<20)),
	)
	if err != nil {
		return nil, errors.New("create STT protected dependency connection")
	}
	return connection, nil
}

// BindDelegated валидирует exact operation registry и закрыто отказывает до
// RPC: существующий internalrpcauth поддерживает первичный proof, но не
// server-owned continuation входного root actor.
func (client *Client) BindDelegated(
	ctx context.Context,
	_ value.Principal,
	requestID, correlationID, fullMethod, operation string,
) (context.Context, error) {
	if ctx == nil || requestID == "" || correlationID == "" || operationFor(fullMethod) != operation {
		return nil, errors.New("STT delegated operation is invalid")
	}
	return nil, errs.ErrDelegatedProofPending
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
