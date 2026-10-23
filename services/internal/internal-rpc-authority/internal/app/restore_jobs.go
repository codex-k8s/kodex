package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"os"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/grpcserver"
	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	kubernetespitr "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/repository/kubernetes/pitr"
	kubernetesrestore "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/repository/kubernetes/restore"
	postgresrestore "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/repository/postgres/restore"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// RestoreOperatorConfig carries one prepare/complete command's controller mTLS/application-token locators, exact database/backup/target bindings, Kubernetes manifest-resolution inputs, idempotency/correlation keys and timeout. These fields describe an intended command; authorization and durable state transitions remain owned by the controller.
// RestoreOperatorConfig хранит адреса mTLS и токена приложения контроллера, точные привязки БД, резервной копии и цели, входы разрешения manifest Kubernetes, ключи идемпотентности и сопоставления и срок одной команды prepare или complete. Поля описывают намерение команды; авторизация и устойчивые переходы принадлежат контроллеру.
type RestoreOperatorConfig struct {
	Action                  string        `env:"INTERNAL_RPC_AUTHORITY_RESTORE_ACTION"`
	ControllerAddress       string        `env:"INTERNAL_RPC_AUTHORITY_RESTORE_CONTROLLER_ADDRESS"`
	ControllerTLSServerName string        `env:"INTERNAL_RPC_AUTHORITY_RESTORE_CONTROLLER_TLS_SERVER_NAME"`
	ControllerCAFile        string        `env:"INTERNAL_RPC_AUTHORITY_RESTORE_CONTROLLER_CA_FILE"`
	ClientCertificateFile   string        `env:"INTERNAL_RPC_AUTHORITY_TLS_CERTIFICATE_FILE"`
	ClientPrivateKeyFile    string        `env:"INTERNAL_RPC_AUTHORITY_TLS_PRIVATE_KEY_FILE"`
	ApplicationTokenFile    string        `env:"INTERNAL_RPC_AUTHORITY_RESTORE_OPERATOR_TOKEN_FILE"`
	KubernetesAddress       string        `env:"INTERNAL_RPC_AUTHORITY_KUBERNETES_ADDRESS"`
	KubernetesTLSServerName string        `env:"INTERNAL_RPC_AUTHORITY_KUBERNETES_TLS_SERVER_NAME"`
	KubernetesCAFile        string        `env:"INTERNAL_RPC_AUTHORITY_KUBERNETES_CA_FILE"`
	KubernetesTokenFile     string        `env:"INTERNAL_RPC_AUTHORITY_KUBERNETES_TOKEN_FILE"`
	RestoreID               string        `env:"INTERNAL_RPC_AUTHORITY_RESTORE_ID"`
	DatabaseClusterID       string        `env:"INTERNAL_RPC_AUTHORITY_DATABASE_CLUSTER_ID"`
	BackupResourceName      string        `env:"INTERNAL_RPC_AUTHORITY_CNPG_BACKUP_NAME"`
	BarmanObjectName        string        `env:"INTERNAL_RPC_AUTHORITY_CNPG_BARMAN_OBJECT_NAME"`
	RecoveryTarget          time.Time     `env:"INTERNAL_RPC_AUTHORITY_RECOVERY_TARGET"`
	IdempotencyKey          string        `env:"INTERNAL_RPC_AUTHORITY_IDEMPOTENCY_KEY"`
	CorrelationID           string        `env:"INTERNAL_RPC_AUTHORITY_CORRELATION_ID"`
	Timeout                 time.Duration `env:"INTERNAL_RPC_AUTHORITY_RESTORE_TIMEOUT"`
}

// LoadRestoreOperatorConfig parses environment over fixed defaults, normalizes recovery time to a UTC second and validates the closed action, controller host/SNI, database and Kubernetes endpoints plus required backup/token/idempotency references. It does not read those credentials, authenticate an operator, resolve a backup manifest or validate every timeout during parsing.
// LoadRestoreOperatorConfig разбирает окружение поверх фиксированных исходных параметров, нормализует время восстановления до секунды UTC и проверяет закрытое действие, host и SNI контроллера, адреса БД и Kubernetes и обязательные ссылки резервной копии, токена и идемпотентности. При разборе реквизиты не читаются, оператор не аутентифицируется, manifest резервной копии не разрешается и все сроки не проверяются.
func LoadRestoreOperatorConfig() (RestoreOperatorConfig, error) {
	config := RestoreOperatorConfig{
		ControllerAddress:       "internal-rpc-authority-restore-controller.kodex-system.svc:8443",
		ControllerTLSServerName: "internal-rpc-authority-restore-controller.kodex-system.svc",
		ControllerCAFile:        "/var/run/config/kodex/internal-rpc-authority/restore-operator/controller-ca.pem",
		ClientCertificateFile:   "/var/run/secrets/kodex/internal-rpc-authority/restore-operator/tls/tls.crt",
		ClientPrivateKeyFile:    "/var/run/secrets/kodex/internal-rpc-authority/restore-operator/tls/tls.key",
		ApplicationTokenFile:    "/var/run/secrets/tokens/restore-operator/token",
		KubernetesAddress:       "https://kubernetes.default.svc:443",
		KubernetesTLSServerName: "kubernetes.default.svc",
		KubernetesCAFile:        "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt",
		KubernetesTokenFile:     "/var/run/secrets/tokens/kubernetes-api/token",
		DatabaseClusterID:       "internal-rpc-authority-primary",
		Timeout:                 20 * time.Second,
	}
	if err := parseEnvironment(&config); err != nil {
		return RestoreOperatorConfig{}, err
	}
	config.RecoveryTarget = config.RecoveryTarget.UTC().Truncate(time.Second)
	host, _, splitErr := net.SplitHostPort(config.ControllerAddress)
	if splitErr != nil ||
		host != config.ControllerTLSServerName ||
		config.ControllerTLSServerName !=
			"internal-rpc-authority-restore-controller.kodex-system.svc" ||
		(config.Action != "prepare" && config.Action != "complete") ||
		config.RestoreID == "" ||
		config.RecoveryTarget.IsZero() ||
		config.DatabaseClusterID != "internal-rpc-authority-primary" ||
		config.KubernetesAddress != "https://kubernetes.default.svc:443" ||
		config.KubernetesTLSServerName != "kubernetes.default.svc" ||
		config.KubernetesCAFile == "" ||
		config.KubernetesTokenFile == "" ||
		config.ApplicationTokenFile == "" ||
		config.BackupResourceName == "" ||
		config.BarmanObjectName == "" ||
		config.IdempotencyKey == "" ||
		config.CorrelationID == "" {
		return RestoreOperatorConfig{}, errors.New(
			"restore operator command binding is invalid",
		)
	}
	return config, nil
}

// RunRestoreOperator resolves the exact backup manifest under a bounded command context, loads controller mTLS and a projected application bearer, then calls the selected specialized prepare or complete RPC with the same restore/backup/time/idempotency bindings. It requires QUIESCING for prepare or COMPLETED for complete and closes clients plus telemetry on return; it adds no application-level blind retry or local owner approval. This performs real external command work if invoked and is not a unit probe.
// RunRestoreOperator разрешает точный manifest резервной копии в ограниченном контексте команды, загружает mTLS контроллера и проецируемый bearer приложения, затем вызывает выбранный специализированный RPC prepare или complete с теми же привязками восстановления, резервной копии, времени и идемпотентности. Для prepare требуется QUIESCING, для complete — COMPLETED; при возврате закрываются клиенты и телеметрия, слепого прикладного повтора или локального одобрения владельца нет. При вызове выполняется настоящая внешняя команда, это не юнит-проба.
func RunRestoreOperator(
	ctx context.Context,
	shutdownBase context.Context,
	buildVersion string,
) error {
	config, err := LoadRestoreOperatorConfig()
	if err != nil {
		return err
	}
	telemetry, _, err := startTelemetry(
		ctx,
		"internal_rpc_authority_restore_operator",
		buildVersion,
	)
	if err != nil {
		return err
	}
	defer telemetry.cleanupAfterStartupFailure(shutdownBase, config.Timeout)
	manifestResolver, err := kubernetespitr.NewManifestResolver(
		kubernetespitr.ManifestResolverConfig{
			Address:          config.KubernetesAddress,
			TLSServerName:    config.KubernetesTLSServerName,
			CAFile:           config.KubernetesCAFile,
			TokenFile:        config.KubernetesTokenFile,
			Namespace:        "kodex-system",
			BackupName:       config.BackupResourceName,
			SourceCluster:    config.DatabaseClusterID,
			BarmanObjectName: config.BarmanObjectName,
			BarmanServerName: config.DatabaseClusterID,
			Timeout:          min(config.Timeout, 10*time.Second),
		},
	)
	if err != nil {
		return err
	}
	defer manifestResolver.Close()
	tlsConfig, err := loadRestoreClientTLS(
		config.ControllerCAFile,
		config.ClientCertificateFile,
		config.ClientPrivateKeyFile,
		config.ControllerTLSServerName,
	)
	if err != nil {
		return err
	}
	connection, err := grpc.NewClient(
		config.ControllerAddress,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
		grpc.WithUnaryInterceptor(telemetry.UnaryClientInterceptor(map[string]string{
			internalrpcauthorityv1.RestoreControllerService_PrepareRestore_FullMethodName:  "prepare_restore",
			internalrpcauthorityv1.RestoreControllerService_CompleteRestore_FullMethodName: "complete_restore",
		})),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(grpcserver.StrictProtoCodec())),
	)
	if err != nil {
		return errors.New("create restore controller client")
	}
	defer connection.Close()
	client := internalrpcauthorityv1.NewRestoreControllerServiceClient(connection)
	callContext, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()
	manifest, err := manifestResolver.Resolve(callContext, config.RecoveryTarget)
	if err != nil {
		return err
	}
	tokenRaw, err := securefile.ReadProjectedServiceAccountToken(
		config.ApplicationTokenFile,
		16<<10,
	)
	if err != nil || strings.TrimSpace(string(tokenRaw)) == "" {
		return errors.New("read restore operator application credential")
	}
	callContext = metadata.AppendToOutgoingContext(
		callContext,
		"authorization",
		"Bearer "+strings.TrimSpace(string(tokenRaw)),
	)
	if config.Action == "prepare" {
		response, callErr := client.PrepareRestore(
			callContext,
			&internalrpcauthorityv1.PrepareRestoreRequest{
				RestoreId:                  config.RestoreID,
				DatabaseClusterId:          config.DatabaseClusterID,
				BackupManifestDigestSha256: manifest.DigestSHA256,
				RecoveryTargetTime:         timestamppb.New(config.RecoveryTarget),
				IdempotencyKey:             config.IdempotencyKey,
				CorrelationId:              config.CorrelationID,
			},
		)
		if callErr != nil {
			return callErr
		}
		if response.GetTransition().GetPhase() !=
			internalrpcauthorityv1.RestorePhase_RESTORE_PHASE_QUIESCING {
			return errors.New("restore prepare transition is not QUIESCING")
		}
		return nil
	}
	response, err := client.CompleteRestore(
		callContext,
		&internalrpcauthorityv1.CompleteRestoreRequest{
			RestoreId:                  config.RestoreID,
			DatabaseClusterId:          config.DatabaseClusterID,
			BackupManifestDigestSha256: manifest.DigestSHA256,
			RecoveryTargetTime:         timestamppb.New(config.RecoveryTarget),
			IdempotencyKey:             config.IdempotencyKey,
			CorrelationId:              config.CorrelationID,
		},
	)
	if err != nil {
		return err
	}
	if response.GetTransition().GetPhase() !=
		internalrpcauthorityv1.RestorePhase_RESTORE_PHASE_COMPLETED {
		return errors.New("restore completion transition is not COMPLETED")
	}
	return nil
}

// RunRestoreRecovery loads the controller's configuration, opens restore-capability PostgreSQL and Kubernetes coordination under a fifteen-second startup context, applies the durable fence to the loaded coordination state and checks that exact fence's readiness. It closes the pool/client and independently cleans telemetry on return; it does not execute PITR, prepare an owner decision or complete a restore.
// RunRestoreRecovery загружает конфигурацию контроллера, открывает PostgreSQL с возможностью restore и координацию Kubernetes в стартовом контексте пятнадцать секунд, применяет устойчивое ограждение к загруженному состоянию координации и проверяет готовность этого ограждения. Пул и клиент закрываются, телеметрия независимо очищается при возврате; PITR, подготовка решения владельца и завершение восстановления не выполняются.
func RunRestoreRecovery(
	ctx context.Context,
	shutdownBase context.Context,
	buildVersion string,
) error {
	config, err := LoadRestoreControllerConfig()
	if err != nil {
		return err
	}
	telemetry, _, err := startTelemetry(
		ctx,
		"internal_rpc_authority_restore_recovery",
		buildVersion,
	)
	if err != nil {
		return err
	}
	defer telemetry.cleanupAfterStartupFailure(shutdownBase, config.ShutdownTimeout)
	startup, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	pool, err := openRestorePostgres(startup, config)
	if err != nil {
		return err
	}
	defer pool.Close()
	fence, err := postgresrestore.New(pool)
	if err != nil {
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
		return err
	}
	defer coordination.Close()
	state, err := coordination.Load(startup)
	if err != nil {
		return err
	}
	if err := fence.ApplyRestoreFence(startup, state); err != nil {
		return err
	}
	return fence.RestoreFenceReady(startup, state)
}

// loadRestoreClientTLS loads a parseable nonempty PEM CA pool and one client certificate/key pair, then returns TLS 1.3 minimum with the exact supplied ServerName and no insecure verification override. Building this config neither establishes a connection nor substitutes for the application bearer and per-method authority checks.
// loadRestoreClientTLS загружает разбираемый непустой PEM-пул CA и пару клиентского сертификата и ключа, затем возвращает TLS минимум 1.3 с точным переданным ServerName без обхода проверки. Создание конфигурации не устанавливает соединение и не заменяет bearer приложения и проверку полномочий метода.
func loadRestoreClientTLS(
	caFile string,
	certificateFile string,
	privateKeyFile string,
	serverName string,
) (*tls.Config, error) {
	caRaw, err := os.ReadFile(caFile)
	if err != nil {
		return nil, errors.New("read restore controller CA")
	}
	rootCAs := x509.NewCertPool()
	if !rootCAs.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("restore controller CA is invalid")
	}
	certificate, err := tls.LoadX509KeyPair(certificateFile, privateKeyFile)
	if err != nil {
		return nil, errors.New("load restore operator client certificate")
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		RootCAs:      rootCAs,
		ServerName:   serverName,
		Certificates: []tls.Certificate{certificate},
	}, nil
}
