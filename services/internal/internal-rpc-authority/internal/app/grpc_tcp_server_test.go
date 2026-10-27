package app

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/observability"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// syntheticHealthServer supplies closed synthetic responses for local interceptor verification.
// syntheticHealthServer возвращает закрытые синтетические ответы для локальной проверки interceptors.
type syntheticHealthServer struct {
	healthpb.UnimplementedHealthServer
}

// Check returns one expected or unexpected error without accessing external state.
// Check возвращает одну ожидаемую или неожиданную ошибку без обращения к внешнему состоянию.
func (syntheticHealthServer) Check(_ context.Context, req *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	if req.Service == "expected" {
		return nil, status.Error(codes.InvalidArgument, "synthetic validation")
	}
	return nil, status.Error(codes.Internal, "synthetic internal")
}

// syntheticMutualTLS creates a temporary in-memory test certificate used for both peer roles.
// syntheticMutualTLS создаёт временный тестовый сертификат в памяти для обеих сторон соединения.
func syntheticMutualTLS(t *testing.T) (*tls.Config, *tls.Config) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "synthetic.local"}, DNSNames: []string{"synthetic.local"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	pair := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots}, &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}, ServerName: "synthetic.local", RootCAs: roots}
}

// TestObservedTCPGRPCServer verifies local mTLS, strict unary error handling, bounded method logs
// and metrics for each caller normalizer without starting an authority service or contacting a cluster.
// TestObservedTCPGRPCServer проверяет локальные mTLS, обработку unary ошибок, ограниченные имена
// методов в журнале и метрики для каждого normalizer без запуска authority или обращения к кластеру.
func TestObservedTCPGRPCServer(t *testing.T) {
	serverTLS, clientTLS := syntheticMutualTLS(t)
	runtime, err := observability.NewRuntime(t.Context(), observability.RuntimeConfig{Disabled: true, ServiceName: "synthetic", ServiceVersion: "test", Environment: "test"})
	if err != nil {
		t.Fatal(err)
	}
	for name, normalize := range map[string]func(string) string{"publisher": normalizePublisherMethod, "readback": normalizeReadbackMethod, "restore": normalizeRestoreMethod} {
		t.Run(name, func(t *testing.T) {
			const method = "/grpc.health.v1.Health/Check"
			methods := map[string]string{method: "synthetic_probe"}
			metrics := observability.NewMetrics("synthetic", "test", methods)
			var log bytes.Buffer
			logger := runtime.Logger(&log)
			server := newObservedTCPGRPCServer(serverTLS, metrics, &appTelemetry{Runtime: runtime}, methods, logger, normalize)
			healthpb.RegisterHealthServer(server, syntheticHealthServer{})
			listener := bufconn.Listen(1024 * 1024)
			go func() { _ = server.Serve(listener) }()
			t.Cleanup(func() { server.Stop(); _ = listener.Close() })
			conn, err := grpc.NewClient("passthrough:///synthetic.local", grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }), grpc.WithTransportCredentials(credentials.NewTLS(clientTLS)))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			client := healthpb.NewHealthClient(conn)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			if _, err := client.Check(ctx, &healthpb.HealthCheckRequest{Service: "expected"}); status.Code(err) != codes.InvalidArgument {
				t.Fatalf("expected validation error, got %v", err)
			}
			if _, err := client.Check(ctx, &healthpb.HealthCheckRequest{Service: "unexpected"}); status.Code(err) != codes.Internal {
				t.Fatalf("expected internal error, got %v", err)
			}
			if name == "publisher" {
				missingClient := clientTLS.Clone()
				missingClient.Certificates = nil
				wrongServer := clientTLS.Clone()
				wrongServer.ServerName = "wrong.synthetic.local"
				for scenario, config := range map[string]*tls.Config{"missing client certificate": missingClient, "wrong server name": wrongServer} {
					bad, openErr := grpc.NewClient("passthrough:///synthetic.local", grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }), grpc.WithTransportCredentials(credentials.NewTLS(config)))
					if openErr != nil {
						t.Fatal(openErr)
					}
					badCtx, badCancel := context.WithTimeout(t.Context(), time.Second)
					_, callErr := healthpb.NewHealthClient(bad).Check(badCtx, &healthpb.HealthCheckRequest{})
					badCancel()
					_ = bad.Close()
					if callErr == nil {
						t.Fatalf("%s unexpectedly accepted", scenario)
					}
				}
			}
			if strings.Count(log.String(), "unexpected gRPC failure") != 1 || !strings.Contains(log.String(), `"method":"unknown"`) || strings.Contains(log.String(), "synthetic internal") {
				t.Fatalf("unexpected boundary logging: %s", log.String())
			}
			response := httptest.NewRecorder()
			metrics.PrometheusHandler().ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
			if !strings.Contains(response.Body.String(), `operation="synthetic_probe"`) || !strings.Contains(response.Body.String(), `code="Internal"`) {
				t.Fatal("missing bounded unary metrics")
			}
		})
	}
}
