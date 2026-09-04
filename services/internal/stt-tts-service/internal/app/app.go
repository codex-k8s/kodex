// Package app содержит единственный composition root stt-tts-service.
package app

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/codex-k8s/kodex/libs/go/grpcserver"
	"github.com/codex-k8s/kodex/libs/go/httpserver"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/clients/projection"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/clients/protectedrpc"
	transcriptionservice "github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/service/transcription"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/integration/provider/openai"
	servicemetrics "github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/observability/metrics"
	transportgrpc "github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/transport/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
)

const (
	serviceName        = "stt-tts-service"
	metricsSubsystem   = "stt_tts_service"
	callerSPIFFEID     = "spiffe://kodex.local/ns/kodex-system/sa/control-api-gateway"
	grpcShutdownBudget = 22 * time.Second
)

type checker interface{ Check(context.Context) error }

func Run(lifecycle, shutdownBase context.Context, buildVersion string) (resultErr error) {
	config, err := loadConfig()
	if err != nil {
		return err
	}
	startup, cancelStartup := context.WithTimeout(lifecycle, config.StartupTimeout)
	defer cancelStartup()
	telemetryConfig, err := sharedobservability.RuntimeConfigFromEnv(serviceName, buildVersion)
	if err != nil {
		return err
	}
	telemetry, err := sharedobservability.NewRuntime(startup, telemetryConfig)
	if err != nil {
		return err
	}
	logger := telemetry.Logger(os.Stdout)
	methods := map[string]string{
		sttv1.SpeechToTextService_Transcribe_FullMethodName:         "transcribe",
		sttv1.SpeechToTextService_CheckReadiness_FullMethodName:     "readiness",
		sttv1.SpeechToTextService_CheckProtectedPath_FullMethodName: "protected_path",
	}
	metrics := sharedobservability.NewMetrics(metricsSubsystem, buildVersion, methods)
	outcomes := servicemetrics.New()
	if err := metrics.Register(outcomes.Collector()); err != nil {
		return errors.New("register STT service metrics")
	}
	readiness := serviceruntime.NewReadiness()
	readiness.Set(false, "local_runtime_starting")
	metrics.SetReady(false)
	issuer, err := authorityclient.DialLocal(startup, authorityclient.LocalConfig{
		SocketPath: config.AuthorityIssuerSocket, ExpectedServerUID: config.AuthorityIssuerUID,
		ExpectedServerGID: config.AuthorityIssuerGID, DialTimeout: config.ReadinessTimeout,
	})
	if err != nil {
		return errors.New("connect STT authorization issuer")
	}
	dependencies, err := protectedrpc.Dial(startup, protectedrpc.Config{
		Policy:          protectedrpc.TargetConfig{Target: config.PolicyTarget, TLSServerName: config.PolicyTLSServerName, CAFile: config.DependencyCAFile},
		Credential:      protectedrpc.TargetConfig{Target: config.CredentialTarget, TLSServerName: config.CredentialTLSServerName, CAFile: config.DependencyCAFile},
		CertificateFile: config.WorkloadCertificateFile, PrivateKeyFile: config.WorkloadPrivateKeyFile,
		DialTimeout: config.ReadinessTimeout, Issuer: issuer.Issuer(),
	})
	if err != nil {
		_ = issuer.Close()
		return err
	}
	policy, err := projection.NewPolicy(dependencies.Policy, dependencies)
	if err != nil {
		_ = dependencies.Close()
		_ = issuer.Close()
		return err
	}
	credential, err := projection.NewCredential(dependencies.Credential, dependencies)
	if err != nil {
		_ = dependencies.Close()
		_ = issuer.Close()
		return err
	}
	provider, err := openai.New()
	if err != nil {
		_ = dependencies.Close()
		_ = issuer.Close()
		return err
	}
	domain, err := transcriptionservice.New(policy, credential, provider, outcomes, config.RequestTimeout)
	if err != nil {
		_ = dependencies.Close()
		_ = issuer.Close()
		return err
	}
	verifier, err := authorityclient.DialLocal(startup, authorityclient.LocalConfig{
		SocketPath: config.AuthorityVerifierSocket, ExpectedServerUID: config.AuthorityVerifierUID,
		ExpectedServerGID: config.AuthorityVerifierGID, DialTimeout: config.ReadinessTimeout,
	})
	if err != nil {
		_ = dependencies.Close()
		_ = issuer.Close()
		return errors.New("connect STT authorization verifier")
	}
	handler, err := transportgrpc.New(domain, config.SpoolDirectory, readiness, config.RequestTimeout)
	if err != nil {
		_ = verifier.Close()
		_ = dependencies.Close()
		_ = issuer.Close()
		return err
	}
	tlsConfig, err := serverTLS(config)
	if err != nil {
		_ = verifier.Close()
		_ = dependencies.Close()
		_ = issuer.Close()
		return err
	}
	errorObserver := grpcserver.ErrorObserverFunc(func(ctx context.Context, method string, code codes.Code, _ error) {
		logger.ErrorContext(ctx, "unexpected gRPC failure", "method", normalizeMethod(methods, method), "code", code.String(),
			"correlation_id", normalizeCorrelation(sharedobservability.CorrelationID(ctx)))
	})
	grpcServer := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(tlsConfig)), grpc.ForceServerCodec(grpcserver.StrictProtoCodec()),
		grpc.ChainUnaryInterceptor(metrics.UnaryServerInterceptor(), telemetry.UnaryServerInterceptor(methods),
			authorityclient.VerifierUnaryServerInterceptor(verifier.Verifier()), grpcserver.RejectMalformedUnary,
			grpcserver.ErrorBoundary(errorObserver)),
		grpc.ChainStreamInterceptor(metrics.StreamServerInterceptor(), sharedobservability.StreamCorrelationServerInterceptor(),
			telemetry.StreamServerInterceptor(methods), authorityclient.VerifierStreamServerInterceptor(verifier.Verifier()),
			handler.StreamServerInterceptor(), grpcserver.RejectMalformedStream, grpcserver.StreamErrorBoundary(errorObserver)),
		grpc.MaxConcurrentStreams(uint32(value.MaximumConcurrentStreams)), grpc.MaxRecvMsgSize(68<<10), grpc.MaxSendMsgSize(1<<20),
	)
	sttv1.RegisterSpeechToTextServiceServer(grpcServer, handler)
	grpcListener, err := net.Listen("tcp", config.GRPCListen)
	if err != nil {
		_ = verifier.Close()
		_ = dependencies.Close()
		_ = issuer.Close()
		return errors.New("listen for STT gRPC")
	}
	technical, err := httpserver.New(httpserver.Config{
		Address: config.TechnicalListen, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second,
		MaximumHeaderBytes: 16 << 10, MaximumConnections: 128,
	}, readiness, metrics.PrometheusHandler(), httpserver.ExactGETRoute{
		Path: "/diagnostics/protected-path", ContentType: "application/json; charset=utf-8",
		Handler: protectedPathHandler(domain, config.ReadinessTimeout),
	})
	if err != nil || technical.Listen() != nil {
		_ = grpcListener.Close()
		_ = verifier.Close()
		_ = dependencies.Close()
		_ = issuer.Close()
		return errors.New("listen for STT technical HTTP")
	}
	localChecks := []checker{domainLocalCheck{domain}, dependencies, verifierReadiness{verifier.Verifier()}, spoolReadiness{config.SpoolDirectory}}
	if _, err := updateLocalReadiness(startup, readiness, metrics, localChecks...); err != nil {
		_ = grpcListener.Close()
		_ = technical.Shutdown(shutdownBase)
		_ = verifier.Close()
		_ = dependencies.Close()
		_ = issuer.Close()
		return errors.Join(errors.New("STT local startup barrier failed"), err)
	}
	workers := serviceruntime.StartWorkers(lifecycle, serveGRPC(grpcServer, grpcListener), serveHTTP(technical),
		monitorReadiness(readiness, metrics, logger, config.ReadinessTimeout, localChecks...))
	workerDone := make(chan error, 1)
	go func() { workerDone <- workers.Wait(context.WithoutCancel(shutdownBase)) }()
	select {
	case resultErr = <-workerDone:
	case <-lifecycle.Done():
	}
	readiness.Set(false, "shutting_down")
	metrics.SetReady(false)
	workers.Stop()
	shutdownErr := serviceruntime.RunShutdown(shutdownBase,
		serviceruntime.ShutdownOperation{Name: "STT gRPC server", Timeout: grpcShutdownBudget, Run: func(ctx context.Context) error {
			graceful, cancelGraceful := context.WithTimeout(ctx, 20*time.Second)
			defer cancelGraceful()
			force, cancelForce := context.WithTimeout(shutdownBase, grpcShutdownBudget)
			defer cancelForce()
			return grpcserver.GracefulStop(graceful, force, grpcServer)
		}},
		serviceruntime.ShutdownOperation{Name: "STT technical HTTP server", Timeout: 2 * time.Second, Run: technical.Shutdown},
		serviceruntime.ShutdownOperation{Name: "STT workers", Timeout: 2 * time.Second, Run: workers.Wait},
		serviceruntime.ShutdownOperation{Name: "STT dependency connections", Timeout: time.Second, Run: func(context.Context) error { return dependencies.Close() }},
		serviceruntime.ShutdownOperation{Name: "STT verifier connection", Timeout: time.Second, Run: func(context.Context) error { return verifier.Close() }},
		serviceruntime.ShutdownOperation{Name: "STT issuer connection", Timeout: time.Second, Run: func(context.Context) error { return issuer.Close() }},
		serviceruntime.ShutdownOperation{Name: "STT tracing", Timeout: time.Second, Run: telemetry.ShutdownTracing},
		serviceruntime.ShutdownOperation{Name: "STT Sentry", Timeout: time.Second, Run: telemetry.FlushSentry},
	)
	return errors.Join(resultErr, shutdownErr)
}

type domainLocalCheck struct{ service *transcriptionservice.Service }

func (check domainLocalCheck) Check(ctx context.Context) error { return check.service.CheckLocal(ctx) }

type verifierReadiness struct {
	client internalrpcauthorityv1.AuthorizationVerifierServiceClient
}

func (check verifierReadiness) Check(ctx context.Context) error {
	response, err := check.client.CheckReadiness(ctx, &internalrpcauthorityv1.AuthorizationVerifierServiceCheckReadinessRequest{})
	if err != nil || !response.GetReady() {
		return errors.New("STT authorization verifier is not ready")
	}
	return nil
}

type spoolReadiness struct{ directory string }

func (check spoolReadiness) Check(context.Context) error {
	file, err := os.CreateTemp(check.directory, ".readiness-*")
	if err != nil {
		return errors.New("STT spool is not writable")
	}
	name := file.Name()
	closeErr := file.Close()
	removeErr := os.Remove(name)
	return errors.Join(closeErr, removeErr)
}

func checkAll(ctx context.Context, checkers ...checker) error {
	var result error
	for _, dependency := range checkers {
		result = errors.Join(result, dependency.Check(ctx))
	}
	return result
}

type readinessMetrics interface{ SetReady(bool) }

func updateLocalReadiness(ctx context.Context, readiness *serviceruntime.Readiness, metrics readinessMetrics, checkers ...checker) (bool, error) {
	err := checkAll(ctx, checkers...)
	ready := err == nil
	reason := "ready"
	if !ready {
		reason = "local_runtime_unavailable"
	}
	metrics.SetReady(ready)
	return readiness.Set(ready, reason), err
}

func monitorReadiness(readiness *serviceruntime.Readiness, metrics *sharedobservability.Metrics, logger *slog.Logger, timeout time.Duration, checkers ...checker) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
			check, cancel := context.WithTimeout(ctx, timeout)
			changed, err := updateLocalReadiness(check, readiness, metrics, checkers...)
			cancel()
			if err == nil {
				if changed {
					logger.InfoContext(ctx, "STT readiness restored")
				}
			} else {
				if changed {
					logger.WarnContext(ctx, "STT readiness lost", "error_class", "local_runtime")
				}
			}
		}
	}
}

func protectedPathHandler(service *transcriptionservice.Service, timeout time.Duration) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		ctx, cancel := context.WithTimeout(request.Context(), timeout)
		defer cancel()
		ready := service.CheckProtectedPath(ctx) == nil
		status := http.StatusOK
		stage := "ready"
		if !ready {
			status = http.StatusFailedDependency
			stage = "delegated_authority"
		}
		writer.WriteHeader(status)
		_ = json.NewEncoder(writer).Encode(map[string]any{"ready": ready, "stage": stage})
	})
}

func serveGRPC(server *grpc.Server, listener net.Listener) serviceruntime.Worker {
	return func(ctx context.Context) error {
		done := make(chan error, 1)
		go func() {
			err := server.Serve(listener)
			if errors.Is(err, grpc.ErrServerStopped) {
				err = nil
			}
			done <- err
		}()
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func serveHTTP(server *httpserver.Server) serviceruntime.Worker {
	return func(ctx context.Context) error {
		done := make(chan error, 1)
		go func() { done <- server.Serve() }()
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func serverTLS(config Config) (*tls.Config, error) {
	certificate, err := tls.LoadX509KeyPair(config.ServerCertificateFile, config.ServerPrivateKeyFile)
	if err != nil {
		return nil, errors.New("load STT server identity")
	}
	ca, err := os.ReadFile(config.ClientCAFile)
	if err != nil || len(ca) == 0 || len(ca) > 1<<20 {
		return nil, errors.New("read STT client CA")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return nil, errors.New("parse STT client CA")
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate}, ClientCAs: pool, ClientAuth: tls.RequireAndVerifyClientCert,
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.VerifiedChains) == 0 || len(state.VerifiedChains[0]) == 0 {
				return errors.New("STT client certificate is unverified")
			}
			for _, identity := range state.VerifiedChains[0][0].URIs {
				if subtle.ConstantTimeCompare([]byte(identity.String()), []byte(callerSPIFFEID)) == 1 {
					return nil
				}
			}
			return errors.New("STT client identity is invalid")
		}}, nil
}

func normalizeMethod(methods map[string]string, method string) string {
	if name, ok := methods[method]; ok {
		return name
	}
	return "unknown"
}

func normalizeCorrelation(correlationID string) string {
	if correlationID == "" {
		return "unknown"
	}
	return correlationID
}
