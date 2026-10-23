package authorityproof

import (
	"errors"
	"time"

	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/google/uuid"
)

// runtimeExecutionProofError is the fixed refusal diagnostic for invalid runtime provenance or an
// exhausted execution/grant deadline, without exposing private owner details.
// runtimeExecutionProofError — фиксированная диагностика отказа при неверном происхождении выполнения
// либо исчерпанном сроке выполнения и grant без выдачи закрытых данных владельца.
const runtimeExecutionProofError = "runtime execution authority is unavailable"

// runtimeMaterializationProofRequired selects only project and system-assistant credential
// materialization operations that require owner-proven runtime execution lineage.
// runtimeMaterializationProofRequired выбирает только операции материализации учётных данных проекта и
// системного помощника, требующие подтверждённого владельцем происхождения выполнения.
func runtimeMaterializationProofRequired(operation string) bool {
	return operation == "platform.runtime.credentials.materialize" ||
		operation == "platform.runtime.credentials.system-assistant.materialize"
}

// runtimeExecutionActor accepts only runtime-controller materialization with valid owner-resolved
// actor/tenant/revision pins, HUMAN or SERVICE kind and the exact project-versus-system scope, then
// returns immutable-execution provenance instead of generic worker identity.
// runtimeExecutionActor принимает только материализацию runtime-controller с допустимыми разрешёнными
// владельцем исполнителем, организацией и ревизией, видом HUMAN либо SERVICE и точной областью проекта
// или системы, затем возвращает происхождение неизменяемого выполнения вместо общей личности нагрузки.
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

// runtimeExecutionProofExpiry takes the minimum of producer, nonzero lease and grant expiries, truncates
// to whole UTC seconds and refuses a result not after now; it never extends an upstream lifetime.
// runtimeExecutionProofExpiry берёт минимум сроков производителя, непустых lease и grant, усекает до
// целых секунд UTC и отклоняет результат не позже now, никогда не продлевая исходный срок.
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
