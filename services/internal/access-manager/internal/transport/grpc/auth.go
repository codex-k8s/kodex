package grpc

import (
	"context"
	"crypto/subtle"
	"strings"

	grpcruntime "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	metadataAuthorization = "authorization"
	metadataCallerType    = "x-kodex-caller-type"
	metadataCallerID      = "x-kodex-caller-id"
)

type callerIdentityContextKey struct{}

// CallerIdentity is the verified platform caller for one gRPC request.
type CallerIdentity struct {
	Type string
	ID   string
}

// UnaryCallerAuthInterceptor rejects requests without a verified platform caller identity.
func UnaryCallerAuthInterceptor(required bool, sharedToken string) grpcruntime.UnaryServerInterceptor {
	sharedToken = strings.TrimSpace(sharedToken)
	return func(ctx context.Context, req any, info *grpcruntime.UnaryServerInfo, handler grpcruntime.UnaryHandler) (any, error) {
		if !required {
			return handler(ctx, req)
		}
		identity, err := authenticateCaller(ctx, sharedToken)
		if err != nil {
			return nil, err
		}
		return handler(context.WithValue(ctx, callerIdentityContextKey{}, identity), req)
	}
}

// CallerIdentityFromContext returns the verified platform caller when the auth interceptor populated it.
func CallerIdentityFromContext(ctx context.Context) (CallerIdentity, bool) {
	identity, ok := ctx.Value(callerIdentityContextKey{}).(CallerIdentity)
	return identity, ok
}

func authenticateCaller(ctx context.Context, sharedToken string) (CallerIdentity, error) {
	if sharedToken == "" {
		return CallerIdentity{}, status.Error(codes.Unauthenticated, "gRPC caller auth token is not configured")
	}
	incoming, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return CallerIdentity{}, status.Error(codes.Unauthenticated, "missing gRPC caller metadata")
	}
	if !authorizedBySharedToken(incoming, sharedToken) {
		return CallerIdentity{}, status.Error(codes.Unauthenticated, "invalid gRPC caller token")
	}
	identity := CallerIdentity{
		Type: firstMetadataValue(incoming, metadataCallerType),
		ID:   firstMetadataValue(incoming, metadataCallerID),
	}
	if identity.Type == "" || identity.ID == "" {
		return CallerIdentity{}, status.Error(codes.Unauthenticated, "missing gRPC caller identity")
	}
	return identity, nil
}

func authorizedBySharedToken(incoming metadata.MD, sharedToken string) bool {
	authorization := firstMetadataValue(incoming, metadataAuthorization)
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
