package grpcserver

import (
	"context"
	"errors"
)

// stoppableServer exposes the two gRPC shutdown operations required for a
// bounded graceful-then-forced stop, including test doubles.
// RU: stoppableServer задаёт две операции остановки gRPC для ограниченного
// перехода от плавного завершения к принудительному, включая тестовые объекты.
type stoppableServer interface {
	// GracefulStop waits for in-flight RPCs to finish before returning.
	// RU: GracefulStop ожидает завершения текущих RPC перед возвратом.
	GracefulStop()
	// Stop forces the server to stop accepting or serving RPCs.
	// RU: Stop принудительно прекращает приём и выполнение RPC.
	Stop()
}

// GracefulStop waits for in-flight RPCs until a bounded graceful deadline;
// after expiry it starts Stop independently and joins both operations only
// within a second bounded force context.
// RU: GracefulStop ждёт текущие RPC до ограниченного плавного срока, затем
// независимо запускает Stop и ожидает обе операции лишь в отдельном
// ограниченном контексте принудительной остановки.
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
