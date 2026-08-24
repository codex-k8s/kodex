package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
	kubernetespitr "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/repository/kubernetes/pitr"
	kubernetesrestore "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/repository/kubernetes/restore"
)

// RestorePITRConfig задаёт independent executable CloudNativePG owner.
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

// LoadRestorePITRConfig читает closed immutable PITR inputs.
func LoadRestorePITRConfig() (RestorePITRConfig, error) {
	config := RestorePITRConfig{
		KubernetesAddress:       "https://kubernetes.default.svc:443",
		KubernetesTLSServerName: "kubernetes.default.svc",
		KubernetesCAFile:        "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt",
		KubernetesTokenFile:     "/var/run/secrets/tokens/kubernetes/token",
		EvidencePrivateJWKFile:  "/var/run/secrets/mattercodex/internal-rpc-authority/restore-pitr/evidence-private.jwk",
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

// RunRestorePITR выполняет фактический PITR и публикует independent evidence.
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
		Namespace:     "mattercodex-system",
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
		Namespace:          "mattercodex-system",
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
