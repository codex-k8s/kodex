package app

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	health "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"
)

// controlPlaneBlockingRPC simulates a real admitted handler before it owns any spool file. It
// can honor cancellation or deliberately wait for an external dependency to return.
// controlPlaneBlockingRPC моделирует actual admitted handler до открытия spool file. Он может
// учитывать cancellation либо намеренно ожидать завершения external dependency.
type controlPlaneBlockingRPC struct {
	health.UnimplementedHealthServer
	entered            chan struct{}
	release            chan struct{}
	ignoreCancellation bool
}

// wait signals admission and preserves the independent dependency lifetime when requested.
// wait отмечает admission и при запросе сохраняет independent dependency lifetime.
func (server *controlPlaneBlockingRPC) wait(ctx context.Context) {
	close(server.entered)
	if server.ignoreCancellation {
		<-server.release
		return
	}
	select {
	case <-server.release:
	case <-ctx.Done():
	}
}

// Check represents a synchronous capture finalizer blocked before opening its anonymous file.
// Check представляет synchronous capture finalizer, заблокированный до anonymous file open.
func (server *controlPlaneBlockingRPC) Check(ctx context.Context, _ *health.HealthCheckRequest) (*health.HealthCheckResponse, error) {
	server.wait(ctx)
	return &health.HealthCheckResponse{}, nil
}

// Watch proves the same handler-join boundary for server streaming output consumers.
// Watch проверяет ту же handler-join boundary для server streaming output consumers.
func (server *controlPlaneBlockingRPC) Watch(_ *health.HealthCheckRequest, stream health.Health_WatchServer) error {
	server.wait(stream.Context())
	return nil
}

// TestControlPlaneGRPCShutdownRequiresActualHandlerJoin drives real unary and streaming RPCs.
// Forced cancellation joins a cooperative handler; a handler still waiting before file creation
// must keep dependencies alive until its actual return, regardless of the shutdown timeout.
// TestControlPlaneGRPCShutdownRequiresActualHandlerJoin выполняет actual unary и streaming RPC.
// Forced cancellation ожидает cooperative handler; handler, ожидающий до file creation,
// удерживает dependencies до actual return независимо от shutdown timeout.
func TestControlPlaneGRPCShutdownRequiresActualHandlerJoin(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		for _, ignore := range []bool{false, true} {
			name := "unary"
			if streaming {
				name = "stream"
			}
			if ignore {
				name += "-blocked-before-spool"
			} else {
				name += "-cancellation-joined"
			}
			t.Run(name, func(t *testing.T) {
				rpc := &controlPlaneBlockingRPC{entered: make(chan struct{}), release: make(chan struct{}), ignoreCancellation: ignore}
				var release sync.Once
				server := grpc.NewServer(grpc.WaitForHandlers(true))
				health.RegisterHealthServer(server, rpc)
				listener := bufconn.Listen(64 << 10)
				lifecycle := newControlPlaneGRPCLifecycle(server, listener)
				worker, cancelWorker := context.WithCancel(t.Context())
				workerDone := make(chan struct{})
				go func() {
					_ = lifecycle.serve(worker)
					close(workerDone)
				}()
				connection, err := grpc.NewClient("passthrough:///synthetic-control-plane", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					release.Do(func() { close(rpc.release) })
					cancelWorker()
					_ = connection.Close()
					server.Stop()
				}()
				callDone := make(chan struct{})
				go func() {
					client := health.NewHealthClient(connection)
					if streaming {
						stream, err := client.Watch(t.Context(), &health.HealthCheckRequest{})
						if err == nil {
							_, _ = stream.Recv()
						}
					} else {
						_, _ = client.Check(t.Context(), &health.HealthCheckRequest{})
					}
					close(callDone)
				}()
				waitControlPlaneRPCTest(t, rpc.entered)
				cancelWorker()
				stop, cancelStop := context.WithTimeout(t.Context(), 200*time.Millisecond)
				_ = lifecycle.shutdown(stop)
				cancelStop()
				if lifecycle.joined() == ignore {
					t.Fatal("shutdown inferred handler join from cancellation or timeout")
				}
				release.Do(func() { close(rpc.release) })
				for _, done := range []<-chan struct{}{lifecycle.serveDone, lifecycle.gracefulDone, lifecycle.forcedDone, workerDone, callDone} {
					waitControlPlaneRPCTest(t, done)
				}
				if !lifecycle.joined() {
					t.Fatal("actual handler return did not establish dependency join")
				}
			})
		}
	}
}

// waitControlPlaneRPCTest bounds fixture joins and never treats an elapsed observation as success.
// waitControlPlaneRPCTest ограничивает fixture joins и не принимает elapsed observation за успех.
func waitControlPlaneRPCTest(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("control-plane RPC fixture did not join")
	}
}
