package grpcserver

import (
	"context"
	"crypto/subtle"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	// MetadataAuthorization is the incoming authorization metadata key.
	MetadataAuthorization = "authorization"
	// MetadataCallerType is the platform caller type metadata key.
	MetadataCallerType = "x-kodex-caller-type"
	// MetadataCallerID is the platform caller identifier metadata key.
	MetadataCallerID = "x-kodex-caller-id"
)

type callerIdentityContextKey struct{}

// Authenticator verifies the caller and may enrich the request context.
type Authenticator interface {
	AuthenticateCaller(context.Context) (context.Context, error)
}

// AuthenticatorFunc adapts a function to the Authenticator interface.
type AuthenticatorFunc func(context.Context) (context.Context, error)

// AuthenticateCaller verifies the caller through the wrapped function.
func (fn AuthenticatorFunc) AuthenticateCaller(ctx context.Context) (context.Context, error) {
	return fn(ctx)
}

// CallerIdentity is the verified platform caller for one gRPC request.
type CallerIdentity struct {
	Type string
	ID   string
}

// SharedTokenAuthenticator verifies service-to-service calls by bearer token and caller metadata.
type SharedTokenAuthenticator struct {
	SharedToken string
}

// NewSharedTokenAuthenticator creates the default platform shared-token authenticator.
func NewSharedTokenAuthenticator(sharedToken string) SharedTokenAuthenticator {
	return SharedTokenAuthenticator{SharedToken: strings.TrimSpace(sharedToken)}
}

// AuthenticateCaller verifies shared-token metadata and stores CallerIdentity in the request context.
func (auth SharedTokenAuthenticator) AuthenticateCaller(ctx context.Context) (context.Context, error) {
	identity, err := auth.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	return ContextWithCallerIdentity(ctx, identity), nil
}

// UnaryAuthInterceptor rejects requests without a verified platform caller identity.
func UnaryAuthInterceptor(required bool, authenticator Authenticator) UnaryInterceptor {
	return func(ctx context.Context, req any, info *UnaryServerInfo, handler UnaryHandler) (any, error) {
		if !required {
			return handler(ctx, req)
		}
		if authenticator == nil {
			return nil, status.Error(codes.Unauthenticated, "grpc authenticator is not configured")
		}
		authenticatedCtx, err := authenticator.AuthenticateCaller(ctx)
		if err != nil {
			return nil, err
		}
		return handler(authenticatedCtx, req)
	}
}

// ContextWithCallerIdentity stores a verified platform caller in the context.
func ContextWithCallerIdentity(ctx context.Context, identity CallerIdentity) context.Context {
	return context.WithValue(ctx, callerIdentityContextKey{}, identity)
}

// CallerIdentityFromContext returns the verified platform caller when auth populated it.
func CallerIdentityFromContext(ctx context.Context) (CallerIdentity, bool) {
	identity, ok := ctx.Value(callerIdentityContextKey{}).(CallerIdentity)
	return identity, ok
}

func (auth SharedTokenAuthenticator) authenticate(ctx context.Context) (CallerIdentity, error) {
	sharedToken := strings.TrimSpace(auth.SharedToken)
	if sharedToken == "" {
		return CallerIdentity{}, status.Error(codes.Unauthenticated, "grpc caller auth token is not configured")
	}
	incoming, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return CallerIdentity{}, status.Error(codes.Unauthenticated, "missing grpc caller metadata")
	}
	if !authorizedBySharedToken(incoming, sharedToken) {
		return CallerIdentity{}, status.Error(codes.Unauthenticated, "invalid grpc caller token")
	}
	identity := CallerIdentity{
		Type: firstMetadataValue(incoming, MetadataCallerType),
		ID:   firstMetadataValue(incoming, MetadataCallerID),
	}
	if identity.Type == "" || identity.ID == "" {
		return CallerIdentity{}, status.Error(codes.Unauthenticated, "missing grpc caller identity")
	}
	return identity, nil
}

func authorizedBySharedToken(incoming metadata.MD, sharedToken string) bool {
	authorization := firstMetadataValue(incoming, MetadataAuthorization)
	if len(authorization) < len("Bearer ") || !strings.EqualFold(authorization[:len("Bearer ")], "Bearer ") {
		return false
	}
	providedToken := strings.TrimSpace(authorization[len("Bearer "):])
	return subtle.ConstantTimeCompare([]byte(providedToken), []byte(sharedToken)) == 1
}

func firstMetadataValue(incoming metadata.MD, key string) string {
	values := incoming.Get(key)
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}
