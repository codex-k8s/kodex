package grpcserver

import (
	"fmt"
	"time"
)

// Config contains transport-level limits for one gRPC server replica.
type Config struct {
	MaxInFlight          int
	MaxConcurrentStreams uint32
	UnaryTimeout         time.Duration
	KeepaliveTime        time.Duration
	KeepaliveTimeout     time.Duration
	KeepaliveMinTime     time.Duration
	PermitWithoutStream  bool
	MaxRecvMessageBytes  int
	MaxSendMessageBytes  int
	AuthRequired         bool
}

// Validate checks runtime limits before a gRPC server is constructed.
func (cfg Config) Validate() error {
	if cfg.MaxInFlight < 1 {
		return fmt.Errorf("grpc max in-flight must be greater than zero")
	}
	if cfg.MaxConcurrentStreams < 1 {
		return fmt.Errorf("grpc max concurrent streams must be greater than zero")
	}
	if cfg.UnaryTimeout < 0 {
		return fmt.Errorf("grpc unary timeout must not be negative")
	}
	if cfg.KeepaliveTime <= 0 {
		return fmt.Errorf("grpc keepalive time must be positive")
	}
	if cfg.KeepaliveTimeout <= 0 {
		return fmt.Errorf("grpc keepalive timeout must be positive")
	}
	if cfg.KeepaliveMinTime <= 0 {
		return fmt.Errorf("grpc keepalive min time must be positive")
	}
	if cfg.MaxRecvMessageBytes < 1 {
		return fmt.Errorf("grpc max receive message bytes must be greater than zero")
	}
	if cfg.MaxSendMessageBytes < 1 {
		return fmt.Errorf("grpc max send message bytes must be greater than zero")
	}
	return nil
}
