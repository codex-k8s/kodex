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
	"strings"
	"time"

	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/udscred"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Канонические UDS-пути и имя метаданных контекста авторизации.
const (
	IssuerSocketPath      = "/run/kodex/internal-rpc-authority/issuer.sock"
	VerifierSocketPath    = "/run/kodex/internal-rpc-authority/verifier.sock"
	AuthorizationMetadata = "x-kodex-authorization"
	proofRetryTimeout     = 500 * time.Millisecond
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
	for {
		state := connection.GetState()
		if state == connectivity.Ready {
			break
		}
		if state == connectivity.Shutdown {
			_ = connection.Close()
			return nil, errors.New("local authority connection shut down before readiness")
		}
		connection.Connect()
		if !connection.WaitForStateChange(ctx, state) {
			_ = connection.Close()
			return nil, errors.New("local authority connection context ended before readiness")
		}
	}
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

type requestDigestKey struct{}

// RequestDigest возвращает digest фактического unary protobuf request,
// вычисленный interceptor до обращения к proof resolver.
func RequestDigest(ctx context.Context) (string, bool) {
	value, ok := ctx.Value(requestDigestKey{}).(string)
	return value, ok && value != ""
}

func bindRequestDigest(ctx context.Context, request any) (context.Context, string, error) {
	message, ok := request.(proto.Message)
	if !ok || message == nil {
		return nil, "", status.Error(codes.InvalidArgument, "protobuf request is required")
	}
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(message)
	if err != nil {
		return nil, "", status.Error(codes.InvalidArgument, "protobuf request encoding failed")
	}
	digest := sha256.Sum256(raw)
	value := hex.EncodeToString(digest[:])
	return context.WithValue(ctx, requestDigestKey{}, value), value, nil
}

type continuationSource struct {
	compact  string
	verified *internalrpcauthorityv1.VerifiedAuthorizationContext
}

type continuationSourceKey struct{}
type continuationRequestKey struct{}

type continuationRequest struct {
	operationID, fullMethod, requestID, correlationID string
}

// BindContinuation разрешает child issuance только из контекста, созданного
// verifier interceptor после durable acceptance parent JTI.
func BindContinuation(
	ctx context.Context,
	operationID, fullMethod, requestID, correlationID string,
) (context.Context, error) {
	if ctx == nil || operationID == "" || fullMethod == "" || requestID == "" ||
		correlationID == "" || len(operationID) > 128 || len(fullMethod) > 256 ||
		len(requestID) > 128 || len(correlationID) > 128 ||
		strings.TrimSpace(requestID) != requestID || strings.TrimSpace(correlationID) != correlationID {
		return nil, status.Error(codes.InvalidArgument, "continuation request is invalid")
	}
	source, ok := ctx.Value(continuationSourceKey{}).(continuationSource)
	if !ok || source.compact == "" || source.verified == nil {
		return nil, status.Error(codes.FailedPrecondition, "verified parent authorization context required")
	}
	return context.WithValue(ctx, continuationRequestKey{}, continuationRequest{
		operationID: operationID, fullMethod: fullMethod,
		requestID: requestID, correlationID: correlationID,
	}), nil
}

// ContinuationUnaryClientInterceptor выпускает exact child context в момент,
// когда доступен фактический protobuf request.
func ContinuationUnaryClientInterceptor(
	issuer internalrpcauthorityv1.AuthorizationIssuerServiceClient,
	operations OperationResolver,
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
		pending, pendingOK := ctx.Value(continuationRequestKey{}).(continuationRequest)
		source, sourceOK := ctx.Value(continuationSourceKey{}).(continuationSource)
		if !ok || !pendingOK || !sourceOK || pending.operationID != operationID || pending.fullMethod != method {
			return status.Error(codes.PermissionDenied, "internal RPC continuation is not registered")
		}
		_, requestDigest, err := bindRequestDigest(ctx, request)
		if err != nil {
			return err
		}
		issued, err := issuer.IssueContinuationAuthorizationContext(ctx,
			&internalrpcauthorityv1.IssueContinuationAuthorizationContextRequest{
				OperationId: operationID, ParentAuthorizationContextCompactJws: source.compact,
				RequestId: pending.requestID, CorrelationId: pending.correlationID,
				RequestDigestSha256: requestDigest,
			})
		if err != nil {
			return newLocalAuthorityError(err, pending.correlationID)
		}
		return invoker(metadata.AppendToOutgoingContext(ctx, AuthorizationMetadata, issued.GetCompactJws()), method, request, reply, connection, options...)
	}
}

// LocalAuthorityError отличает сбой локальной выдачи контекста от ошибки
// downstream RPC. Внешняя граница может безопасно нормализовать только этот
// доверенный тип, не принимая произвольный bare gRPC status за доменный ответ.
type LocalAuthorityError struct {
	code          codes.Code
	correlationID string
}

func (failure *LocalAuthorityError) Error() string {
	return failure.GRPCStatus().Err().Error()
}

// GRPCStatus сохраняет классификацию для стандартных gRPC helpers.
func (failure *LocalAuthorityError) GRPCStatus() *status.Status {
	message := "authority proof is unavailable"
	switch failure.code {
	case codes.Canceled:
		message = "authority dependency request canceled"
	case codes.Unavailable:
		message = "authority dependency is unavailable"
	case codes.DeadlineExceeded:
		message = "authority dependency deadline exceeded"
	}
	return status.New(failure.code, message)
}

// CorrelationID возвращает идентификатор exact proof request без раскрытия
// credential или внутренней причины отказа.
func (failure *LocalAuthorityError) CorrelationID() string {
	return failure.correlationID
}

func newLocalAuthorityError(err error, correlationID string) error {
	code := authorityFailureCode(err)
	switch code {
	case codes.Canceled, codes.Unavailable, codes.DeadlineExceeded:
	default:
		code = codes.Unauthenticated
	}
	return &LocalAuthorityError{code: code, correlationID: correlationID}
}

func authorityFailureCode(err error) codes.Code {
	switch {
	case errors.Is(err, context.Canceled):
		return codes.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return codes.DeadlineExceeded
	default:
		return status.Code(err)
	}
}

func authorityProofWithRetry(
	ctx context.Context,
	proofs ProofProvider,
	operationID, method string,
) (string, string, error) {
	proof, correlationID, err := proofs.AuthorityProof(ctx, operationID, method)
	if err == nil || !retryableProofFailure(ctx, err) {
		return proof, correlationID, err
	}
	retryCtx, cancel := context.WithTimeout(ctx, proofRetryTimeout)
	defer cancel()
	return proofs.AuthorityProof(retryCtx, operationID, method)
}

func retryableProofFailure(ctx context.Context, err error) bool {
	if ctx == nil || ctx.Err() != nil {
		return false
	}
	switch authorityFailureCode(err) {
	case codes.Canceled, codes.DeadlineExceeded, codes.Unavailable:
		return true
	default:
		return false
	}
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
		var requestDigest string
		var err error
		ctx, requestDigest, err = bindRequestDigest(ctx, request)
		if err != nil {
			return err
		}
		operationID, ok := operations.OperationID(method)
		if !ok {
			return status.Error(codes.PermissionDenied, "internal RPC operation is not registered")
		}
		proof, correlationID, err := authorityProofWithRetry(ctx, proofs, operationID, method)
		if err != nil {
			return newLocalAuthorityError(err, correlationID)
		}
		issued, err := issuer.IssueAuthorizationContext(
			ctx,
			&internalrpcauthorityv1.IssueAuthorizationContextRequest{
				OperationId:              operationID,
				CorrelationId:            correlationID,
				AuthorityProofCompactJws: proof,
				RequestDigestSha256:      requestDigest,
			},
		)
		if err != nil {
			return newLocalAuthorityError(err, correlationID)
		}
		ctx = metadata.AppendToOutgoingContext(
			ctx,
			AuthorizationMetadata,
			issued.GetCompactJws(),
		)
		return invoker(ctx, method, request, reply, connection, options...)
	}
}

// IssuerStreamClientInterceptor выпускает тот же exact-method контекст до
// открытия streaming RPC. Один контекст связывает всю ограниченную сессию
// upload/download; повторное открытие требует нового proof и проходит replay
// protection проверяющей стороны.
func IssuerStreamClientInterceptor(
	issuer internalrpcauthorityv1.AuthorizationIssuerServiceClient,
	operations OperationResolver,
	proofs ProofProvider,
) grpc.StreamClientInterceptor {
	return func(
		ctx context.Context,
		description *grpc.StreamDesc,
		connection *grpc.ClientConn,
		method string,
		streamer grpc.Streamer,
		options ...grpc.CallOption,
	) (grpc.ClientStream, error) {
		operationID, ok := operations.OperationID(method)
		if !ok {
			return nil, status.Error(codes.PermissionDenied, "internal RPC operation is not registered")
		}
		proof, correlationID, err := authorityProofWithRetry(ctx, proofs, operationID, method)
		if err != nil {
			return nil, newLocalAuthorityError(err, correlationID)
		}
		issued, err := issuer.IssueAuthorizationContext(ctx, &internalrpcauthorityv1.IssueAuthorizationContextRequest{
			OperationId: operationID, CorrelationId: correlationID, AuthorityProofCompactJws: proof,
		})
		if err != nil {
			return nil, newLocalAuthorityError(err, correlationID)
		}
		ctx = metadata.AppendToOutgoingContext(ctx, AuthorizationMetadata, issued.GetCompactJws())
		return streamer(ctx, description, connection, method, options...)
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
		_, requestDigest, err := bindRequestDigest(ctx, request)
		if err != nil {
			return nil, err
		}
		verified, err := verifier.VerifyAuthorizationContext(
			ctx,
			&internalrpcauthorityv1.VerifyAuthorizationContextRequest{
				CompactJws:                  values[0],
				ObservedFullMethod:          info.FullMethod,
				DownstreamPeer:              transport,
				ObservedRequestDigestSha256: requestDigest,
			},
		)
		if err != nil {
			return nil, err
		}
		if verified.GetContext() == nil {
			return nil, status.Error(codes.Internal, "verified authorization context missing")
		}
		authorized := context.WithValue(ctx, verifiedContextKey{}, verified.GetContext())
		authorized = context.WithValue(authorized, continuationSourceKey{}, continuationSource{
			compact: values[0], verified: verified.GetContext(),
		})
		return handler(authorized, request)
	}
}

// VerifierStreamServerInterceptor применяет ту же exact-method, mTLS и replay
// границу к streaming RPC и передаёт проверенный context серверному stream.
func VerifierStreamServerInterceptor(
	verifier internalrpcauthorityv1.AuthorizationVerifierServiceClient,
) grpc.StreamServerInterceptor {
	return func(
		service any,
		stream grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		ctx := stream.Context()
		transport, err := verifiedMTLSPeer(ctx)
		if err != nil {
			return err
		}
		incoming, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return status.Error(codes.Unauthenticated, "authorization context required")
		}
		values := incoming.Get(AuthorizationMetadata)
		if len(values) != 1 || values[0] == "" {
			return status.Error(codes.Unauthenticated, "authorization context required")
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
			return err
		}
		if verified.GetContext() == nil {
			return status.Error(codes.Internal, "verified authorization context missing")
		}
		return handler(service, &authorizedServerStream{
			ServerStream: stream,
			ctx: context.WithValue(
				context.WithValue(ctx, verifiedContextKey{}, verified.GetContext()),
				continuationSourceKey{}, continuationSource{compact: values[0], verified: verified.GetContext()},
			),
		})
	}
}

type authorizedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (stream *authorizedServerStream) Context() context.Context { return stream.ctx }

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
