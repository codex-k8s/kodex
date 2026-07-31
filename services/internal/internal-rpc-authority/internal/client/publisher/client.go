package publisher

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"os"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/grpcserver"
	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	domainrepository "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Config задаёт точную mTLS-границу и идентичность publisher.
type Config struct {
	Address               string
	TLSServerName         string
	CACertificateFile     string
	ClientCertificateFile string
	ClientPrivateKeyFile  string
	Timeout               time.Duration
	UnaryInterceptor      grpc.UnaryClientInterceptor
}

// Client вызывает publisher через точную mTLS-границу.
type Client struct {
	timeout    time.Duration
	connection *grpc.ClientConn
	client     internalrpcauthorityv1.RestoreRoleCredentialPublisherServiceClient
}

// New создаёт клиент с обязательной проверкой TLS и SPIFFE.
func New(config Config) (*Client, error) {
	host, _, err := net.SplitHostPort(config.Address)
	if err != nil ||
		host != config.TLSServerName ||
		config.TLSServerName !=
			"internal-rpc-authority-publisher.mattercodex-system.svc" ||
		config.Timeout < time.Second ||
		config.Timeout > 10*time.Second ||
		config.UnaryInterceptor == nil {
		return nil, errors.New("invalid restore publisher client configuration")
	}
	caRaw, err := os.ReadFile(config.CACertificateFile)
	if err != nil {
		return nil, errors.New("read restore publisher CA")
	}
	rootCAs := x509.NewCertPool()
	if !rootCAs.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("restore publisher CA is invalid")
	}
	certificate, err := tls.LoadX509KeyPair(
		config.ClientCertificateFile,
		config.ClientPrivateKeyFile,
	)
	if err != nil {
		return nil, errors.New("load restore controller client certificate")
	}
	tlsConfig := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		RootCAs:      rootCAs,
		ServerName:   config.TLSServerName,
		Certificates: []tls.Certificate{certificate},
	}
	connection, err := grpc.NewClient(
		config.Address,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
		grpc.WithUnaryInterceptor(config.UnaryInterceptor),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(grpcserver.StrictProtoCodec())),
	)
	if err != nil {
		return nil, errors.New("create restore publisher gRPC client")
	}
	connection.Connect()
	return &Client{
		timeout:    config.Timeout,
		connection: connection,
		client: internalrpcauthorityv1.NewRestoreRoleCredentialPublisherServiceClient(
			connection,
		),
	}, nil
}

// PublishRoleCredential запрашивает связанную с целью роль восстановления.
func (client *Client) PublishRoleCredential(
	ctx context.Context,
	directiveCompact string,
	idempotencyKey string,
) (model.RestoreDeliveryRecord, error) {
	callContext, cancel := context.WithTimeout(ctx, client.timeout)
	defer cancel()
	response, err := client.client.PublishRoleCredential(
		callContext,
		&internalrpcauthorityv1.PublishRoleCredentialRequest{
			IssuanceDirectiveCompactJws: directiveCompact,
			IdempotencyKey:              idempotencyKey,
			CorrelationId:               idempotencyKey,
		},
	)
	if err != nil {
		return model.RestoreDeliveryRecord{}, err
	}
	if response.GetDeliveryReceiptCompactJws() == "" ||
		len(response.GetRoleCredentialDigestSha256()) != 64 ||
		response.GetCredentialGeneration() == 0 ||
		response.GetAckKeyGeneration() == 0 {
		return model.RestoreDeliveryRecord{}, errors.New(
			"restore publisher response binding rejected",
		)
	}
	return model.RestoreDeliveryRecord{
		DeliveryReceiptCompactJWS:  response.GetDeliveryReceiptCompactJws(),
		RoleCredentialDigestSHA256: response.GetRoleCredentialDigestSha256(),
		CredentialGeneration:       response.GetCredentialGeneration(),
		ACKKeyGeneration:           response.GetAckKeyGeneration(),
	}, nil
}

// PublisherReady проверяет рабочий путь publisher.
func (client *Client) PublisherReady(ctx context.Context) error {
	callContext, cancel := context.WithTimeout(ctx, client.timeout)
	defer cancel()
	response, err := client.client.CheckReadiness(
		callContext,
		&internalrpcauthorityv1.RestoreRoleCredentialPublisherServiceCheckReadinessRequest{},
	)
	if err != nil {
		return err
	}
	if !response.GetReady() ||
		response.GetTargetRegistryRevision() == 0 ||
		strings.TrimSpace(response.GetTargetRegistryDigestSha256()) == "" ||
		!response.GetVaultExactTargetReadbackReady() {
		return errors.New("restore publisher is not ready")
	}
	return nil
}

// Close закрывает gRPC-соединение.
func (client *Client) Close() error {
	return client.connection.Close()
}

var _ domainrepository.RestoreCredentialPublisher = (*Client)(nil)
