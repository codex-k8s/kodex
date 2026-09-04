// Package app содержит единственный composition root stt-tts-service.
package app

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/codex-k8s/kodex/libs/go/grpcserver"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/clients/openai"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/clients/projection"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/clients/protectedrpc"
	transcriptionservice "github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/service/transcription"
	transportgrpc "github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/transport/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
)

const (
	serviceName      = "stt-tts-service"
	metricsSubsystem = "stt_tts_service"
	callerSPIFFEID   = "spiffe://kodex.local/ns/kodex-system/sa/control-api-gateway"
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
		sttv1.SpeechToTextService_Transcribe_FullMethodName:     "transcribe",
		sttv1.SpeechToTextService_CheckReadiness_FullMethodName: "readiness",
	}
	metrics := sharedobservability.NewMetrics(metricsSubsystem, buildVersion, methods)
	defer func() {
		trace, cancelTrace := context.WithTimeout(shutdownBase, 5*time.Second)
		resultErr = errors.Join(resultErr, telemetry.ShutdownTracing(trace))
		cancelTrace()
		sentry, cancelSentry := context.WithTimeout(shutdownBase, 5*time.Second)
		resultErr = errors.Join(resultErr, telemetry.FlushSentry(sentry))
		cancelSentry()
	}()
	dependencies, err := protectedrpc.Dial(startup, protectedrpc.Config{
		Policy:          protectedrpc.TargetConfig{Target: config.PolicyTarget, TLSServerName: config.PolicyTLSServerName, CAFile: config.DependencyCAFile},
		Credential:      protectedrpc.TargetConfig{Target: config.CredentialTarget, TLSServerName: config.CredentialTLSServerName, CAFile: config.DependencyCAFile},
		Resolver:        protectedrpc.TargetConfig{Target: config.ResolverTarget, TLSServerName: config.ResolverTLSServerName, CAFile: config.DependencyCAFile},
		CertificateFile: config.WorkloadCertificateFile, PrivateKeyFile: config.WorkloadPrivateKeyFile,
		ApplicationGrantFile: config.ApplicationGrantFile, ExpectedIssuerUID: config.AuthorityIssuerUID,
		ExpectedIssuerGID: config.AuthorityIssuerGID, DialTimeout: config.ReadinessTimeout,
	})
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, dependencies.Close()) }()
	policy, err := projection.NewPolicy(dependencies.Policy)
	if err != nil {
		return err
	}
	credential, err := projection.NewCredential(dependencies.Credential)
	if err != nil {
		return err
	}
	provider, err := openai.New()
	if err != nil {
		return err
	}
	domain, err := transcriptionservice.New(policy, credential, provider, config.RequestTimeout)
	if err != nil {
		return err
	}
	verifier, err := authorityclient.DialLocal(startup, authorityclient.LocalConfig{
		SocketPath: config.AuthorityVerifierSocket, ExpectedServerUID: config.AuthorityVerifierUID,
		ExpectedServerGID: config.AuthorityVerifierGID, DialTimeout: config.ReadinessTimeout,
	})
	if err != nil {
		return errors.New("connect STT authorization verifier")
	}
	defer func() { resultErr = errors.Join(resultErr, verifier.Close()) }()
	handler, err := transportgrpc.New(domain)
	if err != nil {
		return err
	}
	tlsConfig, err := serverTLS(config)
	if err != nil {
		return err
	}
	grpcServer := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(tlsConfig)), grpc.ForceServerCodec(grpcserver.StrictProtoCodec()),
		grpc.ChainUnaryInterceptor(
			metrics.UnaryServerInterceptor(), telemetry.UnaryServerInterceptor(methods),
			authorityclient.VerifierUnaryServerInterceptor(verifier.Verifier()),
			grpcserver.RejectMalformedUnary,
			grpcserver.ErrorBoundary(grpcserver.ErrorObserverFunc(func(_ context.Context, method string, code codes.Code, _ error) {
				logger.Error("unexpected gRPC failure", "method", normalizeMethod(methods, method), "code", code.String())
			})),
		),
		grpc.MaxRecvMsgSize(26<<20), grpc.MaxSendMsgSize(1<<20),
	)
	sttv1.RegisterSpeechToTextServiceServer(grpcServer, handler)
	listener, err := net.Listen("tcp", config.GRPCListen)
	if err != nil {
		return errors.New("listen for STT gRPC")
	}
	readiness := serviceruntime.NewReadiness()
	readiness.Set(false, "infrastructure_starting")
	metrics.SetReady(false)
	technical := technicalServer(lifecycle, config, readiness, metrics)
	verifierCheck := verifierReadiness{client: verifier.Verifier()}
	checks := []checker{domain, dependencies, verifierCheck}
	if err := checkAll(startup, checks...); err != nil {
		_ = listener.Close()
		return errors.Join(errors.New("STT startup barrier failed"), err)
	}
	readiness.Set(true, "ready")
	metrics.SetReady(true)
	workers := serviceruntime.StartWorkers(lifecycle, serveGRPC(grpcServer, listener), serveHTTP(technical),
		monitorReadiness(readiness, metrics, logger, config.ReadinessTimeout, checks...))
	err = workers.Wait(context.WithoutCancel(lifecycle))
	shutdownErr := serviceruntime.RunShutdown(shutdownBase,
		serviceruntime.ShutdownOperation{Name: "STT gRPC server", Timeout: config.ShutdownTimeout, Run: func(context.Context) error { grpcServer.GracefulStop(); return nil }},
		serviceruntime.ShutdownOperation{Name: "STT technical HTTP server", Timeout: config.ShutdownTimeout, Run: technical.Shutdown},
		serviceruntime.ShutdownOperation{Name: "STT workers", Timeout: config.ShutdownTimeout, Run: workers.Wait},
	)
	return errors.Join(err, shutdownErr)
}

type verifierReadiness struct {
	client internalrpcauthorityv1.AuthorizationVerifierServiceClient
}

func (checker verifierReadiness) Check(ctx context.Context) error {
	response, err := checker.client.CheckReadiness(ctx, &internalrpcauthorityv1.AuthorizationVerifierServiceCheckReadinessRequest{})
	if err != nil || !response.GetReady() {
		return errors.New("STT authorization verifier is not ready")
	}
	return nil
}

func checkAll(ctx context.Context, checkers ...checker) error {
	var result error
	for _, dependency := range checkers {
		result = errors.Join(result, dependency.Check(ctx))
	}
	return result
}

func monitorReadiness(readiness *serviceruntime.Readiness, metrics *sharedobservability.Metrics, logger *slog.Logger, timeout time.Duration, checkers ...checker) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			check, cancel := context.WithTimeout(ctx, timeout)
			err := checkAll(check, checkers...)
			cancel()
			if err == nil {
				metrics.SetReady(true)
				if readiness.Set(true, "ready") {
					logger.InfoContext(ctx, "STT readiness restored")
				}
			} else {
				metrics.SetReady(false)
				if readiness.Set(false, "dependency_unavailable") {
					logger.WarnContext(ctx, "STT readiness lost", "error_class", "dependency")
				}
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}
}

func technicalServer(lifecycle context.Context, config Config, readiness *serviceruntime.Readiness, metrics *sharedobservability.Metrics) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/readyz", func(writer http.ResponseWriter, _ *http.Request) {
		ready, reason := readiness.Ready()
		if !ready {
			http.Error(writer, reason, http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("/metrics", metrics.PrometheusHandler())
	return &http.Server{Addr: config.TechnicalListen, Handler: mux, BaseContext: func(net.Listener) context.Context { return lifecycle }, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
}

func serveGRPC(server *grpc.Server, listener net.Listener) serviceruntime.Worker {
	return func(ctx context.Context) error {
		done := make(chan error, 1)
		go func() { done <- server.Serve(listener) }()
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			server.GracefulStop()
			return <-done
		}
	}
}
func serveHTTP(server *http.Server) serviceruntime.Worker {
	return func(ctx context.Context) error {
		done := make(chan error, 1)
		go func() { done <- server.ListenAndServe() }()
		select {
		case err := <-done:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		case <-ctx.Done():
			_ = server.Close()
			return nil
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
		},
	}, nil
}

func normalizeMethod(methods map[string]string, method string) string {
	if name, ok := methods[method]; ok {
		return name
	}
	return "unknown"
}
