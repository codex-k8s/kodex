package app

import (
	"errors"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/service/secretdraft"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/integration/draftowner"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/integration/draftruntime"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/integration/stagingcrypto"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/integration/stagingguard"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/integration/stagingstorage"
	store "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/observability"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// newSecretDrafts obtains in-cluster Kubernetes configuration with a five-second request timeout and
// delegates explicit draft composition; it neither starts recovery nor opens the keyring during
// construction.
// newSecretDrafts получает in-cluster Kubernetes configuration с пятисекундным request timeout и
// делегирует явную draft composition; при создании не запускает recovery и не открывает keyring.
func newSecretDrafts(config Config, ownerClient controlplanev1.RuntimeSecretDraftWorkServiceClient, runtimeStore *store.Store,
	metrics *observability.SecretDrafts) (*secretdraft.Service, error) {
	apiConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, errors.New("load secret draft Kubernetes configuration")
	}
	apiConfig.Timeout = 5 * time.Second
	client, err := kubernetes.NewForConfig(apiConfig)
	if err != nil {
		return nil, errors.New("create secret draft Kubernetes client")
	}
	return composeSecretDrafts(config, ownerClient, runtimeStore, metrics, client)
}

// composeSecretDrafts wires owner, durable guard, file keys, AEAD, isolated ciphertext store, shared
// runtime store and observer with a bounded value limit; it creates no alternate storage authority and
// does not run readiness/recovery itself.
// composeSecretDrafts связывает owner, durable guard, file keys, AEAD, isolated ciphertext store, shared
// runtime store и observer с bounded value limit; альтернативную storage authority не создаёт,
// readiness/recovery самостоятельно не выполняет.
func composeSecretDrafts(config Config, ownerClient controlplanev1.RuntimeSecretDraftWorkServiceClient, runtimeStore *store.Store,
	metrics *observability.SecretDrafts, client kubernetes.Interface) (*secretdraft.Service, error) {
	if client == nil || metrics == nil {
		return nil, errors.New("secret draft composition dependencies are invalid")
	}
	owner, err := draftowner.New(ownerClient, config.ClaimantID)
	if err != nil {
		return nil, err
	}
	guard, err := stagingguard.New(client.CoreV1().ConfigMaps(config.DraftNamespace), config.DraftNamespace, config.DraftKeyGuardName)
	if err != nil {
		return nil, err
	}
	keys, err := stagingcrypto.NewFileKeys(config.DraftKeyringFile, guard)
	if err != nil {
		return nil, err
	}
	cipher, err := stagingcrypto.New(keys)
	if err != nil {
		return nil, err
	}
	staged, err := stagingstorage.New(client.CoreV1().Secrets(config.DraftNamespace), config.DraftNamespace)
	if err != nil {
		return nil, err
	}
	runtime, err := draftruntime.New(runtimeStore)
	if err != nil {
		return nil, err
	}
	maximum := min(config.MaximumSecretBytes, value.MaximumDraftValueBytes)
	return secretdraft.New(owner, cipher, keys, staged, runtime, maximum, config.DraftNamespace, config.RuntimeNamespace,
		secretdraft.WithObserver(metrics))
}
