package app

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/grpcserver"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/application"
	publisherclient "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/client/publisher"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/service"
	model "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/publisher"
	kubernetespitr "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/repository/kubernetes/pitr"
	kubernetesrestore "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/repository/kubernetes/restore"
	postgresrestore "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/repository/postgres/restore"
	sessionrepository "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/repository/postgres/session"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/snapshot"
	authoritygrpc "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/transport/grpc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
)

// RestoreControllerConfig describes restore coordination listeners, exact database/controller identity, trust/target inputs, publisher and Kubernetes clients, evidence verification locators and readiness/shutdown budgets. Parsed references do not prove the existence of an approved restore, a trusted evidence object or a ready PostgreSQL connection.
// RestoreControllerConfig описывает listeners координации восстановления, точные идентичности БД и контроллера, входы доверия и целей, клиенты publisher и Kubernetes, адреса проверки evidence и бюджеты готовности и завершения. Разобранные ссылки не доказывают наличие одобренного восстановления, доверенного evidence или готового подключения PostgreSQL.
type RestoreControllerConfig struct {
	Listen                   string `env:"INTERNAL_RPC_AUTHORITY_RESTORE_CONTROLLER_LISTEN"`
	TechnicalListen          string `env:"INTERNAL_RPC_AUTHORITY_TECHNICAL_LISTEN"`
	TLSCertificateFile       string `env:"INTERNAL_RPC_AUTHORITY_TLS_CERTIFICATE_FILE"`
	TLSPrivateKeyFile        string `env:"INTERNAL_RPC_AUTHORITY_TLS_PRIVATE_KEY_FILE"`
	ClientCAFile             string `env:"INTERNAL_RPC_AUTHORITY_CLIENT_CA_FILE"`
	PostgresDSNFile          string `env:"INTERNAL_RPC_AUTHORITY_POSTGRES_DSN_FILE"`
	PostgresTLSServerName    string `env:"INTERNAL_RPC_AUTHORITY_POSTGRES_TLS_SERVER_NAME"`
	PostgresExpectedUser     string `env:"INTERNAL_RPC_AUTHORITY_POSTGRES_EXPECTED_SESSION_USER"`
	DatabaseClusterID        string `env:"INTERNAL_RPC_AUTHORITY_DATABASE_CLUSTER_ID"`
	ControllerGeneration     uint64 `env:"INTERNAL_RPC_AUTHORITY_RESTORE_CONTROLLER_GENERATION"`
	TargetRegistryFile       string `env:"INTERNAL_RPC_AUTHORITY_TARGET_REGISTRY_FILE"`
	ManifestRootJWKFile      string `env:"INTERNAL_RPC_AUTHORITY_MANIFEST_ROOT_JWK_FILE"`
	ManifestRootMetadataFile string `env:"INTERNAL_RPC_AUTHORITY_MANIFEST_ROOT_METADATA_FILE"`
	ManifestTrustBundleFile  string `env:"INTERNAL_RPC_AUTHORITY_MANIFEST_TRUST_BUNDLE_FILE"`
	RestoreRoleTrustFile     string `env:"INTERNAL_RPC_AUTHORITY_RESTORE_ROLE_TRUST_FILE"`
	PublisherAddress         string `env:"INTERNAL_RPC_AUTHORITY_PUBLISHER_ADDRESS"`
	PublisherTLSServerName   string `env:"INTERNAL_RPC_AUTHORITY_PUBLISHER_TLS_SERVER_NAME"`
	PublisherCAFile          string `env:"INTERNAL_RPC_AUTHORITY_PUBLISHER_CA_FILE"`
	KubernetesAddress        string `env:"INTERNAL_RPC_AUTHORITY_KUBERNETES_ADDRESS"`
	KubernetesTLSServerName  string `env:"INTERNAL_RPC_AUTHORITY_KUBERNETES_TLS_SERVER_NAME"`
	KubernetesCAFile         string `env:"INTERNAL_RPC_AUTHORITY_KUBERNETES_CA_FILE"`
	KubernetesTokenFile      string `env:"INTERNAL_RPC_AUTHORITY_KUBERNETES_TOKEN_FILE"`
	RestoreEvidencePublicJWK string `env:"INTERNAL_RPC_AUTHORITY_RESTORE_EVIDENCE_PUBLIC_JWK_FILE"`
	KubernetesNamespace      string
	KubernetesResourceName   string
	ReadinessTimeout         time.Duration `env:"INTERNAL_RPC_AUTHORITY_READINESS_TIMEOUT"`
	ReadinessInterval        time.Duration `env:"INTERNAL_RPC_AUTHORITY_READINESS_INTERVAL"`
	ShutdownTimeout          time.Duration `env:"INTERNAL_RPC_AUTHORITY_SHUTDOWN_TIMEOUT"`
}

// LoadRestoreControllerConfig sets the restore-controller defaults and fixed resource locators, parses environment fields and requires valid listener syntax, nonzero controller generation, expected database user and the exact database-cluster ID. It bounds readiness interval/timeout but does not load certificates, verify external trust or prove infrastructure readiness during parsing.
// LoadRestoreControllerConfig задаёт исходные параметры restore-controller и фиксированные адреса ресурсов, разбирает поля окружения и требует синтаксис listeners, ненулевое поколение контроллера, ожидаемого пользователя БД и точный идентификатор кластера БД. Интервал и срок готовности ограничиваются, но сертификаты, внешнее доверие и готовность инфраструктуры при разборе не проверяются.
func LoadRestoreControllerConfig() (RestoreControllerConfig, error) {
	config := RestoreControllerConfig{
		Listen:                   ":8443",
		TechnicalListen:          ":9090",
		TLSCertificateFile:       "/var/run/secrets/kodex/internal-rpc-authority/restore-controller/tls/tls.crt",
		TLSPrivateKeyFile:        "/var/run/secrets/kodex/internal-rpc-authority/restore-controller/tls/tls.key",
		ClientCAFile:             "/var/run/config/kodex/internal-rpc-authority/restore-controller/client-ca.pem",
		PostgresDSNFile:          "/var/run/secrets/kodex/internal-rpc-authority/restore-controller/database/dsn",
		PostgresTLSServerName:    "internal-rpc-authority-postgresql-rw.kodex-system.svc.cluster.local",
		DatabaseClusterID:        "internal-rpc-authority-primary",
		TargetRegistryFile:       "/var/run/config/kodex/internal-rpc-authority/restore-controller/key-delivery-targets.yaml",
		ManifestRootJWKFile:      "/usr/local/share/internal-rpc-authority/manifest-root/bootstrap-public.jwk",
		ManifestRootMetadataFile: "/usr/local/share/internal-rpc-authority/manifest-root/bootstrap-metadata.json",
		ManifestTrustBundleFile:  "/var/run/config/kodex/internal-rpc-authority/restore-controller/manifest-trust.jws",
		RestoreRoleTrustFile:     "/var/run/config/kodex/internal-rpc-authority/restore-controller/restore-role-trust.jws",
		PublisherAddress:         "internal-rpc-authority-publisher.kodex-system.svc:8444",
		PublisherTLSServerName:   "internal-rpc-authority-publisher.kodex-system.svc",
		PublisherCAFile:          "/var/run/config/kodex/internal-rpc-authority/restore-controller/publisher-ca.pem",
		KubernetesAddress:        "https://kubernetes.default.svc:443",
		KubernetesTLSServerName:  "kubernetes.default.svc",
		KubernetesCAFile:         "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt",
		KubernetesTokenFile:      "/var/run/secrets/tokens/kubernetes/token",
		RestoreEvidencePublicJWK: "/var/run/config/kodex/internal-rpc-authority/restore-trust/evidence-public.jwk",
		KubernetesNamespace:      "kodex-system",
		KubernetesResourceName:   "internal-rpc-authority-restore-coordination",
		ReadinessTimeout:         2 * time.Second,
		ReadinessInterval:        5 * time.Second,
		ShutdownTimeout:          10 * time.Second,
	}
	if err := parseEnvironment(&config); err != nil {
		return RestoreControllerConfig{}, err
	}
	if _, _, err := net.SplitHostPort(config.Listen); err != nil {
		return RestoreControllerConfig{}, errors.New(
			"restore controller listen address is invalid",
		)
	}
	if _, _, err := net.SplitHostPort(config.TechnicalListen); err != nil {
		return RestoreControllerConfig{}, errors.New(
			"restore controller technical listen address is invalid",
		)
	}
	if config.ControllerGeneration == 0 ||
		config.PostgresExpectedUser == "" ||
		config.DatabaseClusterID != "internal-rpc-authority-primary" ||
		config.ReadinessTimeout < 100*time.Millisecond ||
		config.ReadinessTimeout > 5*time.Second ||
		config.ReadinessInterval < time.Second ||
		config.ReadinessInterval > time.Minute {
		return RestoreControllerConfig{}, errors.New(
			"restore controller database identity is invalid",
		)
	}
	return config, nil
}

// RunRestoreController composes PostgreSQL fencing, Kubernetes coordination/evidence, the mTLS publisher client, verified restore-role trust and the exact TLS-derived ES256 controller key, then recovers startup state before serving restore RPC. Its monitor reports direct infrastructure readiness; shutdown retains worker/server/publisher/evidence/Kubernetes/database order before telemetry. The caller owns lifecycle and shutdown-base contexts; invoking this runtime can recover durable restore state and is not an ordinary local unit check.
// RunRestoreController собирает ограждение PostgreSQL, координацию и evidence Kubernetes, mTLS-клиент publisher, проверенное доверие роли восстановления и точный ES256-ключ контроллера из TLS, затем восстанавливает стартовое состояние до обслуживания restore RPC. Монитор сообщает прямую готовность инфраструктуры; завершение сохраняет порядок исполнителей, серверов, publisher, evidence, Kubernetes и БД перед телеметрией. Контексты жизненного цикла и базы завершения принадлежат вызывающему; runtime может восстанавливать устойчивое состояние restore и не является обычной локальной юнит-проверкой.
func RunRestoreController(
	lifecycle context.Context,
	shutdownBase context.Context,
	buildVersion string,
) error {
	config, err := LoadRestoreControllerConfig()
	if err != nil {
		return err
	}
	telemetry, logger, err := startTelemetry(
		lifecycle,
		"internal_rpc_authority_restore_controller",
		buildVersion,
	)
	if err != nil {
		return err
	}
	telemetryFinished := false
	defer func() {
		if !telemetryFinished {
			telemetry.cleanupAfterStartupFailure(shutdownBase, config.ShutdownTimeout)
		}
	}()
	startup, cancel := context.WithTimeout(lifecycle, 20*time.Second)
	defer cancel()
	pool, err := openRestorePostgres(startup, config)
	if err != nil {
		return err
	}
	fence, err := postgresrestore.New(pool)
	if err != nil {
		pool.Close()
		return err
	}
	coordination, err := kubernetesrestore.New(kubernetesrestore.Config{
		Address:       config.KubernetesAddress,
		TLSServerName: config.KubernetesTLSServerName,
		CAFile:        config.KubernetesCAFile,
		TokenFile:     config.KubernetesTokenFile,
		Namespace:     config.KubernetesNamespace,
		ResourceName:  config.KubernetesResourceName,
		Timeout:       5 * time.Second,
	})
	if err != nil {
		pool.Close()
		return err
	}
	evidence, err := kubernetespitr.NewVerifier(kubernetespitr.Config{
		Address:            config.KubernetesAddress,
		TLSServerName:      config.KubernetesTLSServerName,
		CAFile:             config.KubernetesCAFile,
		TokenFile:          config.KubernetesTokenFile,
		Namespace:          config.KubernetesNamespace,
		EvidenceSecretName: "internal-rpc-authority-restore-evidence",
		PublicJWKFile:      config.RestoreEvidencePublicJWK,
		Timeout:            5 * time.Second,
	})
	if err != nil {
		coordination.Close()
		pool.Close()
		return err
	}
	publisherConnection, err := publisherclient.New(publisherclient.Config{
		Address:               config.PublisherAddress,
		TLSServerName:         config.PublisherTLSServerName,
		CACertificateFile:     config.PublisherCAFile,
		ClientCertificateFile: config.TLSCertificateFile,
		ClientPrivateKeyFile:  config.TLSPrivateKeyFile,
		Timeout:               5 * time.Second,
		UnaryInterceptor: telemetry.UnaryClientInterceptor(map[string]string{
			internalrpcauthorityv1.RestoreRoleCredentialPublisherService_PublishRoleCredential_FullMethodName: "publish_role_credential",
			internalrpcauthorityv1.RestoreRoleCredentialPublisherService_CheckReadiness_FullMethodName:        "check_readiness",
		}),
	})
	if err != nil {
		evidence.Close()
		coordination.Close()
		pool.Close()
		return err
	}
	targetRegistry, err := publisher.LoadRegistry(config.TargetRegistryFile)
	if err != nil {
		_ = publisherConnection.Close()
		evidence.Close()
		coordination.Close()
		pool.Close()
		return err
	}
	roleKeys, roleTrust, err := snapshot.LoadRestoreRoleTrust(
		snapshot.RestoreRoleTrustOptions{
			ManifestRootPublicJWKFile:  config.ManifestRootJWKFile,
			ManifestRootMetadataFile:   config.ManifestRootMetadataFile,
			ManifestTrustBundleJWSFile: config.ManifestTrustBundleFile,
			RestoreRoleTrustJWSFile:    config.RestoreRoleTrustFile,
		},
	)
	if err != nil {
		_ = publisherConnection.Close()
		evidence.Close()
		coordination.Close()
		pool.Close()
		return err
	}
	controllerKey, err := loadControllerSigningKey(
		config.TLSCertificateFile,
		config.TLSPrivateKeyFile,
	)
	if err != nil {
		_ = publisherConnection.Close()
		evidence.Close()
		coordination.Close()
		pool.Close()
		return err
	}
	domain, err := service.NewRestoreController(
		config.DatabaseClusterID,
		controllerKey,
		config.ControllerGeneration,
		targetRegistry,
		roleKeys,
		roleTrust,
		coordination,
		fence,
		publisherConnection,
		evidence,
	)
	if err != nil {
		_ = publisherConnection.Close()
		evidence.Close()
		coordination.Close()
		pool.Close()
		return err
	}
	restoreApplication := application.NewRestoreController(domain)
	if err := restoreApplication.Recover(startup); err != nil {
		_ = publisherConnection.Close()
		evidence.Close()
		coordination.Close()
		pool.Close()
		return err
	}
	serverTLS, err := loadMTLSServerConfig(
		config.TLSCertificateFile,
		config.TLSPrivateKeyFile,
		config.ClientCAFile,
	)
	if err != nil {
		_ = publisherConnection.Close()
		evidence.Close()
		coordination.Close()
		pool.Close()
		return err
	}
	methods := map[string]string{
		internalrpcauthorityv1.RestoreControllerService_PrepareRestore_FullMethodName:        "prepare_restore",
		internalrpcauthorityv1.RestoreControllerService_GetRestoreDirective_FullMethodName:   "get_restore_directive",
		internalrpcauthorityv1.RestoreControllerService_AcknowledgeQuiescence_FullMethodName: "acknowledge_quiescence",
		internalrpcauthorityv1.RestoreControllerService_CompleteRestore_FullMethodName:       "complete_restore",
		internalrpcauthorityv1.RestoreControllerService_CheckReadiness_FullMethodName:        "check_readiness",
	}
	metrics := observability.NewMetrics(
		"internal_rpc_authority_restore_controller",
		buildVersion,
		methods,
	)
	readiness := serviceruntime.NewReadiness()
	readiness.Set(false, "starting")
	metrics.SetReady(false)
	grpcRuntime := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(serverTLS)),
		grpc.ForceServerCodec(grpcserver.StrictProtoCodec()),
		grpc.ChainUnaryInterceptor(
			metrics.UnaryServerInterceptor(),
			telemetry.UnaryServerInterceptor(methods),
			grpcserver.ErrorBoundary(grpcserver.ErrorObserverFunc(func(
				_ context.Context,
				method string,
				code codes.Code,
				_ error,
			) {
				logger.Error(
					"unexpected gRPC failure",
					"method", normalizeRestoreMethod(method),
					"code", code.String(),
				)
			})),
		),
	)
	internalrpcauthorityv1.RegisterRestoreControllerServiceServer(
		grpcRuntime,
		authoritygrpc.NewRestoreControllerServer(
			restoreApplication,
			coordination,
		),
	)
	grpcListener, err := net.Listen("tcp", config.Listen)
	if err != nil {
		_ = publisherConnection.Close()
		evidence.Close()
		coordination.Close()
		pool.Close()
		return errors.New("listen on restore controller endpoint")
	}
	technicalListener, err := net.Listen("tcp", config.TechnicalListen)
	if err != nil {
		_ = grpcListener.Close()
		_ = publisherConnection.Close()
		evidence.Close()
		coordination.Close()
		pool.Close()
		return errors.New("listen on restore controller technical endpoint")
	}
	technicalServer := newSnapshotTechnicalServer(readiness, metrics)
	workers := serviceruntime.StartWorkers(
		lifecycle,
		monitorRestoreReadiness(
			restoreApplication,
			readiness,
			metrics,
			logger,
			config.ReadinessInterval,
			config.ReadinessTimeout,
		),
	)
	serveErrors := make(chan error, 2)
	go func() {
		if serveErr := grpcRuntime.Serve(grpcListener); serveErr != nil {
			serveErrors <- errors.New("serve restore controller gRPC")
		}
	}()
	go func() {
		if serveErr := technicalServer.Serve(technicalListener); serveErr != nil &&
			!errors.Is(serveErr, http.ErrServerClosed) {
			serveErrors <- errors.New("serve restore controller technical HTTP")
		}
	}()
	logger.Info("restore controller started")
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
				Name:    "workers",
				Timeout: config.ShutdownTimeout,
				Run: func(ctx context.Context) error {
					workers.Stop()
					return workers.Wait(ctx)
				},
			},
			serviceruntime.ShutdownOperation{
				Name:    "grpc-server",
				Timeout: config.ShutdownTimeout,
				Run:     func(ctx context.Context) error { return stopGRPC(ctx, grpcRuntime) },
			},
			serviceruntime.ShutdownOperation{
				Name:    "technical-http",
				Timeout: config.ShutdownTimeout,
				Run:     technicalServer.Shutdown,
			},
			serviceruntime.ShutdownOperation{
				Name:    "publisher-client",
				Timeout: config.ShutdownTimeout,
				Run:     func(context.Context) error { return publisherConnection.Close() },
			},
			serviceruntime.ShutdownOperation{
				Name:    "restore-evidence-client",
				Timeout: config.ShutdownTimeout,
				Run: func(context.Context) error {
					evidence.Close()
					return nil
				},
			},
			serviceruntime.ShutdownOperation{
				Name:    "kubernetes-client",
				Timeout: config.ShutdownTimeout,
				Run: func(context.Context) error {
					coordination.Close()
					return nil
				},
			},
			serviceruntime.ShutdownOperation{
				Name:    "postgresql",
				Timeout: config.ShutdownTimeout,
				Run: func(context.Context) error {
					pool.Close()
					return nil
				},
			},
		)...,
	)
	telemetryFinished = true
	logger.Info("restore controller stopped")
	return errors.Join(runtimeErr, shutdownErr)
}

// restoreStartupReadiness is the application port for a direct startup-eligibility check returning the current restore state under the caller's bounded context. It exposes no generic prepare, approve or completion command to the readiness monitor.
// restoreStartupReadiness — порт приложения для прямой проверки стартовой допустимости с возвратом текущего состояния restore в ограниченном контексте вызывающего. Универсальные команды подготовки, подтверждения или завершения монитору готовности не предоставляются.
type restoreStartupReadiness interface {
	// StartupReady returns restore state or a failure of direct startup eligibility; the monitor consumes only the error and does not treat the returned state as permission to perform a restore.
	// StartupReady возвращает состояние восстановления либо отказ прямой стартовой допустимости; монитор использует только ошибку и не считает возвращённое состояние разрешением выполнять восстановление.
	StartupReady(context.Context) (model.RestoreState, error)
}

// monitorRestoreReadiness constructs a worker that probes immediately and on configured ticks with a separate bounded child context per probe, stopping with ctx.Err on cancellation. Construction itself starts no goroutine and performs no probe; serviceruntime owns the returned worker lifecycle.
// monitorRestoreReadiness создаёт исполнителя немедленной проверки и проверок по настроенным тикам с отдельным ограниченным дочерним контекстом каждой, завершающегося с ctx.Err при отмене. Само создание не запускает goroutine и не выполняет проверку; жизненный цикл возвращённого исполнителя принадлежит serviceruntime.
func monitorRestoreReadiness(application restoreStartupReadiness, readiness *serviceruntime.Readiness, metrics *observability.Metrics, logger *slog.Logger, interval, timeout time.Duration) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			probe, cancel := context.WithTimeout(ctx, timeout)
			refreshRestoreReadiness(probe, application, readiness, metrics, logger)
			cancel()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}
}

// refreshRestoreReadiness calls direct StartupReady once, updates the local flag and ready metric, and emits only transition logs with a fixed infrastructure error class. It discards the returned state and never approves, starts or completes a restore.
// refreshRestoreReadiness однократно вызывает прямой StartupReady, обновляет локальный признак и метрику ready и выводит только смены состояния с фиксированным классом ошибки инфраструктуры. Возвращённое состояние отбрасывается, подтверждение, запуск и завершение восстановления не выполняются.
func refreshRestoreReadiness(ctx context.Context, application restoreStartupReadiness, readiness *serviceruntime.Readiness, metrics *observability.Metrics, logger *slog.Logger) {
	_, err := application.StartupReady(ctx)
	if err != nil {
		if readiness.Set(false, "direct-infrastructure-unavailable") {
			logger.Error("restore controller readiness unavailable", "error_class", "postgresql_or_kubernetes")
		}
		metrics.SetReady(false)
		return
	}
	if readiness.Set(true, "ready") {
		logger.Info("restore controller readiness restored")
	}
	metrics.SetReady(true)
}

// openRestorePostgres reads the bounded DSN, rejects TLS fallback/foreign host or missing CA/SNI/insecure verification, installs the restore capability's exact session setup/acquire checks and a four-connection instrumented pool. Unlike the generic capability constructor, initial DSN user/password shape is not checked here; session identity is checked by its hooks. It retries connectivity within the caller's startup context and closes the pool if that budget fails.
// openRestorePostgres читает ограниченный DSN, отклоняет запасные TLS-пути, чужой host, отсутствие CA или SNI и небезопасную проверку, устанавливает точную настройку и проверку сессии возможности restore и инструментированный пул четырёх соединений. В отличие от общего конструктора возможности, исходная форма user и password DSN здесь не проверяется; идентичность сессии проверяют hooks. Подключение повторяется в стартовом контексте вызывающего, при неуспехе бюджета пул закрывается.
func openRestorePostgres(
	ctx context.Context,
	config RestoreControllerConfig,
) (*pgxpool.Pool, error) {
	raw, err := readPrivateFile(config.PostgresDSNFile, maxDSNFileBytes)
	if err != nil {
		return nil, errors.New("read restore controller PostgreSQL DSN file")
	}
	poolConfig, err := pgxpool.ParseConfig(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, errors.New("parse restore controller PostgreSQL DSN")
	}
	instrumentPGX(poolConfig, "internal_rpc_authority_restore_controller")
	if len(poolConfig.ConnConfig.Fallbacks) != 0 ||
		poolConfig.ConnConfig.Host != config.PostgresTLSServerName ||
		poolConfig.ConnConfig.TLSConfig == nil ||
		poolConfig.ConnConfig.TLSConfig.RootCAs == nil ||
		poolConfig.ConnConfig.TLSConfig.ServerName != config.PostgresTLSServerName ||
		poolConfig.ConnConfig.TLSConfig.InsecureSkipVerify {
		return nil, errors.New("restore controller PostgreSQL TLS boundary rejected")
	}
	poolConfig.MaxConns = 4
	poolConfig.ConnConfig.RuntimeParams["application_name"] =
		"internal_rpc_authority_restore_controller"
	poolConfig.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		return sessionrepository.Configure(
			ctx,
			connection,
			config.PostgresExpectedUser,
			sessionrepository.CapabilityRestoreController,
		)
	}
	poolConfig.BeforeAcquire = func(ctx context.Context, connection *pgx.Conn) bool {
		return sessionrepository.Ensure(
			ctx,
			connection,
			config.PostgresExpectedUser,
			sessionrepository.CapabilityRestoreController,
		) == nil
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, errors.New("open restore controller PostgreSQL pool")
	}
	if err := waitForPostgresConnectivity(ctx, pool.Ping, 250*time.Millisecond, 2*time.Second); err != nil {
		pool.Close()
		return nil, errors.New("verify restore controller PostgreSQL connectivity")
	}
	return pool, nil
}

// waitForPostgresConnectivity rejects a nil ping or nonpositive/inverted retry intervals, then retries every ping failure with doubling delay capped at the supplied maximum. The first success returns nil; cancellation while waiting stops the timer and returns ctx.Err rather than exposing the last connection error or creating a background retry.
// waitForPostgresConnectivity отклоняет nil ping и неположительные либо перевёрнутые интервалы, затем повторяет любую ошибку ping с удвоенной задержкой до переданного максимума. Первый успех возвращает nil; отмена ожидания останавливает таймер и возвращает ctx.Err без раскрытия последней ошибки подключения или фонового повтора.
func waitForPostgresConnectivity(
	ctx context.Context,
	ping func(context.Context) error,
	initialInterval time.Duration,
	maximumInterval time.Duration,
) error {
	if ping == nil || initialInterval <= 0 || maximumInterval < initialInterval {
		return errors.New("PostgreSQL connectivity retry policy is invalid")
	}
	interval := initialInterval
	for {
		if err := ping(ctx); err == nil {
			return nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		interval = min(interval*2, maximumInterval)
	}
}

// loadControllerSigningKey requires exactly one certificate in the loaded TLS pair, matching ECDSA private/public keys on P-256, and derives a controller key ID from the first twelve bytes of the certificate's SHA-256 digest. It returns the ES256 key for controller signing without verifying certificate expiry, a trust chain or workload authorization itself.
// loadControllerSigningKey требует ровно один сертификат загруженной TLS-пары, совпадающие приватный и публичный ECDSA-ключи P-256 и выводит идентификатор контроллера из первых двенадцати байтов SHA-256 сертификата. Возвращается ES256-ключ подписания контроллера без собственной проверки срока сертификата, цепочки доверия или полномочий workload.
func loadControllerSigningKey(
	certificateFile string,
	privateKeyFile string,
) (internalrpcauth.ES256Key, error) {
	pair, err := tls.LoadX509KeyPair(certificateFile, privateKeyFile)
	if err != nil || len(pair.Certificate) != 1 {
		return internalrpcauth.ES256Key{}, errors.New(
			"load restore controller signing certificate",
		)
	}
	certificate, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return internalrpcauth.ES256Key{}, errors.New(
			"parse restore controller signing certificate",
		)
	}
	privateKey, ok := pair.PrivateKey.(*ecdsa.PrivateKey)
	publicKey, publicOK := certificate.PublicKey.(*ecdsa.PublicKey)
	if !ok ||
		!publicOK ||
		privateKey.Curve != elliptic.P256() ||
		publicKey.Curve != elliptic.P256() ||
		!privateKey.PublicKey.Equal(publicKey) {
		return internalrpcauth.ES256Key{}, errors.New(
			"restore controller TLS key is not exact ES256",
		)
	}
	digest := sha256.Sum256(certificate.Raw)
	return internalrpcauth.ES256Key{
		KeyID:   "controller-tls-" + hex.EncodeToString(digest[:12]),
		Public:  publicKey,
		Private: privateKey,
	}, nil
}

// normalizeRestoreMethod maps the five registered restore-controller RPC to fixed diagnostic labels and normalizes every other full method to unknown, without granting restore or owner-decision authority.
// normalizeRestoreMethod сопоставляет пять зарегистрированных RPC restore-controller фиксированным диагностическим меткам и сводит любой другой полный метод к unknown без выдачи полномочий восстановления или решения владельца.
func normalizeRestoreMethod(method string) string {
	switch method {
	case internalrpcauthorityv1.RestoreControllerService_PrepareRestore_FullMethodName:
		return "prepare_restore"
	case internalrpcauthorityv1.RestoreControllerService_GetRestoreDirective_FullMethodName:
		return "get_restore_directive"
	case internalrpcauthorityv1.RestoreControllerService_AcknowledgeQuiescence_FullMethodName:
		return "acknowledge_quiescence"
	case internalrpcauthorityv1.RestoreControllerService_CompleteRestore_FullMethodName:
		return "complete_restore"
	case internalrpcauthorityv1.RestoreControllerService_CheckReadiness_FullMethodName:
		return "check_readiness"
	default:
		return "unknown"
	}
}
