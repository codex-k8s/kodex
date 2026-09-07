package authorityproof

import (
	"errors"
	"time"

	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/google/uuid"
)

const runtimeExecutionProofError = "runtime execution authority is unavailable"

func runtimeMaterializationProofRequired(operation string) bool {
	return operation == "platform.runtime.credentials.materialize" ||
		operation == "platform.runtime.credentials.system-assistant.materialize"
}

// runtimeExecutionActor принимает только provenance, разрешённый owner repository.
// Общий worker credential подтверждает workload, но не назначает root actor.
func runtimeExecutionActor(resolved platformrepo.ProofAuthority, workload, operation string) (identity, string, error) {
	execution := resolved.RuntimeExecution
	if workload != "runtime-controller" || !runtimeMaterializationProofRequired(operation) || execution == nil ||
		uuid.Validate(resolved.ActorID) != nil || uuid.Validate(resolved.OrganizationID) != nil ||
		uuid.Validate(execution.RevisionID) != nil || execution.Generation == 0 || !validDigest(execution.RevisionDigest) ||
		(execution.ActorKind != "HUMAN" && execution.ActorKind != "SERVICE") {
		return identity{}, "", errors.New(runtimeExecutionProofError)
	}
	assistant := operation == "platform.runtime.credentials.system-assistant.materialize"
	if (assistant && resolved.ProjectID != "") || (!assistant && (uuid.Validate(resolved.ProjectID) != nil || resolved.ProjectVersion == 0)) {
		return identity{}, "", errors.New(runtimeExecutionProofError)
	}
	return identity{ID: resolved.ActorID, Provenance: provenance{
		Source: "RUNTIME_EXECUTION", Reference: execution.RevisionID,
		Revision: execution.Generation, DigestSHA256: execution.RevisionDigest,
	}}, execution.ActorKind, nil
}

func runtimeExecutionProofExpiry(now, producerExpiry, leaseExpiry, grantExpiry time.Time) (time.Time, error) {
	if leaseExpiry.IsZero() || grantExpiry.IsZero() {
		return time.Time{}, errors.New(runtimeExecutionProofError)
	}
	expiresAt := producerExpiry
	for _, limit := range []time.Time{leaseExpiry, grantExpiry} {
		if limit.Before(expiresAt) {
			expiresAt = limit
		}
	}
	expiresAt = expiresAt.UTC().Truncate(time.Second)
	if !expiresAt.After(now) {
		return time.Time{}, errors.New(runtimeExecutionProofError)
	}
	return expiresAt, nil
}
