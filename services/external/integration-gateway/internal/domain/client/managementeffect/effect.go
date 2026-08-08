package managementeffect

import (
	"context"
	"errors"

	domainrepo "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/repository/gateway"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/entity"
)

var ErrOutcomeUnknown = errors.New("management effect outcome is unknown")
var ErrCleanupIncomplete = errors.New("management cleanup phase is incomplete")

type (
	Readback struct {
		ResourceID               string
		Version                  uint64
		Digest                   string
		CredentialBindingID      string
		CredentialBindingVersion uint64
		CredentialBindingDigest  string
	}
	Client interface {
		SyncProvider(context.Context, domainrepo.Scope, entity.ManagedProviderConnection, entity.CredentialGeneration, string) (Readback, error)
		SyncPool(context.Context, domainrepo.Scope, entity.ManagedProviderPool, string) (Readback, error)
		GitIntentSHA256(domainrepo.Scope, entity.GitSourceBinding, entity.GitReconciliation, []byte, string) (string, error)
		ReconcileGit(context.Context, domainrepo.Scope, entity.GitSourceBinding, entity.GitReconciliation, []byte, string) (Readback, error)
		Check(context.Context) error
	}
)
