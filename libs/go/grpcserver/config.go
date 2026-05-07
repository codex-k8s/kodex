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

// RuntimeSettings contains service-level gRPC runtime settings before validation.
type RuntimeSettings struct {
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

// ConfigFromRuntimeSettings converts service runtime settings to server config.
func ConfigFromRuntimeSettings(settings RuntimeSettings) Config {
	return Config(settings)
}

// ConfigFromRuntimeValues converts common service env fields to server config.
func ConfigFromRuntimeValues(
	maxInFlight int,
	maxConcurrentStreams uint32,
	unaryTimeout time.Duration,
	keepaliveTime time.Duration,
	keepaliveTimeout time.Duration,
	keepaliveMinTime time.Duration,
	permitWithoutStream bool,
	maxRecvMessageBytes int,
	maxSendMessageBytes int,
	authRequired bool,
) Config {
	return ConfigFromRuntimeSettings(RuntimeSettings{
		MaxInFlight:          maxInFlight,
		MaxConcurrentStreams: maxConcurrentStreams,
		UnaryTimeout:         unaryTimeout,
		KeepaliveTime:        keepaliveTime,
		KeepaliveTimeout:     keepaliveTimeout,
		KeepaliveMinTime:     keepaliveMinTime,
		PermitWithoutStream:  permitWithoutStream,
		MaxRecvMessageBytes:  maxRecvMessageBytes,
		MaxSendMessageBytes:  maxSendMessageBytes,
		AuthRequired:         authRequired,
	})
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
