package grpcserver

import grpcruntime "google.golang.org/grpc"

// UnaryServerInfo aliases the upstream gRPC unary server info type.
type UnaryServerInfo = grpcruntime.UnaryServerInfo

// UnaryHandler aliases the upstream gRPC unary handler type.
type UnaryHandler = grpcruntime.UnaryHandler
