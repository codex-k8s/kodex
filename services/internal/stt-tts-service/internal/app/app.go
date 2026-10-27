// Package app composes the STT service's protected RPC, audio/provider adapters, local readiness and bounded process lifecycle.
// RU: Пакет app собирает защищённый RPC STT, адаптеры аудио/провайдера, локальную готовность и ограниченный жизненный цикл процесса.
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
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/clients/projection"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/clients/protectedrpc"
	transcriptionservice "github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/service/transcription"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/integration/audio/ffmpeg"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/integration/provider/openai"
	servicemetrics "github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/observability/metrics"
	transportgrpc "github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/transport/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

const (
	// serviceName identifies the workload when initializing telemetry and its protected dependency clients.
	// RU: serviceName определяет workload при инициализации telemetry и защищённых клиентов зависимостей.
	serviceName = "stt-tts-service"
	// metricsSubsystem fixes this process's metrics namespace independently of request or provider identifiers.
	// RU: metricsSubsystem фиксирует пространство метрик процесса независимо от идентификаторов запроса или провайдера.
	metricsSubsystem = "stt_tts_service"
	// callerSPIFFEID pins the sole gateway caller accepted by the workload certificate and trusted-profile bindings.
	// RU: callerSPIFFEID закрепляет единственного вызывающего gateway для проверки workload-сертификата и привязок trusted-профиля.
	callerSPIFFEID = "spiffe://kodex.local/ns/kodex-system/sa/control-api-gateway"
	// grpcShutdownBudget bounds the complete graceful/forced gRPC stop phase before dependency connections are closed.
	// RU: grpcShutdownBudget ограничивает полную фазу graceful/forced остановки gRPC до закрытия соединений зависимостей.
	grpcShutdownBudget = 22 * time.Second
)

// checker supplies a local startup/readiness probe; remote provider availability is not part of this process readiness contract.
// RU: checker задаёт локальную startup/readiness проверку; доступность удалённого провайдера не входит в контракт готовности процесса.
type checker interface {
	// Check validates its selected local dependency within the supplied context when that dependency supports cancellation.
	// RU: Check проверяет выбранную локальную зависимость в переданном контексте, если зависимость поддерживает отмену.
	Check(context.Context) error
}

// Run wires telemetry, profile-specific authorization, dependency projections, audio decoding and RPC before admitting work at a local startup barrier.
// RU: Run связывает telemetry, профильную авторизацию, проекции зависимостей, декодер аудио и RPC до допуска работы через локальный startup barrier.
// Shutdown clears readiness, stops serving and attempts a bounded worker join before closing dependency/authority clients; later cleanup still runs after an earlier failure.
// RU: Shutdown снимает готовность, останавливает обслуживание и пытается ограниченно дождаться worker до закрытия клиентов зависимостей/authority; ранний отказ не отменяет следующие действия очистки.
func Run(lifecycle, shutdownBase context.Context, buildVersion string) (resultErr error) {
	config, err := loadConfig()
	if err != nil {
		return err
	}
	startup, cancelStartup := context.WithTimeout(lifecycle, config.StartupTimeout)
	defer cancelStartup()
	telemetry, err := sharedobservability.NewRuntimeFromEnv(startup, serviceName, buildVersion)
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, serviceruntime.RunShutdown(context.WithoutCancel(shutdownBase),
			serviceruntime.ShutdownOperation{Name: "STT tracing", Timeout: time.Second, Run: telemetry.ShutdownTracing},
			serviceruntime.ShutdownOperation{Name: "STT Sentry", Timeout: time.Second, Run: telemetry.FlushSentry},
		))
	}()
	logger := telemetry.Logger(os.Stdout)
	methods := map[string]string{
		sttv1.SpeechToTextService_GetModelCatalog_FullMethodName:    "model_catalog",
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
	issuer, err := dialProfileAuthority(startup, config.RPCProfile, authorityclient.LocalConfig{
		SocketPath: config.AuthorityIssuerSocket, ExpectedServerUID: config.AuthorityIssuerUID,
		ExpectedServerGID: config.AuthorityIssuerGID, DialTimeout: config.ReadinessTimeout,
	})
	if err != nil {
		return errors.New("connect STT authorization issuer")
	}
	dependencies, err := protectedrpc.Dial(startup, protectedrpc.Config{
		RPCProfile:      config.RPCProfile,
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
	provider, err := openai.New(config.Egress)
	if err != nil {
		_ = dependencies.Close()
		_ = issuer.Close()
		return err
	}
	domain, err := transcriptionservice.New(policy, credential, provider, outcomes, config.RequestTimeout, ffmpeg.New(config.SpoolDirectory))
	if err != nil {
		_ = dependencies.Close()
		_ = issuer.Close()
		return err
	}
	verifier, err := dialProfileAuthority(startup, config.RPCProfile, authorityclient.LocalConfig{
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
	security, err := newRPCSecurity(config, verifier, dependencies)
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
	serverOptions := []grpc.ServerOption{
		grpc.ForceServerCodec(grpcserver.StrictProtoCodec()),
		grpc.ChainUnaryInterceptor(metrics.UnaryServerInterceptor(), telemetry.UnaryServerInterceptor(methods),
			security.unary, grpcserver.RejectMalformedUnary,
			grpcserver.ErrorBoundary(errorObserver)),
		grpc.ChainStreamInterceptor(metrics.StreamServerInterceptor(), sharedobservability.StreamCorrelationServerInterceptor(),
			telemetry.StreamServerInterceptor(methods), security.stream,
			handler.StreamServerInterceptor(), grpcserver.RejectMalformedStream, grpcserver.StreamErrorBoundary(errorObserver)),
		grpc.MaxConcurrentStreams(uint32(value.MaximumConcurrentStreams)), grpc.MaxRecvMsgSize(68 << 10), grpc.MaxSendMsgSize(1 << 20),
	}
	grpcServer := grpc.NewServer(append(serverOptions, security.options...)...)
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
	localChecks := []checker{domainLocalCheck{domain}, spoolReadiness{config.SpoolDirectory}}
	if config.RPCProfile != transportprofile.TrustedCluster {
		localChecks = append(localChecks, verifierReadiness{verifier.Verifier()})
	}
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
	)
	return errors.Join(resultErr, shutdownErr)
}

// domainLocalCheck exposes only the domain service's local checks to process readiness, not a provider transcription or delegated proof probe.
// RU: domainLocalCheck предоставляет готовности процесса только локальные проверки доменного сервиса, а не распознавание провайдера или проверку делегированного proof.
type domainLocalCheck struct{ service *transcriptionservice.Service }

// Check delegates to CheckLocal without issuing a policy or credential projection RPC itself.
// RU: Check вызывает CheckLocal, сама не отправляя RPC проекции политики или реквизитов.
func (check domainLocalCheck) Check(ctx context.Context) error { return check.service.CheckLocal(ctx) }

// verifierReadiness probes the local authorization verifier's reported readiness in the protected deployment profile.
// RU: verifierReadiness проверяет заявленную готовность локального verifier авторизации в защищённом профиле развёртывания.
type verifierReadiness struct {
	client internalrpcauthorityv1.AuthorizationVerifierServiceClient
}

// Check rejects a failed RPC or a false/nil readiness response with a safe diagnostic, without minting user authority.
// RU: Check отклоняет ошибку RPC или false/nil ответ готовности безопасной диагностикой, не выдавая пользовательских полномочий.
func (check verifierReadiness) Check(ctx context.Context) error {
	response, err := check.client.CheckReadiness(ctx, &internalrpcauthorityv1.AuthorizationVerifierServiceCheckReadinessRequest{})
	if err != nil || !response.GetReady() {
		return errors.New("STT authorization verifier is not ready")
	}
	return nil
}

// spoolReadiness records the local spool directory whose temporary-file lifecycle must be writable for request processing.
// RU: spoolReadiness хранит локальный каталог spool, в котором должен быть доступен жизненный цикл временного файла для обработки запросов.
type spoolReadiness struct{ directory string }

// Check creates, closes and removes one temporary file, returning joined cleanup errors; filesystem calls do not observe the unused context.
// RU: Check создаёт, закрывает и удаляет один временный файл, объединяя ошибки очистки; файловые вызовы не учитывают неиспользуемый контекст.
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

// checkAll runs every supplied check in order and joins failures instead of suppressing later checks after an earlier failure.
// RU: checkAll выполняет все переданные проверки по порядку и объединяет ошибки, не пропуская последующие после раннего отказа.
func checkAll(ctx context.Context, checkers ...checker) error {
	var result error
	for _, dependency := range checkers {
		result = errors.Join(result, dependency.Check(ctx))
	}
	return result
}

// readinessMetrics separates the technical ready gauge from the authoritative precomputed readiness snapshot.
// RU: readinessMetrics отделяет техническую метрику готовности от авторитетного предварительно вычисленного readiness снимка.
type readinessMetrics interface {
	// SetReady publishes the local check result without performing external health checks.
	// RU: SetReady публикует локальный результат без внешних health-проверок.
	SetReady(bool)
}

// updateLocalReadiness derives both the metric and snapshot from the same complete check result and reports whether the snapshot changed.
// RU: updateLocalReadiness выводит метрику и снимок из одного полного результата проверок и сообщает об изменении снимка.
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

// monitorReadiness refreshes local readiness every ten seconds within a check budget and logs only readiness transitions with closed diagnostics.
// RU: monitorReadiness обновляет локальную готовность каждые десять секунд в бюджете проверки и журналирует только переходы с закрытой диагностикой.
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

// protectedPathHandler probes delegated authority on the separate diagnostic route and returns only ready/stage, not credentials or provider details.
// RU: protectedPathHandler проверяет делегированные полномочия на отдельном диагностическом маршруте и возвращает только ready/stage без реквизитов и данных провайдера.
// It is not the Kubernetes local-readiness predicate and the HTTP wrapper owns exact-route restrictions.
// RU: Это не локальный readiness предикат Kubernetes; ограничениями точного HTTP маршрута владеет обёртка сервера.
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

// serveGRPC runs the existing listener in a worker and reports cancellation or serving failure, normalizing ordinary gRPC stop to success.
// RU: serveGRPC обслуживает существующий listener в worker и сообщает отмену или отказ, считая штатную остановку gRPC успехом.
// The composition root owns actual graceful/forced server stop and joining rather than this cancellation branch.
// RU: Настоящими graceful/forced остановкой сервера и ожиданием владеет композиция, а не эта ветвь отмены.
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

// serveHTTP adapts technical HTTP serving to the worker context; cancellation returns before the composition root's separate bounded Shutdown.
// RU: serveHTTP связывает обслуживание технического HTTP с контекстом worker; отмена возвращается до отдельного ограниченного Shutdown в композиции.
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

// serverTLS loads the workload key pair and bounded accepted CA content, requiring TLS 1.3 and a verified client chain with the exact gateway SPIFFE URI.
// RU: serverTLS загружает workload-пару ключей и ограниченное принимаемое содержимое CA, требуя TLS 1.3 и проверенную цепочку клиента с точным SPIFFE URI gateway.
// CA size is checked after reading; certificate transport identity does not replace application authorization.
// RU: Размер CA проверяется после чтения; транспортная идентичность сертификата не заменяет прикладную авторизацию.
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

// normalizeMethod maps a full RPC name to the predeclared bounded method label or the single unknown fallback.
// RU: normalizeMethod отображает полное имя RPC в заранее заданную ограниченную метку метода либо единственное значение unknown.
func normalizeMethod(methods map[string]string, method string) string {
	if name, ok := methods[method]; ok {
		return name
	}
	return "unknown"
}

// normalizeCorrelation replaces only an absent already-established correlation ID with unknown; it does not validate or regenerate nonempty IDs.
// RU: normalizeCorrelation заменяет только отсутствующий уже назначенный correlation ID на unknown; непустые ID функция не проверяет и не создаёт заново.
func normalizeCorrelation(correlationID string) string {
	if correlationID == "" {
		return "unknown"
	}
	return correlationID
}
