package authorityclient

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"path/filepath"
	"time"

	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/udscred"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// Канонические UDS-пути и имя метаданных контекста авторизации.
const (
	IssuerSocketPath      = "/run/mattercodex/internal-rpc-authority/issuer.sock"
	VerifierSocketPath    = "/run/mattercodex/internal-rpc-authority/verifier.sock"
	AuthorizationMetadata = "x-mattercodex-authorization"
)

// LocalConfig задаёт проверяемую идентичность локального authority-сервера.
type LocalConfig struct {
	SocketPath        string
	ExpectedServerUID uint32
	ExpectedServerGID uint32
	DialTimeout       time.Duration
}

// LocalConnection владеет gRPC-соединением через проверенный UDS.
type LocalConnection struct {
	connection *grpc.ClientConn
}

// DialLocal подключается к именованному UDS с проверкой uid/gid сервера.
func DialLocal(ctx context.Context, config LocalConfig) (*LocalConnection, error) {
	if err := validateLocalConfig(config); err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: config.DialTimeout}
	connection, err := grpc.NewClient(
		"passthrough:///internal-rpc-authority",
		grpc.WithContextDialer(func(dialContext context.Context, _ string) (net.Conn, error) {
			return dialer.DialContext(dialContext, "unix", config.SocketPath)
		}),
		grpc.WithTransportCredentials(udscred.New(
			config.ExpectedServerUID,
			config.ExpectedServerGID,
		)),
	)
	if err != nil {
		return nil, errors.New("create local authority connection")
	}
	if err := ctx.Err(); err != nil {
		_ = connection.Close()
		return nil, errors.New("local authority connection context canceled")
	}
	connection.Connect()
	return &LocalConnection{connection: connection}, nil
}

// Issuer возвращает клиент локального issuer.
func (connection *LocalConnection) Issuer() internalrpcauthorityv1.AuthorizationIssuerServiceClient {
	return internalrpcauthorityv1.NewAuthorizationIssuerServiceClient(connection.connection)
}

// Verifier возвращает клиент локального verifier.
func (connection *LocalConnection) Verifier() internalrpcauthorityv1.AuthorizationVerifierServiceClient {
	return internalrpcauthorityv1.NewAuthorizationVerifierServiceClient(connection.connection)
}

// Close закрывает локальное gRPC-соединение.
func (connection *LocalConnection) Close() error {
	return connection.connection.Close()
}

func validateLocalConfig(config LocalConfig) error {
	if (config.SocketPath != IssuerSocketPath &&
		config.SocketPath != VerifierSocketPath) ||
		!filepath.IsAbs(config.SocketPath) ||
		config.ExpectedServerUID == 0 ||
		config.ExpectedServerGID == 0 ||
		config.DialTimeout < 100*time.Millisecond ||
		config.DialTimeout > 5*time.Second {
		return errors.New("invalid local authority client configuration")
	}
	info, err := os.Lstat(filepath.Dir(config.SocketPath))
	if err != nil {
		return errors.New("inspect local authority socket directory")
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("local authority socket directory rejected")
	}
	return nil
}

// ProofProvider получает краткоживущее доказательство для точной операции.
type ProofProvider interface {
	AuthorityProof(
		ctx context.Context,
		operationID string,
		fullMethod string,
	) (compactJWS string, correlationID string, err error)
}

// OperationResolver разрешает полный RPC-метод в зарегистрированную операцию.
type OperationResolver interface {
	OperationID(fullMethod string) (string, bool)
}

// IssuerUnaryClientInterceptor выпускает и добавляет контекст перед downstream RPC.
func IssuerUnaryClientInterceptor(
	issuer internalrpcauthorityv1.AuthorizationIssuerServiceClient,
	operations OperationResolver,
	proofs ProofProvider,
) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		request any,
		reply any,
		connection *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		options ...grpc.CallOption,
	) error {
		operationID, ok := operations.OperationID(method)
		if !ok {
			return status.Error(codes.PermissionDenied, "internal RPC operation is not registered")
		}
		proof, correlationID, err := proofs.AuthorityProof(ctx, operationID, method)
		if err != nil {
			return status.Error(codes.Unauthenticated, "authority proof is unavailable")
		}
		issued, err := issuer.IssueAuthorizationContext(
			ctx,
			&internalrpcauthorityv1.IssueAuthorizationContextRequest{
				OperationId:              operationID,
				CorrelationId:            correlationID,
				AuthorityProofCompactJws: proof,
			},
		)
		if err != nil {
			return err
		}
		ctx = metadata.AppendToOutgoingContext(
			ctx,
			AuthorizationMetadata,
			issued.GetCompactJws(),
		)
		return invoker(ctx, method, request, reply, connection, options...)
	}
}

type verifiedContextKey struct{}

// VerifiedAuthorizationContext возвращает проверенный серверным interceptor контекст.
func VerifiedAuthorizationContext(
	ctx context.Context,
) (*internalrpcauthorityv1.VerifiedAuthorizationContext, bool) {
	value, ok := ctx.Value(verifiedContextKey{}).(*internalrpcauthorityv1.VerifiedAuthorizationContext)
	return value, ok && value != nil
}

// VerifierUnaryServerInterceptor проверяет mTLS peer и подписанный контекст.
func VerifierUnaryServerInterceptor(
	verifier internalrpcauthorityv1.AuthorizationVerifierServiceClient,
) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		request any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		transport, err := verifiedMTLSPeer(ctx)
		if err != nil {
			return nil, err
		}
		incoming, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "authorization context required")
		}
		values := incoming.Get(AuthorizationMetadata)
		if len(values) != 1 || values[0] == "" {
			return nil, status.Error(codes.Unauthenticated, "authorization context required")
		}
		verified, err := verifier.VerifyAuthorizationContext(
			ctx,
			&internalrpcauthorityv1.VerifyAuthorizationContextRequest{
				CompactJws:         values[0],
				ObservedFullMethod: info.FullMethod,
				DownstreamPeer:     transport,
			},
		)
		if err != nil {
			return nil, err
		}
		if verified.GetContext() == nil {
			return nil, status.Error(codes.Internal, "verified authorization context missing")
		}
		return handler(
			context.WithValue(ctx, verifiedContextKey{}, verified.GetContext()),
			request,
		)
	}
}

func verifiedMTLSPeer(
	ctx context.Context,
) (*internalrpcauthorityv1.DownstreamTransportPeer, error) {
	value, ok := peer.FromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "mTLS client identity required")
	}
	tlsInfo, ok := value.AuthInfo.(credentials.TLSInfo)
	if !ok ||
		len(tlsInfo.State.VerifiedChains) == 0 ||
		len(tlsInfo.State.PeerCertificates) != 1 {
		return nil, status.Error(codes.Unauthenticated, "mTLS client identity required")
	}
	certificate := tlsInfo.State.PeerCertificates[0]
	spiffeID, err := exactSPIFFEID(certificate)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(certificate.Raw)
	return &internalrpcauthorityv1.DownstreamTransportPeer{
		SpiffeId:          spiffeID,
		CertificateSha256: hex.EncodeToString(digest[:]),
	}, nil
}

func exactSPIFFEID(certificate *x509.Certificate) (string, error) {
	if certificate == nil || len(certificate.URIs) != 1 {
		return "", status.Error(codes.PermissionDenied, "mTLS SPIFFE identity rejected")
	}
	value := certificate.URIs[0]
	if value.Scheme != "spiffe" ||
		value.Host == "" ||
		value.RawQuery != "" ||
		value.Fragment != "" ||
		value.User != nil {
		return "", status.Error(codes.PermissionDenied, "mTLS SPIFFE identity rejected")
	}
	return value.String(), nil
}
