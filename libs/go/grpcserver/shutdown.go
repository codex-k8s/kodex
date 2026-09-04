package grpcserver

import (
	"context"
	"errors"
	"time"
)

const forcedStopJoinTimeout = time.Second

type stoppableServer interface {
	GracefulStop()
	Stop()
}

// GracefulStop ждёт in-flight RPC до deadline, затем вызывает Stop и
// ограниченно ожидает завершение goroutine GracefulStop.
func GracefulStop(ctx context.Context, server stoppableServer) error {
	if ctx == nil || server == nil {
		return errors.New("gRPC shutdown configuration is invalid")
	}
	done := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		server.Stop()
	}
	timer := time.NewTimer(forcedStopJoinTimeout)
	defer timer.Stop()
	select {
	case <-done:
		return ctx.Err()
	case <-timer.C:
		return errors.Join(ctx.Err(), errors.New("forced gRPC shutdown join timed out"))
	}
}
