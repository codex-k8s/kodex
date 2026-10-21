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

const (
	// IssuerSocketPath is the only accepted UDS path for local context issuance.
	// RU: IssuerSocketPath — единственный допустимый UDS-путь локального выпуска контекста.
	IssuerSocketPath = "/run/kodex/internal-rpc-authority/issuer.sock"
	// VerifierSocketPath is the only accepted UDS path for local verification.
	// RU: VerifierSocketPath — единственный допустимый UDS-путь локальной проверки.
	VerifierSocketPath = "/run/kodex/internal-rpc-authority/verifier.sock"
	// AuthorizationMetadata names the single outbound/inbound context header.
	// RU: AuthorizationMetadata задаёт имя единственного заголовка контекста.
	AuthorizationMetadata = "x-kodex-authorization"
	// proofRetryTimeout bounds the one retry of a transient proof failure.
	// RU: proofRetryTimeout ограничивает единственный повтор временного отказа proof.
	proofRetryTimeout = 500 * time.Millisecond
)

// LocalConfig selects an allowlisted authority socket and the exact UID/GID
// that the UDS transport must authenticate before serving application RPCs.
// RU: LocalConfig выбирает разрешённый socket authority и точные UID/GID,
// которые UDS-транспорт обязан проверить до обслуживания RPC приложения.
type LocalConfig struct {
	SocketPath        string
	ExpectedServerUID uint32
	ExpectedServerGID uint32
	DialTimeout       time.Duration
}

// LocalConnection owns a gRPC connection established through the checked UDS.
// RU: LocalConnection владеет gRPC-соединением через проверенный UDS.
type LocalConnection struct {
	connection *grpc.ClientConn
}

// DialLocal accepts only canonical socket configuration, checks its directory,
// authenticates the server UID/GID through UDS credentials, and waits for Ready.
// RU: DialLocal принимает только каноническую конфигурацию socket, проверяет
// каталог, удостоверяет UID/GID сервера через UDS и ожидает состояния Ready.
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

// Issuer returns the typed issuance client on this authenticated connection.
// RU: Issuer возвращает типизированный клиент выпуска на удостоверенном соединении.
func (connection *LocalConnection) Issuer() internalrpcauthorityv1.AuthorizationIssuerServiceClient {
	return internalrpcauthorityv1.NewAuthorizationIssuerServiceClient(connection.connection)
}

// Verifier returns the typed verification client on this authenticated connection.
// RU: Verifier возвращает типизированный клиент проверки на удостоверенном соединении.
func (connection *LocalConnection) Verifier() internalrpcauthorityv1.AuthorizationVerifierServiceClient {
	return internalrpcauthorityv1.NewAuthorizationVerifierServiceClient(connection.connection)
}

// Close releases the underlying local gRPC connection.
// RU: Close освобождает нижележащее локальное gRPC-соединение.
func (connection *LocalConnection) Close() error {
	return connection.connection.Close()
}

// validateLocalConfig rejects noncanonical paths, missing server identity,
// out-of-range deadlines, and a missing or symlinked socket directory.
// RU: validateLocalConfig отклоняет неканонические пути, отсутствующую
// идентичность сервера, недопустимый тайм-аут и отсутствующий каталог socket
// либо его подмену символической ссылкой.
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

// ProofProvider obtains a short-lived proof for one registered operation and
// full RPC method without becoming the source of transport authority itself.
// RU: ProofProvider получает краткоживущий proof для зарегистрированной
// операции и полного RPC-метода, не становясь источником транспортных прав.
type ProofProvider interface {
	// AuthorityProof returns compact proof and correlation for the exact operation.
	// RU: AuthorityProof возвращает compact proof и correlation для точной операции.
	AuthorityProof(
		ctx context.Context,
		operationID string,
		fullMethod string,
	) (compactJWS string, correlationID string, err error)
}

// OperationResolver maps a full RPC method to an explicitly registered operation.
// RU: OperationResolver сопоставляет полный RPC-метод явно зарегистрированной операции.
type OperationResolver interface {
	// OperationID rejects methods without an exact registration.
	// RU: OperationID отклоняет методы без точной регистрации.
	OperationID(fullMethod string) (string, bool)
}

// requestDigestKey keeps the deterministic request digest private to this package.
// RU: requestDigestKey оставляет детерминированный digest запроса внутри пакета.
type requestDigestKey struct{}

// RequestDigest reads the digest of the actual unary or sole initial
// server-stream request, computed before the proof provider is invoked.
// RU: RequestDigest читает digest фактического unary либо единственного
// первичного server-stream запроса, вычисленный до обращения к поставщику proof.
func RequestDigest(ctx context.Context) (string, bool) {
	value, ok := ctx.Value(requestDigestKey{}).(string)
	return value, ok && value != ""
}

// bindRequestDigest deterministically encodes a protobuf request, stores its
// SHA-256 digest in context, and rejects non-protobuf or unencodable input.
// RU: bindRequestDigest детерминированно кодирует protobuf-запрос, сохраняет
// его SHA-256 digest в контексте и отклоняет неподходящий вход.
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

// continuationSource retains the verified parent context and its compact JWS
// only inside the interceptor-controlled context.
// RU: continuationSource хранит проверенный контекст родителя и его compact JWS
// только внутри контекста, управляемого interceptor.
type continuationSource struct {
	compact  string
	verified *internalrpcauthorityv1.VerifiedAuthorizationContext
}

// continuationSourceKey prevents callers from forging the parent's context slot.
// RU: continuationSourceKey не даёт caller подделать слот родительского контекста.
type continuationSourceKey struct{}

// continuationRequestKey keeps a pending child request package-private.
// RU: continuationRequestKey оставляет ожидающий дочерний запрос внутри пакета.
type continuationRequestKey struct{}

// continuationRequest binds the intended child operation and exact request /
// correlation identifiers until the actual protobuf request is available.
// RU: continuationRequest связывает дочернюю операцию и точные идентификаторы
// запроса/correlation до появления фактического protobuf-запроса.
type continuationRequest struct {
	operationID, fullMethod, requestID, correlationID string
}

// BindContinuation accepts a child issuance request only from a context
// created by the verifier after durable acceptance of the parent JTI.
// RU: BindContinuation допускает запрос дочернего выпуска только из контекста,
// созданного verifier после устойчивого принятия JTI родителя.
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

// ContinuationUnaryClientInterceptor matches the registered child operation
// and parent provenance, then issues a digest-bound child context immediately
// before invoking the downstream RPC with the actual protobuf request.
// RU: ContinuationUnaryClientInterceptor сверяет зарегистрированную дочернюю
// операцию и происхождение родителя, затем выпускает привязанный к digest
// контекст перед вызовом downstream RPC с фактическим protobuf-запросом.
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
			return newLocalAuthorityError(err, pending.correlationID, StageContinuationIssue)
		}
		return invoker(metadata.AppendToOutgoingContext(ctx, AuthorizationMetadata, issued.GetCompactJws()), method, request, reply, connection, options...)
	}
}

// LocalAuthorityError distinguishes local context-issuance failure from a
// downstream RPC error. Only this trusted type carries a sanitized diagnostic
// for a caller to classify without trusting an arbitrary bare gRPC status.
// RU: LocalAuthorityError отличает локальный отказ выпуска контекста от ошибки
// downstream RPC. Только этот доверенный тип несёт очищенную диагностику;
// произвольный bare gRPC status не становится доверенным доменным ответом.
type LocalAuthorityError struct {
	code          codes.Code
	correlationID string
	diagnostic    Diagnostic
}

// Error formats the public gRPC status with a bounded local diagnostic,
// excluding the raw provider error and its untrusted details.
// RU: Error форматирует публичный gRPC status с ограниченной локальной
// диагностикой, исключая исходную ошибку поставщика и чужие details.
func (failure *LocalAuthorityError) Error() string {
	return failure.GRPCStatus().Err().Error() + " [" + failure.diagnostic.String() + "]"
}

// GRPCStatus exposes only the classified code and a fixed public message to
// standard gRPC helpers; its local diagnostic is deliberately omitted.
// RU: GRPCStatus отдаёт стандартным gRPC helpers только классифицированный код
// и фиксированный публичный текст; локальная диагностика намеренно исключена.
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

// CorrelationID returns the exact proof request identifier without exposing
// credentials or the internal cause of the failure.
// RU: CorrelationID возвращает идентификатор точного proof-запроса, не раскрывая
// удостоверения или внутреннюю причину отказа.
func (failure *LocalAuthorityError) CorrelationID() string {
	return failure.correlationID
}

// newLocalAuthorityError preserves only transient dependency codes as-is;
// all other failures become Unauthenticated with a sanitized diagnostic.
// RU: newLocalAuthorityError сохраняет только временные коды зависимости;
// остальные отказы становятся Unauthenticated с очищенной диагностикой.
func newLocalAuthorityError(err error, correlationID string, stage DiagnosticStage) error {
	code := authorityFailureCode(err)
	switch code {
	case codes.Canceled, codes.Unavailable, codes.DeadlineExceeded:
	default:
		code = codes.Unauthenticated
	}
	return &LocalAuthorityError{code: code, correlationID: correlationID, diagnostic: diagnosticFrom(err, stage)}
}

// authorityFailureCode maps context cancellation/deadline before consulting
// the gRPC status embedded in the provider error.
// RU: authorityFailureCode сначала классифицирует отмену и срок контекста,
// затем обращается к gRPC status в ошибке поставщика.
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

// authorityProofWithRetry makes at most one bounded retry of a transient
// provider failure and otherwise returns the original proof result unchanged.
// RU: authorityProofWithRetry выполняет не более одного ограниченного повтора
// временного отказа поставщика; иначе возвращает исходный результат proof.
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

// retryableProofFailure permits a retry only while the caller context is
// live and the classified failure is canceled, deadline, or unavailable.
// RU: retryableProofFailure допускает повтор лишь при живом контексте caller
// и классификации Canceled, DeadlineExceeded либо Unavailable.
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

// IssuerUnaryClientInterceptor digests the actual request, resolves an exact
// operation, obtains proof and authorization context, then invokes downstream.
// RU: IssuerUnaryClientInterceptor вычисляет digest фактического запроса,
// разрешает точную операцию, получает proof и контекст, затем вызывает downstream.
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
			return newLocalAuthorityError(err, correlationID, StageProofResolve)
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
			return newLocalAuthorityError(err, correlationID, StageContextIssue)
		}
		ctx = metadata.AppendToOutgoingContext(
			ctx,
			AuthorizationMetadata,
			issued.GetCompactJws(),
		)
		return invoker(ctx, method, request, reply, connection, options...)
	}
}

// IssuerStreamClientInterceptor issues exact-method context before opening a
// stream, or defers it to the initial request for explicitly request-bound
// methods. Each new stream needs a fresh proof and verifier replay protection.
// RU: IssuerStreamClientInterceptor выпускает контекст точного метода до
// открытия stream либо откладывает его до первого запроса для явно привязанных
// методов. Каждый новый stream требует свежего proof и защиты verifier от replay.
func IssuerStreamClientInterceptor(
	issuer internalrpcauthorityv1.AuthorizationIssuerServiceClient,
	operations OperationResolver,
	proofs ProofProvider,
	requestBoundMethods ...string,
) grpc.StreamClientInterceptor {
	return func(
		ctx context.Context,
		description *grpc.StreamDesc,
		connection *grpc.ClientConn,
		method string,
		streamer grpc.Streamer,
		options ...grpc.CallOption,
	) (grpc.ClientStream, error) {
		if requestBoundStreamMethod(method, requestBoundMethods) {
			return newRequestBoundClientStream(ctx, description, connection, method, streamer, options, issuer, operations, proofs)
		}
		operationID, ok := operations.OperationID(method)
		if !ok {
			return nil, status.Error(codes.PermissionDenied, "internal RPC operation is not registered")
		}
		proof, correlationID, err := authorityProofWithRetry(ctx, proofs, operationID, method)
		if err != nil {
			return nil, newLocalAuthorityError(err, correlationID, StageProofResolve)
		}
		issued, err := issuer.IssueAuthorizationContext(ctx, &internalrpcauthorityv1.IssueAuthorizationContextRequest{
			OperationId: operationID, CorrelationId: correlationID, AuthorityProofCompactJws: proof,
		})
		if err != nil {
			return nil, newLocalAuthorityError(err, correlationID, StageContextIssue)
		}
		ctx = metadata.AppendToOutgoingContext(ctx, AuthorizationMetadata, issued.GetCompactJws())
		return streamer(ctx, description, connection, method, options...)
	}
}

// verifiedContextKey keeps server-verified authorization private to this package.
// RU: verifiedContextKey оставляет проверенную сервером авторизацию внутри пакета.
type verifiedContextKey struct{}

// VerifiedAuthorizationContext exposes only context installed after the
// server interceptor has verified transport peer and signed authorization.
// RU: VerifiedAuthorizationContext выдаёт только контекст, установленный после
// проверки транспортного peer и подписанной авторизации серверным interceptor.
func VerifiedAuthorizationContext(
	ctx context.Context,
) (*internalrpcauthorityv1.VerifiedAuthorizationContext, bool) {
	value, ok := ctx.Value(verifiedContextKey{}).(*internalrpcauthorityv1.VerifiedAuthorizationContext)
	return value, ok && value != nil
}

// VerifierUnaryServerInterceptor checks the mTLS peer, one signed context,
// exact method, and request digest before calling the protected handler.
// RU: VerifierUnaryServerInterceptor проверяет mTLS peer, единственный
// подписанный контекст, точный метод и digest до вызова защищённого handler.
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

// VerifierStreamServerInterceptor applies the mTLS, exact-method and replay
// boundary to streaming RPCs; request-bound streams also verify the initial
// payload digest before publishing context or sending a response.
// RU: VerifierStreamServerInterceptor применяет mTLS, точный метод и защиту от
// replay к stream RPC; привязанные к запросу stream дополнительно проверяют
// digest первого сообщения до публикации контекста или отправки ответа.
func VerifierStreamServerInterceptor(
	verifier internalrpcauthorityv1.AuthorizationVerifierServiceClient,
	requestBoundMethods ...string,
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
		if requestBoundStreamMethod(info.FullMethod, requestBoundMethods) {
			if info.IsClientStream || !info.IsServerStream {
				return status.Error(codes.Internal, "request-bound server stream descriptor is invalid")
			}
			return handler(service, &requestBoundServerStream{ServerStream: stream, ctx: ctx,
				verifier: verifier, transport: transport, compact: values[0], method: info.FullMethod})
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

// authorizedServerStream wraps a server stream with the verified context.
// RU: authorizedServerStream оборачивает серверный stream проверенным контекстом.
type authorizedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

// Context returns the context containing verified authorization for the
// downstream streaming handler.
// RU: Context возвращает контекст с проверенной авторизацией downstream handler.
func (stream *authorizedServerStream) Context() context.Context { return stream.ctx }

// verifiedMTLSPeer requires a verified TLS chain and exactly one peer
// certificate, then returns its exact SPIFFE ID and certificate digest.
// RU: verifiedMTLSPeer требует проверенную TLS-цепь и ровно один сертификат
// peer, затем возвращает точный SPIFFE ID и digest сертификата.
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

// exactSPIFFEID accepts only one URI SAN with a plain spiffe scheme and host;
// user info, query, fragment and missing identity fail closed.
// RU: exactSPIFFEID принимает один URI SAN с простыми scheme spiffe и host;
// user info, query, fragment и отсутствие идентичности отклоняются.
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
