package errs

import (
	"errors"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
)

// ErrKubernetesConfigurationPlanUnsupported keeps scalar assistant grant plans closed while the
// direct manager API owns exact namespace/ref mutations. It cannot select a sibling namespace.
// ErrKubernetesConfigurationPlanUnsupported закрывает scalar assistant grant plans; direct manager
// API владеет exact namespace/ref mutations. Не допускает выбора sibling namespace.
var ErrKubernetesConfigurationPlanUnsupported = errors.Join(ErrConflict, errors.New(managedkubernetes.ConfigurationPlanUnsupportedMessage))

// ErrKubernetesStagingQuota refuses new bytes after the explicit physical storage quota is filled.
// It does not claim a native effect was dispatched or erase retained idempotency receipts.
// ErrKubernetesStagingQuota отклоняет new bytes при исчерпании явной physical storage quota.
// Не утверждает dispatch native effect и не удаляет retained idempotency receipts.
var ErrKubernetesStagingQuota = errors.New("Kubernetes staging storage quota is exhausted")
