package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
	kubernetespitr "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/repository/kubernetes/pitr"
	kubernetesrestore "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/repository/kubernetes/restore"
)

// RestorePITRConfig describes the independent executor's Kubernetes and evidence-key locators, exact source/backup/Barman inputs, PostgreSQL image and storage shape plus request timeout. It contains desired inputs, not an approved coordination state, a verified image digest or proof that PITR has completed.
// RestorePITRConfig описывает адреса Kubernetes и ключа evidence независимого исполнителя, точные входы source, backup и Barman, образ PostgreSQL, хранилище и срок запроса. Это требуемые входы, а не одобренное состояние координации, проверенный дайджест образа или доказательство завершённого PITR.
type RestorePITRConfig struct {
	KubernetesAddress       string        `env:"INTERNAL_RPC_AUTHORITY_KUBERNETES_ADDRESS"`
	KubernetesTLSServerName string        `env:"INTERNAL_RPC_AUTHORITY_KUBERNETES_TLS_SERVER_NAME"`
	KubernetesCAFile        string        `env:"INTERNAL_RPC_AUTHORITY_KUBERNETES_CA_FILE"`
	KubernetesTokenFile     string        `env:"INTERNAL_RPC_AUTHORITY_KUBERNETES_TOKEN_FILE"`
	EvidencePrivateJWKFile  string        `env:"INTERNAL_RPC_AUTHORITY_RESTORE_EVIDENCE_PRIVATE_JWK_FILE"`
	BackupResourceName      string        `env:"INTERNAL_RPC_AUTHORITY_CNPG_BACKUP_NAME"`
	SourceClusterName       string        `env:"INTERNAL_RPC_AUTHORITY_CNPG_SOURCE_CLUSTER_NAME"`
	BarmanObjectName        string        `env:"INTERNAL_RPC_AUTHORITY_CNPG_BARMAN_OBJECT_NAME"`
	BarmanServerName        string        `env:"INTERNAL_RPC_AUTHORITY_CNPG_BARMAN_SERVER_NAME"`
	PostgresImage           string        `env:"INTERNAL_RPC_AUTHORITY_CNPG_POSTGRES_IMAGE"`
	StorageClass            string        `env:"INTERNAL_RPC_AUTHORITY_CNPG_STORAGE_CLASS"`
	StorageSize             string        `env:"INTERNAL_RPC_AUTHORITY_CNPG_STORAGE_SIZE"`
	Timeout                 time.Duration `env:"INTERNAL_RPC_AUTHORITY_RESTORE_TIMEOUT"`
}

// LoadRestorePITRConfig applies executor defaults and parses environment, requiring fixed source/Barman server names, nonempty backup/object/storage inputs and an image string containing the @sha256: marker. This lexical marker check is not digest verification or admission; client/executor constructors perform later checks, and parsing does not contact Kubernetes.
// LoadRestorePITRConfig задаёт исходные параметры исполнителя и разбирает окружение, требуя фиксированные имена source и Barman server, непустые backup, object и storage и наличие маркера @sha256: в строке образа. Лексическая проверка маркера не проверяет дайджест и не является допуском; последующие конструкторы клиентов и исполнителя проверяют дальше, разбор не обращается к Kubernetes.
func LoadRestorePITRConfig() (RestorePITRConfig, error) {
	config := RestorePITRConfig{
		KubernetesAddress:       "https://kubernetes.default.svc:443",
		KubernetesTLSServerName: "kubernetes.default.svc",
		KubernetesCAFile:        "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt",
		KubernetesTokenFile:     "/var/run/secrets/tokens/kubernetes/token",
		EvidencePrivateJWKFile:  "/var/run/secrets/kodex/internal-rpc-authority/restore-pitr/evidence-private.jwk",
		BarmanServerName:        "internal-rpc-authority-primary",
		SourceClusterName:       "internal-rpc-authority-primary",
		StorageSize:             "20Gi",
		Timeout:                 10 * time.Second,
	}
	if err := parseEnvironment(&config); err != nil {
		return RestorePITRConfig{}, err
	}
	if config.BackupResourceName == "" ||
		config.SourceClusterName != "internal-rpc-authority-primary" ||
		config.BarmanObjectName == "" ||
		config.BarmanServerName != "internal-rpc-authority-primary" ||
		!strings.Contains(config.PostgresImage, "@sha256:") ||
		config.StorageClass == "" ||
		config.StorageSize == "" {
		return RestorePITRConfig{}, errors.New(
			"PITR executor immutable configuration is invalid",
		)
	}
	return config, nil
}

// RunRestorePITR constructs the fixed-namespace coordination/evidence executor clients with three desired database instances, loads current coordination and executes only when shouldExecuteRestorePITR permits it. Recognized inactive phases return success without Execute; unknown phases fail closed. Client construction still precedes the phase decision, and a real invocation can perform CloudNativePG recovery and publish signed evidence; unit tests must use only the separate pure phase predicate.
// RunRestorePITR создаёт клиенты координации и исполнителя evidence фиксированного namespace с тремя требуемыми экземплярами БД, загружает текущую координацию и исполняет только при разрешении shouldExecuteRestorePITR. Распознанные неактивные фазы возвращают успех без Execute, неизвестные отклоняются. Создание клиентов всё равно предшествует выбору фазы, настоящий вызов может выполнить восстановление CloudNativePG и опубликовать подписанное evidence; юнит-тесты должны вызывать только отдельный чистый предикат фазы.
func RunRestorePITR(
	ctx context.Context,
	shutdownBase context.Context,
	buildVersion string,
) error {
	config, err := LoadRestorePITRConfig()
	if err != nil {
		return err
	}
	telemetry, _, err := startTelemetry(
		ctx,
		"internal_rpc_authority_restore_pitr",
		buildVersion,
	)
	if err != nil {
		return err
	}
	defer telemetry.cleanupAfterStartupFailure(shutdownBase, config.Timeout)
	coordination, err := kubernetesrestore.New(kubernetesrestore.Config{
		Address:       config.KubernetesAddress,
		TLSServerName: config.KubernetesTLSServerName,
		CAFile:        config.KubernetesCAFile,
		TokenFile:     config.KubernetesTokenFile,
		Namespace:     "kodex-system",
		ResourceName:  "internal-rpc-authority-restore-coordination",
		Timeout:       config.Timeout,
	})
	if err != nil {
		return err
	}
	defer coordination.Close()
	executor, err := kubernetespitr.NewExecutor(kubernetespitr.ExecutorConfig{
		Address:            config.KubernetesAddress,
		TLSServerName:      config.KubernetesTLSServerName,
		CAFile:             config.KubernetesCAFile,
		TokenFile:          config.KubernetesTokenFile,
		Namespace:          "kodex-system",
		EvidenceSecretName: "internal-rpc-authority-restore-evidence",
		PrivateJWKFile:     config.EvidencePrivateJWKFile,
		BackupName:         config.BackupResourceName,
		SourceClusterName:  config.SourceClusterName,
		BarmanObjectName:   config.BarmanObjectName,
		BarmanServerName:   config.BarmanServerName,
		PostgresImage:      config.PostgresImage,
		StorageClass:       config.StorageClass,
		StorageSize:        config.StorageSize,
		Instances:          3,
		Timeout:            config.Timeout,
	})
	if err != nil {
		return err
	}
	defer executor.Close()
	state, err := coordination.Load(ctx)
	if err != nil {
		return err
	}
	execute, err := shouldExecuteRestorePITR(state)
	if err != nil {
		return err
	}
	if !execute {
		return nil
	}
	return executor.Execute(ctx, state)
}

// shouldExecuteRestorePITR permits execution only for PREPARED, treats empty/OPEN/QUIESCING/COMPLETED as successful no-ops and rejects every other persisted phase. It classifies only the phase; restore identity, evidence eligibility and authority are not validated by this predicate.
// shouldExecuteRestorePITR разрешает исполнение только для PREPARED, считает пустую фазу, OPEN, QUIESCING и COMPLETED успешным отсутствием действия и отклоняет остальные сохранённые фазы. Классифицируется только фаза; идентичность восстановления, допустимость evidence и полномочия этим предикатом не проверяются.
func shouldExecuteRestorePITR(state model.RestoreState) (bool, error) {
	switch state.Phase {
	case "", "OPEN", "QUIESCING", "COMPLETED":
		return false, nil
	case "PREPARED":
		return true, nil
	default:
		return false, errors.New("PITR coordination phase is invalid")
	}
}
