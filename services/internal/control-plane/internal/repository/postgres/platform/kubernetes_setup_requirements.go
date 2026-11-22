package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesSetupRequirements reads only safe target/context metadata for a bounded organization batch.
// No Secret locator, credential, aliases or private registry document enters this projection.
// queryKubernetesSetupRequirements читает только safe target/context metadata bounded organization batch.
// Secret locator, credential, aliases и private registry document не входят в projection.
//
//go:embed sql/kubernetes_setup__requirements.sql
var queryKubernetesSetupRequirements string

// readKubernetesSetupRequirements reads one bounded metadata batch inside the caller's consistent
// scoped snapshot. The caller releases entries only after canonical visibility, personal/manager
// eligibility and exact package kind checks; this internal read grants no authority and never
// replaces fresh Begin authorization or version checks.
// readKubernetesSetupRequirements читает bounded metadata batch внутри consistent scoped snapshot.
// Caller выдаёт entries лишь после canonical visibility, personal/manager eligibility и exact
// package kind checks; internal read не даёт authority и не заменяет fresh Begin/version checks.
func readKubernetesSetupRequirements(ctx context.Context, querier connectionQuerier, current scope, refs []string) (map[string]entity.KubernetesIdentitySetupRequirements, error) {
	result := make(map[string]entity.KubernetesIdentitySetupRequirements, len(refs))
	if len(refs) == 0 {
		return result, nil
	}
	if len(refs) > 101 {
		return nil, errs.ErrInvalid
	}
	rows, err := querier.Query(ctx, queryKubernetesSetupRequirements, pgx.StrictNamedArgs{"organization_id": current.organizationID, "connection_refs": refs})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var ref string
		var requirements entity.KubernetesIdentitySetupRequirements
		if err := rows.Scan(&ref, &requirements.ConnectionVersion, &requirements.Policy, &requirements.TargetRef, &requirements.TargetRevision, &requirements.CanonicalContext); err != nil || !requirements.Valid() {
			return nil, errs.ErrUnavailable
		}
		if _, duplicate := result[ref]; duplicate {
			return nil, errs.ErrUnavailable
		}
		result[ref] = requirements
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return result, nil
}
