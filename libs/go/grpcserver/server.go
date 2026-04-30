package grpcserver

import (
	"fmt"
	"log/slog"

	grpcruntime "google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
)

// UnaryInterceptor aliases the upstream gRPC unary server interceptor type.
type UnaryInterceptor = grpcruntime.UnaryServerInterceptor

// ServerOption aliases the upstream gRPC server option type.
type ServerOption = grpcruntime.ServerOption

// Dependencies contains runtime hooks that stay outside generic server limits.
type Dependencies struct {
	Logger            *slog.Logger
	Metrics           *Metrics
	Authenticator     Authenticator
	UnaryInterceptors []UnaryInterceptor
	ServerOptions     []ServerOption
}

// NewServer builds a gRPC server with the shared kodex runtime boundary.
func NewServer(cfg Config, deps Dependencies) (*grpcruntime.Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.AuthRequired && deps.Authenticator == nil {
		return nil, fmt.Errorf("grpc authenticator is required when auth is enabled")
	}
	interceptors := []UnaryInterceptor{
		UnaryRecoveryInterceptor(deps.Logger),
		UnaryMetricsInterceptor(deps.Metrics),
		UnaryAuthInterceptor(cfg.AuthRequired, deps.Authenticator),
		UnaryInFlightLimitInterceptor(cfg.MaxInFlight, deps.Metrics),
		UnaryDeadlineInterceptor(cfg.UnaryTimeout),
	}
	interceptors = append(interceptors, deps.UnaryInterceptors...)
	options := []ServerOption{
		grpcruntime.MaxConcurrentStreams(cfg.MaxConcurrentStreams),
		grpcruntime.MaxRecvMsgSize(cfg.MaxRecvMessageBytes),
		grpcruntime.MaxSendMsgSize(cfg.MaxSendMessageBytes),
		grpcruntime.KeepaliveParams(keepalive.ServerParameters{
			Time:    cfg.KeepaliveTime,
			Timeout: cfg.KeepaliveTimeout,
		}),
		grpcruntime.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             cfg.KeepaliveMinTime,
			PermitWithoutStream: cfg.PermitWithoutStream,
		}),
		grpcruntime.ChainUnaryInterceptor(interceptors...),
	}
	options = append(options, deps.ServerOptions...)
	return grpcruntime.NewServer(options...), nil
}
