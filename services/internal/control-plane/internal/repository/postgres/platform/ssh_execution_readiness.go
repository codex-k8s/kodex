package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// querySSHExecutionContract checks installed owner objects and privileges without reading credentials or granting an effect.
// querySSHExecutionContract проверяет установленные owner objects и privileges без чтения credentials либо выдачи effect.
//
//go:embed sql/ssh_execution__contract.sql
var querySSHExecutionContract string

// sshExecutionDeliveryReady requires the real registered source profile, configured owner endpoint and installed purpose ledger before the common claim transaction can write any lease. It does not replace per-invocation eligibility.
// sshExecutionDeliveryReady требует real registered source profile, configured owner endpoint и installed purpose ledger до записи любой lease общей claim-транзакцией. Per-invocation eligibility не заменяется.
func (repository *Repository) sshExecutionDeliveryReady(ctx context.Context) (bool, error) {
	if repository.sshIdentityUploadEndpoint == nil || !repository.sshIdentityUploadEndpoint.Valid() {
		return false, nil
	}
	adapter, known := integrationpackage.Adapter(string(integrationpackage.AdapterSSHTarget))
	if !known || adapter.Readiness != integrationpackage.ReadinessReady {
		return false, nil
	}
	for _, key := range []string{integrationpackage.SSHManagedDefinitionKey, integrationpackage.SSHExecuteDefinitionKey} {
		definition, exists := repository.integrationDefinitions[key]
		if !exists {
			return false, nil
		}
		if _, err := sshHealthMode(definition); err != nil {
			return false, nil
		}
	}
	var ready bool
	if repository.pool == nil || repository.pool.QueryRow(ctx, querySSHExecutionContract).Scan(&ready) != nil {
		return false, errs.ErrUnavailable
	}
	return ready, nil
}
