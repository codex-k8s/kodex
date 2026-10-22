package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/codex-k8s/kodex/libs/go/grpcserver"
	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/udscred"
	"github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/application"
	readbackclient "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/client/readback"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/client/restoreagent"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/service"
	authorityrepository "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/repository/postgres/authority"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/snapshot"
	authoritygrpc "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/transport/grpc"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// maxDSNFileBytes bounds private PostgreSQL DSN reads to sixteen KiB before pool parsing; it is a file-size limit, not a database statement or connection budget.
	// maxDSNFileBytes ограничивает чтение приватного файла PostgreSQL DSN шестнадцатью КиБ до разбора пула; это предел файла, а не бюджет запросов БД или соединений.
	maxDSNFileBytes = 16 << 10
	// snapshotReadbackRefreshInterval schedules served-snapshot readback refresh every ten seconds. It does not extend the authoritative receipt expiry; readiness still consults the current served-state check.
	// snapshotReadbackRefreshInterval задаёт обновление readback обслуживаемого снимка каждые десять секунд. Авторитетный срок квитанции не продлевается; готовность по-прежнему обращается к проверке текущего обслуживаемого состояния.
	snapshotReadbackRefreshInterval = 10 * time.Second
	// logMessageStart is the fixed English runtime-start log message emitted after workers and servers are launched, with mode and workload as separate fields.
	// logMessageStart — фиксированное английское сообщение запуска runtime, выводимое после запуска исполнителей и серверов с отдельными полями режима и workload.
	logMessageStart = "internal-rpc-authority runtime started"
	// logMessageStop is the fixed English runtime-stop message emitted after the ordered shutdown sequence, even when that sequence returns collected errors.
	// logMessageStop — фиксированное английское сообщение остановки runtime после упорядоченного завершения, даже если завершение возвращает собранные ошибки.
	logMessageStop = "internal-rpc-authority runtime stopped"
)

// snapshotMaintenance is the narrow port for checking the current served state and refreshing activation/readback. It deliberately exposes no method to rewrite a receipt deadline or replace policy from an unverified caller payload.
// snapshotMaintenance — узкий порт проверки текущего обслуживаемого состояния и обновления активации или readback. Метода переписать срок квитанции или заменить политику непроверенным payload вызывающего нет.
type snapshotMaintenance interface {
	// ServedStateReady checks the already served snapshot under the supplied context; callers use its error to decide whether bounded last-known-good state remains eligible, not to mint a new receipt.
	// ServedStateReady проверяет уже обслуживаемый снимок с переданным контекстом; вызывающие используют ошибку для решения о допустимости ограниченного прежнего состояния, а не для создания новой квитанции.
	ServedStateReady(context.Context) error
	// ActivateSnapshot attempts activation/readback refresh under the supplied context and returns failure without giving callers permission to extend an old receipt locally.
	// ActivateSnapshot пытается выполнить активацию или обновление readback с переданным контекстом и возвращает ошибку без разрешения вызывающему локально продлевать старую квитанцию.
	ActivateSnapshot(context.Context) error
}

// authorityReadinessCondition names the closed local readiness inputs for snapshot maintenance and replay cleanup; arbitrary worker labels are not registered dynamically.
// authorityReadinessCondition именует закрытые локальные условия готовности обслуживания снимка и очистки replay; произвольные метки исполнителей динамически не регистрируются.
type authorityReadinessCondition string

const (
	// conditionSnapshot is the readiness input controlled by snapshot loading, activation and served-state maintenance, separately from replay cleanup health.
	// conditionSnapshot — условие готовности, которым управляют загрузка, активация и поддержание обслуживаемого снимка отдельно от состояния очистки replay.
	conditionSnapshot authorityReadinessCondition = "snapshot"
	// conditionReplay is the readiness input for the reservation cleanup worker, so its failure cannot be hidden by successful snapshot maintenance.
	// conditionReplay — условие готовности исполнителя очистки резервирований, чтобы его отказ не скрывался успешным обслуживанием снимка.
	conditionReplay authorityReadinessCondition = "replay_cleanup"
)

// authorityReadinessOrder fixes both the registered condition set and deterministic first-failure diagnostic precedence: snapshot before replay cleanup.
// authorityReadinessOrder фиксирует набор зарегистрированных условий и детерминированный приоритет диагностики первого отказа: снимок перед очисткой replay.
var authorityReadinessOrder = [...]authorityReadinessCondition{conditionSnapshot, conditionReplay}

// authorityReadiness synchronizes local condition updates and publishes their conjunction to the shared readiness snapshot and metrics. It stores technical health flags, not authorization policy or durable receipt state.
// authorityReadiness синхронизирует обновления локальных условий и публикует их конъюнкцию в общий снимок готовности и метрики. Хранятся технические признаки здоровья, а не политика полномочий или устойчивое состояние квитанций.
type authorityReadiness struct {
	mutex      sync.Mutex
	snapshot   *serviceruntime.Readiness
	metrics    *observability.Metrics
	conditions map[authorityReadinessCondition]bool
}

// newAuthorityReadiness initializes every registered local condition as true and immediately publishes that aggregate. It assumes nonnil readiness/metrics dependencies and does not actively probe PostgreSQL, a snapshot or a worker during construction.
// newAuthorityReadiness задаёт всем зарегистрированным локальным условиям true и немедленно публикует агрегат. Ожидаются ненулевые зависимости готовности и метрик; PostgreSQL, снимок и исполнители при создании активно не проверяются.
func newAuthorityReadiness(snapshot *serviceruntime.Readiness, metrics *observability.Metrics) *authorityReadiness {
	conditions := make(map[authorityReadinessCondition]bool, len(authorityReadinessOrder))
	for _, condition := range authorityReadinessOrder {
		conditions[condition] = true
	}
	state := &authorityReadiness{snapshot: snapshot, metrics: metrics, conditions: conditions}
	state.publishLocked()
	return state
}

// Set changes one registered readiness condition under the mutex and returns true only when that condition's value changes. Unknown conditions and unchanged values return false; publishing the conjunction prevents one worker's success from clearing another worker's failure.
// Set меняет одно зарегистрированное условие готовности под mutex и возвращает true только при изменении его значения. Неизвестное условие и прежнее значение дают false; публикация конъюнкции не позволяет успеху одного исполнителя убрать отказ другого.
func (state *authorityReadiness) Set(condition authorityReadinessCondition, ready bool) bool {
	state.mutex.Lock()
	defer state.mutex.Unlock()
	previous, exists := state.conditions[condition]
	if !exists || previous == ready {
		return false
	}
	state.conditions[condition] = ready
	state.publishLocked()
	return true
}

// publishLocked publishes the conjunction and deterministic first failing reason to both the readiness object and ready metric. Callers hold the mutex, except during construction before the object is shared; no external health probe runs here.
// publishLocked публикует конъюнкцию и детерминированную причину первого отказа в объект готовности и метрику ready. Вызывающий держит mutex, кроме создания до общего доступа к объекту; внешняя проверка здоровья здесь не выполняется.
func (state *authorityReadiness) publishLocked() {
	ready, reason := true, "ready"
	for _, condition := range authorityReadinessOrder {
		if !state.conditions[condition] {
			ready, reason = false, string(condition)+"_unavailable"
			break
		}
	}
	state.snapshot.Set(ready, reason)
	state.metrics.SetReady(ready)
}

// Run composes the exact issuer/verifier profile through configuration, telemetry, capability PostgreSQL, verified snapshot/service, readback delivery and restore coordination, then passes startup restore/activation barriers before launching local servers and workers. Returned failures retain the startup/runtime stage; normal shutdown attempts gRPC stop, worker cancel/join, HTTP stop, client/delivery/database closure and independent telemetry cleanup in order, collecting errors. The caller owns lifecycle and shutdown-base contexts; this function neither creates a root context nor proves successful startup merely from constructing configuration.
// Run собирает точный профиль issuer или verifier через конфигурацию, телеметрию, PostgreSQL с ролью возможности, проверенный снимок и сервис, доставку readback и координацию восстановления, затем проходит стартовые барьеры восстановления и активации до запуска локальных серверов и исполнителей. Возвращённые ошибки сохраняют этап запуска или runtime; штатное завершение последовательно пытается остановить gRPC, отменить и дождаться исполнителей, остановить HTTP, закрыть клиенты, доставку и БД и независимо завершить телеметрию, собирая ошибки. Контексты жизненного цикла и базы завершения принадлежат вызывающему; корневой контекст не создаётся, сборка конфигурации не считается успешным стартом.
func Run(
	lifecycle context.Context,
	shutdownBase context.Context,
	mode Mode,
	buildVersion string,
) (runErr error) {
	stage := stageConfiguration
	defer func() {
		if runErr != nil {
			runErr = &runtimeStageError{stage: stage, cause: runErr}
		}
	}()
	config, err := LoadConfig(mode)
	if err != nil {
		return fmt.Errorf("load runtime configuration: %w", err)
	}
	stage = stageObservability
	telemetry, logger, err := startTelemetry(
		lifecycle,
		config.ServiceName,
		buildVersion,
	)
	if err != nil {
		return fmt.Errorf("start observability runtime: %w", err)
	}
	telemetryFinished := false
	defer func() {
		if !telemetryFinished {
			telemetry.cleanupAfterStartupFailure(shutdownBase, config.ShutdownTimeout)
		}
	}()
	startupCtx, startupCancel := context.WithTimeout(lifecycle, config.StartupTimeout)
	defer startupCancel()
	stage = stagePostgres
	pool, err := openPostgres(startupCtx, config)
	if err != nil {
		return err
	}
	readinessReservationKind := repository.ReservationAuthorizationContext
	if config.Mode == ModeIssuer {
		readinessReservationKind = repository.ReservationAuthorityProof
	}
	store, err := authorityrepository.New(
		pool,
		config.WorkloadID,
		readinessReservationKind,
	)
	if err != nil {
		pool.Close()
		return fmt.Errorf("construct authority repository: %w", err)
	}
	stage = stageSnapshotLoad
	loaded, err := snapshot.Load(snapshot.LoadOptions{
		Role:                       snapshot.Role(config.Mode),
		WorkloadID:                 config.WorkloadID,
		SnapshotJWSFile:            config.SnapshotJWSFile,
		ManifestRootPublicJWKFile:  config.ManifestRootPublicJWKFile,
		ManifestRootMetadataFile:   config.ManifestRootMetadataFile,
		ManifestTrustBundleJWSFile: config.ManifestTrustBundleJWSFile,
		ContextPrivateJWKFile:      config.ContextPrivateJWKFile,
		ProofTrustJWKFile:          config.ProofTrustJWKFile,
		Now:                        time.Now(),
	})
	if err != nil {
		store.Close()
		return fmt.Errorf("load served authority snapshot: %w", err)
	}
	stage = stageAuthorityConstruction
	domainService, err := service.NewAuthority(loaded.Policy, loaded.Keys, store)
	if err != nil {
		store.Close()
		return fmt.Errorf("construct authority domain service: %w", err)
	}
	delivery, err := newRuntimeSecretDelivery(config)
	if err != nil {
		store.Close()
		return fmt.Errorf("construct authority material delivery: %w", err)
	}
	stage = stageReadbackClient
	readbackTLS, err := loadRestoreClientTLS(
		config.ReadbackAttestorCAFile,
		config.WorkloadCertificateFile,
		config.WorkloadPrivateKeyFile,
		config.ReadbackAttestorTLSServerName,
	)
	if err != nil {
		delivery.Close()
		store.Close()
		return fmt.Errorf("load readback attestor mTLS client: %w", err)
	}
	snapshotAttestor, err := readbackclient.NewSecretAttestor(readbackclient.SecretConfig{
		Address: config.ReadbackAttestorAddress, TLS: readbackTLS,
		CredentialPath:          config.ReadbackCredentialSecret,
		PossessionPath:          config.ReadbackPossessionSecret,
		Delivery:                delivery,
		WorkloadID:              config.WorkloadID,
		WorkloadSPIFFEID:        config.WorkloadSPIFFEID,
		Role:                    config.ReadbackRole,
		WorkloadGeneration:      config.WorkloadGeneration,
		CredentialGeneration:    config.CredentialGeneration,
		PossessionKeyGeneration: config.PossessionKeyGeneration,
		UnaryInterceptor: telemetry.UnaryClientInterceptor(map[string]string{
			internalrpcauthorityv1.AuthorityReadbackAttestorService_IssueAttestationChallenge_FullMethodName: "issue_attestation_challenge",
			internalrpcauthorityv1.AuthorityReadbackAttestorService_AttestServedState_FullMethodName:         "attest_served_state",
		}),
	})
	if err != nil {
		delivery.Close()
		store.Close()
		return fmt.Errorf("construct readback attestor client: %w", err)
	}
	readbackAttestors := []*readbackclient.SecretAttestor{snapshotAttestor}
	closeReadbackAttestors := func() error {
		var closeErrors []error
		for _, attestor := range readbackAttestors {
			if closeErr := attestor.Close(); closeErr != nil {
				closeErrors = append(closeErrors, closeErr)
			}
		}
		readbackAttestors = nil
		return errors.Join(closeErrors...)
	}
	defer func() {
		_ = closeReadbackAttestors()
	}()
	var activationAttestor repository.SnapshotAttestor = snapshotAttestor
	if config.Mode == ModeVerifier && config.ResolverEnabled {
		resolverAttestor, resolverErr := readbackclient.NewSecretAttestor(
			readbackclient.SecretConfig{
				Address: config.ReadbackAttestorAddress, TLS: readbackTLS,
				CredentialPath:          config.ResolverReadbackCredentialSecret,
				PossessionPath:          config.ResolverReadbackPossessionSecret,
				Delivery:                delivery,
				WorkloadID:              config.WorkloadID,
				WorkloadSPIFFEID:        config.WorkloadSPIFFEID,
				Role:                    "AUTHORITY_PROOF_RESOLVER",
				WorkloadGeneration:      config.WorkloadGeneration,
				CredentialGeneration:    config.ResolverCredentialGeneration,
				PossessionKeyGeneration: config.ResolverPossessionKeyGeneration,
				UnaryInterceptor: telemetry.UnaryClientInterceptor(map[string]string{
					internalrpcauthorityv1.AuthorityReadbackAttestorService_IssueAttestationChallenge_FullMethodName: "issue_attestation_challenge",
					internalrpcauthorityv1.AuthorityReadbackAttestorService_AttestServedState_FullMethodName:         "attest_served_state",
				}),
			},
		)
		if resolverErr != nil {
			delivery.Close()
			store.Close()
			return fmt.Errorf(
				"construct authority proof resolver readback client: %w",
				resolverErr,
			)
		}
		readbackAttestors = append(readbackAttestors, resolverAttestor)
		activationAttestor = &proofResolverAttestor{
			primary:              snapshotAttestor,
			resolver:             resolverAttestor,
			privateJWKFile:       config.ResolverProofPrivateJWKFile,
			signerGenerationFile: config.ResolverProofSignerGenerationFile,
			proofTrustFile:       config.ResolverProofTrustJWKFile,
			issuer:               config.WorkloadSPIFFEID,
			now:                  time.Now,
		}
	}
	authorityApplication, err := application.NewAuthority(
		domainService,
		activationAttestor,
	)
	if err != nil {
		delivery.Close()
		store.Close()
		return fmt.Errorf("construct authority application: %w", err)
	}
	stage = stageRestoreClient
	restoreTLS, err := loadRestoreClientTLS(
		config.RestoreControllerCAFile,
		config.WorkloadCertificateFile,
		config.WorkloadPrivateKeyFile,
		config.RestoreControllerTLSServerName,
	)
	if err != nil {
		delivery.Close()
		store.Close()
		return fmt.Errorf("load restore controller mTLS client: %w", err)
	}
	restoreWorkloadAgent, err := restoreagent.New(restoreagent.Config{
		Address: config.RestoreControllerAddress, TLS: restoreTLS,
		RoleCredentialSecret:       config.RestoreRoleCredentialSecret,
		ACKPrivateJWKSecret:        config.RestoreACKSecret,
		Delivery:                   delivery,
		ControllerCertificateFile:  config.RestoreControllerCertificateFile,
		ManifestRootPublicJWKFile:  config.ManifestRootPublicJWKFile,
		ManifestRootMetadataFile:   config.ManifestRootMetadataFile,
		ManifestTrustBundleJWSFile: config.ManifestTrustBundleJWSFile,
		RestoreRoleTrustJWSFile:    config.RestoreRoleTrustJWSFile,
		WorkloadID:                 config.WorkloadID, WorkloadSPIFFEID: config.WorkloadSPIFFEID,
		Role: config.ReadbackRole, WorkloadGeneration: config.WorkloadGeneration,
		CredentialGeneration: config.CredentialGeneration,
		ACKKeyGeneration:     config.RestoreACKKeyGeneration,
		UnaryInterceptor: telemetry.UnaryClientInterceptor(map[string]string{
			internalrpcauthorityv1.RestoreControllerService_GetRestoreDirective_FullMethodName:   "get_restore_directive",
			internalrpcauthorityv1.RestoreControllerService_AcknowledgeQuiescence_FullMethodName: "acknowledge_quiescence",
		}),
	})
	if err != nil {
		delivery.Close()
		store.Close()
		return fmt.Errorf("construct restore workload agent: %w", err)
	}
	stage = stageRestoreVerify
	if err := restoreWorkloadAgent.VerifyStartup(
		startupCtx,
		authorityApplication,
	); err != nil {
		delivery.Close()
		store.Close()
		return fmt.Errorf("verify external restore startup barrier: %w", err)
	}
	stage = stageSnapshotActivation
	if err := activateSnapshotUntilReady(startupCtx, authorityApplication); err != nil {
		delivery.Close()
		store.Close()
		return fmt.Errorf("activate served authority snapshot: %w", err)
	}
	stage = stageRestoreOpen
	if err := restoreWorkloadAgent.Poll(
		startupCtx,
		authorityApplication,
	); err != nil {
		delivery.Close()
		store.Close()
		return fmt.Errorf("open external restore startup barrier: %w", err)
	}
	stage = stageLocalServers
	metrics := observability.NewMetrics(
		config.ServiceName,
		buildVersion,
		allowedMethods(mode),
	)
	readiness := serviceruntime.NewReadiness()
	readiness.Set(false, "starting")
	metrics.SetReady(false)
	localReadiness := newAuthorityReadiness(readiness, metrics)
	errorObserver := grpcserver.ErrorObserverFunc(func(
		_ context.Context,
		method string,
		code codes.Code,
		err error,
	) {
		logger.Error(
			"unexpected gRPC failure",
			"method", normalizedMethod(mode, method),
			"code", code.String(),
			"grpc_message", status.Convert(err).Message(),
		)
	})
	grpcRuntime := grpc.NewServer(
		grpc.Creds(udscred.New(config.ExpectedPeerUID, config.ExpectedPeerGID)),
		grpc.ForceServerCodec(grpcserver.StrictProtoCodec()),
		grpc.ChainUnaryInterceptor(
			metrics.UnaryServerInterceptor(),
			telemetry.UnaryServerInterceptor(allowedMethods(mode)),
			grpcserver.ErrorBoundary(errorObserver),
		),
	)
	switch mode {
	case ModeIssuer:
		internalrpcauthorityv1.RegisterAuthorizationIssuerServiceServer(
			grpcRuntime,
			authoritygrpc.NewIssuerServer(authorityApplication),
		)
	case ModeVerifier:
		internalrpcauthorityv1.RegisterAuthorizationVerifierServiceServer(
			grpcRuntime,
			authoritygrpc.NewVerifierServer(authorityApplication),
		)
	}
	unixListener, err := listenUnix(config)
	if err != nil {
		store.Close()
		delivery.Close()
		return err
	}
	technicalListener, err := net.Listen("tcp", config.TechnicalListen)
	if err != nil {
		_ = unixListener.Close()
		store.Close()
		delivery.Close()
		return fmt.Errorf("listen on technical HTTP endpoint: %w", err)
	}
	technicalServer := newTechnicalServer(
		config,
		readiness,
		metrics,
		authorityApplication,
	)
	stage = stageRuntime
	workers := serviceruntime.StartWorkers(
		lifecycle,
		func(ctx context.Context) error {
			runSnapshotReload(
				ctx,
				config,
				store,
				authorityApplication,
				localReadiness,
				logger,
			)
			return nil
		},
		func(ctx context.Context) error {
			runReplayCleanup(ctx, config, store, localReadiness, logger)
			return nil
		},
		func(ctx context.Context) error {
			runRestoreWorkloadAgent(
				ctx,
				config,
				restoreWorkloadAgent,
				authorityApplication,
				logger,
			)
			return nil
		},
	)
	serveErrors := make(chan error, 2)
	go func() {
		if serveErr := grpcRuntime.Serve(unixListener); serveErr != nil {
			serveErrors <- fmt.Errorf("serve authority gRPC: %w", serveErr)
		}
	}()
	go func() {
		if serveErr := technicalServer.Serve(technicalListener); serveErr != nil &&
			!errors.Is(serveErr, http.ErrServerClosed) {
			serveErrors <- fmt.Errorf("serve technical HTTP: %w", serveErr)
		}
	}()
	logger.Info(logMessageStart, "mode", string(mode), "workload", config.WorkloadID)
	var runtimeErr error
	select {
	case <-lifecycle.Done():
	case runtimeErr = <-serveErrors:
	}
	readiness.Set(false, "shutting-down")
	metrics.SetReady(false)
	shutdownErr := serviceruntime.RunShutdown(
		shutdownBase,
		telemetry.shutdownOperations(config.ShutdownTimeout,
			serviceruntime.ShutdownOperation{
				Name:    "grpc-server",
				Timeout: config.ShutdownTimeout,
				Run: func(ctx context.Context) error {
					return stopGRPC(ctx, grpcRuntime)
				},
			},
			serviceruntime.ShutdownOperation{
				Name:    "workers",
				Timeout: config.ShutdownTimeout,
				Run: func(ctx context.Context) error {
					workers.Stop()
					return workers.Wait(ctx)
				},
			},
			serviceruntime.ShutdownOperation{
				Name:    "technical-http",
				Timeout: config.ShutdownTimeout,
				Run:     technicalServer.Shutdown,
			},
			serviceruntime.ShutdownOperation{
				Name:    "readback-attestors",
				Timeout: config.ShutdownTimeout,
				Run: func(context.Context) error {
					return closeReadbackAttestors()
				},
			},
			serviceruntime.ShutdownOperation{
				Name:    "secret-delivery",
				Timeout: config.ShutdownTimeout,
				Run: func(context.Context) error {
					delivery.Close()
					return nil
				},
			},
			serviceruntime.ShutdownOperation{
				Name:    "postgresql",
				Timeout: config.ShutdownTimeout,
				Run: func(context.Context) error {
					store.Close()
					return nil
				},
			},
		)...,
	)
	telemetryFinished = true
	logger.Info(logMessageStop, "mode", string(mode))
	return errors.Join(runtimeErr, shutdownErr)
}

// reservationCleaner is the storage port used by the runtime's bounded expiry cleanup worker; it does not expose arbitrary reservation mutation or another workload's database.
// reservationCleaner — порт хранилища для ограниченного исполнителя очистки истёкших резервирований; произвольное изменение резервирований или БД другого workload не предоставляется.
type reservationCleaner interface {
	// DeleteExpired removes eligible reservations of the specified kind before the supplied cutoff under the request context; repository ownership and retention eligibility remain behind this port.
	// DeleteExpired удаляет допустимые резервирования указанного вида до переданной границы с контекстом запроса; владение и допустимость retention остаются за портом репозитория.
	DeleteExpired(context.Context, repository.ReservationKind, time.Time) error
}

// runRestoreWorkloadAgent polls restore coordination immediately and then on two-second ticks with a bounded readiness context per poll. Failure blocks the authority application and logs only availability edges; successful polling is delegated to the agent's coordination logic, and context cancellation stops the worker rather than creating a new root.
// runRestoreWorkloadAgent опрашивает координацию восстановления сразу и затем по двухсекундным тикам с ограниченным контекстом готовности на опрос. Ошибка блокирует приложение authority и журналирует только смены доступности; успешный опрос передан логике координации агента, отмена контекста прекращает исполнителя без создания нового корня.
func runRestoreWorkloadAgent(
	ctx context.Context,
	config Config,
	agent *restoreagent.Agent,
	authorityApplication *application.Authority,
	logger *slog.Logger,
) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	available := true
	for {
		pollCtx, cancel := context.WithTimeout(ctx, config.ReadinessTimeout)
		err := agent.Poll(pollCtx, authorityApplication)
		cancel()
		if err != nil {
			authorityApplication.SetRestoreBlocked(true)
			if available {
				logger.Error("restore coordination unavailable", "error_class", "restore_controller")
			}
			available = false
		} else {
			if !available {
				logger.Info("restore coordination restored")
			}
			available = true
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// runReplayCleanup selects authority-proof reservations for issuer or authorization-context reservations otherwise, deletes entries older than expiry plus configured retention, and updates only the replay readiness condition. It runs once before waiting for each configured tick, bounds every call and logs failure/recovery edges without exposing database error text.
// runReplayCleanup выбирает резервирования authority-proof для issuer либо authorization-context иначе, удаляет записи старше истечения плюс настроенный retention и обновляет только условие готовности replay. Один проход выполняется до ожидания каждого настроенного тика, каждый вызов ограничен, смены отказа и восстановления журналируются без текста ошибки БД.
func runReplayCleanup(
	ctx context.Context,
	config Config,
	cleaner reservationCleaner,
	readiness *authorityReadiness,
	logger *slog.Logger,
) {
	kind := repository.ReservationAuthorizationContext
	if config.Mode == ModeIssuer {
		kind = repository.ReservationAuthorityProof
	}
	ticker := time.NewTicker(config.ReplayCleanupInterval)
	defer ticker.Stop()
	for {
		cleanupCtx, cancel := context.WithTimeout(ctx, config.ReadinessTimeout)
		err := cleaner.DeleteExpired(
			cleanupCtx,
			kind,
			time.Now().UTC().Add(-config.ReplayRetentionAfterExpiry),
		)
		cancel()
		if err != nil {
			if readiness.Set(conditionReplay, false) {
				logger.Error("replay reservation cleanup unavailable", "error_class", "postgresql")
			}
		} else if readiness.Set(conditionReplay, true) {
			logger.Info("replay reservation cleanup restored")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// activateSnapshotUntilReady retries every failed activation under the caller's startup context with delays of one, two, four, eight and at most ten seconds. Success returns immediately; cancellation stops the timer and joins the last activation error with ctx.Err rather than inventing a successful receipt or running an unbounded background retry.
// activateSnapshotUntilReady повторяет любую неудачную активацию в стартовом контексте вызывающего с задержками одна, две, четыре, восемь и максимум десять секунд. Успех возвращается сразу; отмена останавливает таймер и объединяет последнюю ошибку активации с ctx.Err вместо создания успешной квитанции или неограниченного фонового повтора.
func activateSnapshotUntilReady(
	ctx context.Context,
	authorityApplication *application.Authority,
) error {
	// maximumRetryInterval caps only activation backoff; the caller's startup context still bounds the complete retry sequence.
	// maximumRetryInterval ограничивает только задержку активации; полный ряд повторов по-прежнему ограничен стартовым контекстом вызывающего.
	const maximumRetryInterval = 10 * time.Second
	retryInterval := time.Second
	for {
		err := authorityApplication.ActivateSnapshot(ctx)
		if err == nil {
			return nil
		}
		timer := time.NewTimer(retryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.Join(err, ctx.Err())
		case <-timer.C:
		}
		retryInterval = min(retryInterval*2, maximumRetryInterval)
	}
}

// runSnapshotReload reloads and verifies snapshot material on configured ticks, rejects file/load failures by closing availability, and compares the exact served revision/digest/key/policy/signer tuple. Unchanged material uses bounded readback maintenance; changed material is constructed and activated as a replacement, retaining an old snapshot only for the permitted transient-error class while current served readiness succeeds. It preserves refresh timestamps on deferred refresh, publishes edge diagnostics and stops on the worker context; local scheduling never rewrites authoritative receipt expiry.
// runSnapshotReload по настроенным тикам перечитывает и проверяет материал снимка, закрывает доступность при ошибке файла или загрузки и сравнивает точный набор обслуживаемых ревизии, дайджеста, версии ключей, политики и подписанта. Неизменный материал использует ограниченное обслуживание readback; изменённый создаётся и активируется как замена, а старый снимок сохраняется только для разрешённого временного класса ошибки при успешной проверке текущей готовности. При отложенном обновлении сохраняются прежние времена refresh, публикуются диагностики смен состояний, контекст исполнителя прекращает цикл; локальное расписание не переписывает авторитетный срок квитанции.
func runSnapshotReload(
	ctx context.Context,
	config Config,
	store *authorityrepository.Store,
	authorityApplication *application.Authority,
	readiness *authorityReadiness,
	logger *slog.Logger,
) {
	ticker := time.NewTicker(config.SnapshotReloadInterval)
	defer ticker.Stop()
	lastReadbackRefresh := time.Now()
	readbackRefreshDeferred := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		current := authorityApplication.SnapshotState()
		loaded, err := snapshot.Load(snapshot.LoadOptions{
			Role:                       snapshot.Role(config.Mode),
			WorkloadID:                 config.WorkloadID,
			SnapshotJWSFile:            config.SnapshotJWSFile,
			ManifestRootPublicJWKFile:  config.ManifestRootPublicJWKFile,
			ManifestRootMetadataFile:   config.ManifestRootMetadataFile,
			ManifestTrustBundleJWSFile: config.ManifestTrustBundleJWSFile,
			ContextPrivateJWKFile:      config.ContextPrivateJWKFile,
			ProofTrustJWKFile:          config.ProofTrustJWKFile,
			Now:                        time.Now(),
		})
		if err != nil {
			authorityApplication.SetAvailable(false)
			if readiness.Set(conditionSnapshot, false) {
				logger.Error("authority snapshot reload rejected", "error_class", "snapshot")
			}
			continue
		}
		if loaded.Policy.SourceRevision == current.SourceRevision &&
			loaded.Policy.SourceDigestSHA256 == current.SourceDigestSHA256 &&
			loaded.Policy.KeySetRevision == current.KeySetRevision &&
			loaded.Policy.PolicyRevision == current.PolicyRevision &&
			loaded.Policy.SignerGeneration == current.SignerGeneration {
			probeCtx, probeCancel := context.WithTimeout(ctx, config.ReadinessTimeout)
			previousReadbackRefresh := lastReadbackRefresh
			lastReadbackRefresh, err = maintainServedSnapshot(
				probeCtx,
				authorityApplication,
				lastReadbackRefresh,
				time.Now(),
			)
			probeCancel()
			if err == nil {
				authorityApplication.SetAvailable(true)
				if readiness.Set(conditionSnapshot, true) {
					logger.Info("authority snapshot readiness restored")
				}
				if lastReadbackRefresh.Equal(previousReadbackRefresh) &&
					time.Since(previousReadbackRefresh) >= snapshotReadbackRefreshInterval {
					if !readbackRefreshDeferred {
						readbackRefreshDeferred = true
						logger.Warn("served snapshot readback refresh deferred", "error_class", "readback_attestor")
					}
				} else if readbackRefreshDeferred {
					readbackRefreshDeferred = false
					logger.Info("served snapshot readback refresh restored")
				}
			} else {
				authorityApplication.SetAvailable(false)
				if readiness.Set(conditionSnapshot, false) {
					logger.Error("served snapshot readback unavailable", "error_class", "readback_attestor")
				}
			}
			continue
		}
		next, err := service.NewAuthority(loaded.Policy, loaded.Keys, store)
		activationCtx, activationCancel := context.WithTimeout(
			ctx,
			config.ReadinessTimeout,
		)
		if err == nil {
			err = authorityApplication.ActivateReplacement(activationCtx, next)
		}
		activationCancel()
		if err != nil {
			probeCtx, probeCancel := context.WithTimeout(ctx, config.ReadinessTimeout)
			retained := retainServedSnapshotAfterReplacementFailure(probeCtx, authorityApplication, err)
			probeCancel()
			authorityApplication.SetAvailable(retained)
			if readiness.Set(conditionSnapshot, retained) {
				if retained {
					logger.Warn("replacement readback deferred with bounded served snapshot", "error_class", "readback_attestor")
				} else {
					logger.Error("authority snapshot activation rejected", "error_class", "snapshot")
				}
			}
			continue
		}
		lastReadbackRefresh = time.Now()
		readbackRefreshDeferred = false
		readiness.Set(conditionSnapshot, true)
		logger.Info(
			"authority snapshot activated",
			"source_revision", loaded.Policy.SourceRevision,
			"key_set_revision", loaded.Policy.KeySetRevision,
			"policy_revision", loaded.Policy.PolicyRevision,
			"signer_generation", loaded.Policy.SignerGeneration,
		)
	}
}

// retainServedSnapshotAfterReplacementFailure allows an old served snapshot only when the replacement error belongs to the permitted last-known-good class and current ServedStateReady succeeds. It never activates a replacement, refreshes a receipt or even probes readiness for a noneligible error.
// retainServedSnapshotAfterReplacementFailure разрешает старый обслуживаемый снимок только при допустимом классе ошибки замены и успешном текущем ServedStateReady. Замена не активируется, квитанция не обновляется, при недопустимой ошибке даже готовность не запрашивается.
func retainServedSnapshotAfterReplacementFailure(ctx context.Context, runtime snapshotMaintenance, err error) bool {
	return allowsReadbackLastKnownGood(err) && runtime.ServedStateReady(ctx) == nil
}

// maintainServedSnapshot first checks current readiness when the local refresh interval has not elapsed, otherwise or after a failed check it attempts activation immediately. Successful activation returns now; only a permitted transient failure with a successful served-state check preserves lastRefresh, reusing an earlier check if already made. Other activation/readiness errors are returned, sometimes joined; no local timestamp is advanced on deferred or failed refresh.
// maintainServedSnapshot сначала проверяет текущую готовность, если локальный интервал обновления не истёк; иначе либо после неуспешной проверки сразу пытается активировать снимок. Успешная активация возвращает now; только допустимая временная ошибка с успешной проверкой обслуживаемого состояния сохраняет lastRefresh, переиспользуя уже сделанную проверку. Остальные ошибки активации и готовности возвращаются, иногда объединёнными; при отложенном или неудачном обновлении локальная временная метка не продвигается.
func maintainServedSnapshot(
	ctx context.Context,
	runtime snapshotMaintenance,
	lastRefresh time.Time,
	now time.Time,
) (time.Time, error) {
	servedStateChecked := false
	var servedStateErr error
	if now.Sub(lastRefresh) < snapshotReadbackRefreshInterval {
		servedStateChecked = true
		servedStateErr = runtime.ServedStateReady(ctx)
		if servedStateErr == nil {
			return lastRefresh, nil
		}
	}
	if err := runtime.ActivateSnapshot(ctx); err != nil {
		if !allowsReadbackLastKnownGood(err) {
			return lastRefresh, fmt.Errorf(
				"refresh served snapshot readback: %w",
				err,
			)
		}
		if !servedStateChecked {
			servedStateErr = runtime.ServedStateReady(ctx)
		}
		if servedStateErr == nil {
			return lastRefresh, nil
		}
		return lastRefresh, errors.Join(
			fmt.Errorf("refresh served snapshot readback: %w", err),
			fmt.Errorf("validate current served snapshot readback: %w", servedStateErr),
		)
	}
	return now, nil
}

// allowsReadbackLastKnownGood rejects nil, then accepts a matching context deadline, gRPC Unavailable/DeadlineExceeded/ResourceExhausted, or a net.Error reporting Timeout or Temporary. This is only error classification; retaining a snapshot separately requires a successful authoritative served-state readiness check.
// allowsReadbackLastKnownGood отклоняет nil, затем принимает совпавший срок контекста, gRPC Unavailable, DeadlineExceeded или ResourceExhausted либо net.Error с Timeout или Temporary. Это только классификация ошибки; сохранение снимка отдельно требует успешной авторитетной проверки обслуживаемой готовности.
func allowsReadbackLastKnownGood(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted:
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError) &&
		(networkError.Timeout() || networkError.Temporary())
}

// openPostgres maps the validated issuer/verifier configuration to the shared capability-pool constructor, preserving exact DSN/TLS/user/capability, service name and connection limit without opening an independent connection path.
// openPostgres передаёт проверенную конфигурацию issuer или verifier общему конструктору пула с ролью возможности, сохраняя точные DSN, TLS, пользователя, возможность, имя сервиса и предел соединений без отдельного пути подключения.
func openPostgres(ctx context.Context, config Config) (*pgxpool.Pool, error) {
	return openCapabilityPostgres(ctx, postgresConnectionConfig{
		DSNFile: config.PostgresDSNFile, TLSServerName: config.PostgresTLSServerName,
		ExpectedUser: config.PostgresExpectedSessionUser, Capability: config.DatabaseCapabilityRole,
		ApplicationName: config.ServiceName, MaxConnections: config.PostgresMaxConnections,
	})
}

// newTechnicalServer builds a fresh local metrics/live/health/ready mux with fixed two/five/five/thirty-second HTTP timeouts. Readiness reads the supplied local flag, health returns 204 and live/ready use explicit text content type; config and authorityApplication are currently unused parameters, and creating the server neither binds a listener nor probes the authority RPC.
// newTechnicalServer создаёт новый локальный mux metrics, live, health и ready с фиксированными HTTP-сроками две, пять, пять и тридцать секунд. Готовность читает переданный локальный признак, health возвращает 204, live и ready явно задают текстовый content type; config и authorityApplication пока неиспользуемые параметры, создание сервера не открывает listener и не проверяет RPC authority.
func newTechnicalServer(
	config Config,
	readiness *serviceruntime.Readiness,
	metrics *observability.Metrics,
	authorityApplication *application.Authority,
) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.PrometheusHandler())
	mux.HandleFunc("/livez", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = response.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/healthz", func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/readyz", func(response http.ResponseWriter, _ *http.Request) {
		if ready, _ := readiness.Ready(); !ready {
			http.Error(response, "not ready", http.StatusServiceUnavailable)
			return
		}
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = response.Write([]byte("ready\n"))
	})
	return &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
}

// stopGRPC starts GracefulStop and waits for its completion or the supplied deadline. On cancellation it forces Stop and returns ctx.Err; the error path does not wait again on the graceful-stop channel or create an unbounded shutdown context.
// stopGRPC запускает GracefulStop и ждёт его завершения либо переданного срока. При отмене принудительно вызывает Stop и возвращает ctx.Err; ветвь ошибки повторно не ждёт канал graceful-stop и не создаёт неограниченный контекст завершения.
func stopGRPC(ctx context.Context, server *grpc.Server) error {
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
		return ctx.Err()
	}
}

// readPrivateFile reads a bounded credential through securefile and replaces every rejection with one fixed type/mode/size error, hiding the path and file contents. On success it returns the bytes to the caller without parsing or automatically clearing them.
// readPrivateFile читает ограниченный реквизит через securefile и заменяет любой отказ одной фиксированной ошибкой типа, режима или размера, скрывая путь и содержимое. При успехе байты возвращаются вызывающему без разбора или автоматической очистки.
func readPrivateFile(path string, limit int64) ([]byte, error) {
	value, err := securefile.Read(path, limit)
	if err != nil {
		return nil, errors.New("secret file type, mode or size is invalid")
	}
	return value, nil
}

// allowedMethods returns a new closed full-RPC-to-diagnostic-name map for issuer, or the verifier map for any other mode. Configuration validates supported modes earlier; this map bounds telemetry labels and does not authorize an RPC.
// allowedMethods возвращает новую закрытую карту полного RPC в диагностическое имя для issuer либо карту verifier для любого другого режима. Поддерживаемые режимы ранее проверяет конфигурация; карта ограничивает метки телеметрии и не авторизует RPC.
func allowedMethods(mode Mode) map[string]string {
	if mode == ModeIssuer {
		return map[string]string{
			internalrpcauthorityv1.AuthorizationIssuerService_IssueContinuationAuthorizationContext_FullMethodName: "issue_continuation_authorization_context",
			internalrpcauthorityv1.AuthorizationIssuerService_IssueAuthorizationContext_FullMethodName:             "issue_authorization_context",
			internalrpcauthorityv1.AuthorizationIssuerService_CheckReadiness_FullMethodName:                        "check_readiness",
		}
	}
	return map[string]string{
		internalrpcauthorityv1.AuthorizationVerifierService_VerifyAuthorizationContext_FullMethodName: "verify_authorization_context",
		internalrpcauthorityv1.AuthorizationVerifierService_CheckReadiness_FullMethodName:             "check_readiness",
	}
}

// normalizedMethod resolves a full RPC through the role's closed diagnostic map and returns the fixed unknown fallback for every unregistered method, preventing caller-controlled full names from becoming log-operation labels.
// normalizedMethod разрешает полный RPC через закрытую диагностическую карту роли и возвращает фиксированное unknown для любого незарегистрированного метода, не допуская управляемых вызывающим полных имён в метки операции журнала.
func normalizedMethod(mode Mode, fullMethod string) string {
	if operation, ok := allowedMethods(mode)[fullMethod]; ok {
		return operation
	}
	return "unknown"
}
