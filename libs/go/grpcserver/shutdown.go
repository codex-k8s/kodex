package grpcserver

import (
	"context"
	"errors"
)

type stoppableServer interface {
	GracefulStop()
	Stop()
}

// GracefulStop ждёт in-flight RPC до graceful deadline, затем асинхронно
// вызывает Stop и ограниченно ожидает обе потенциально блокирующие операции в
// независимом force-контексте.
func GracefulStop(gracefulCtx, forceCtx context.Context, server stoppableServer) error {
	if gracefulCtx == nil || forceCtx == nil || server == nil {
		return errors.New("gRPC shutdown configuration is invalid")
	}
	if _, bounded := gracefulCtx.Deadline(); !bounded {
		return errors.New("gRPC graceful shutdown deadline is required")
	}
	if _, bounded := forceCtx.Deadline(); !bounded {
		return errors.New("gRPC forced shutdown deadline is required")
	}
	gracefulDone := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(gracefulDone)
	}()
	select {
	case <-gracefulDone:
		return nil
	case <-gracefulCtx.Done():
	}
	stopDone := make(chan struct{})
	go func() {
		server.Stop()
		close(stopDone)
	}()
	for gracefulDone != nil || stopDone != nil {
		select {
		case <-gracefulDone:
			gracefulDone = nil
		case <-stopDone:
			stopDone = nil
		case <-forceCtx.Done():
			return errors.Join(gracefulCtx.Err(), forceCtx.Err(), errors.New("forced gRPC shutdown did not join"))
		}
	}
	return gracefulCtx.Err()
}
