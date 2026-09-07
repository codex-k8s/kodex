package platform

import (
	"context"
	_ "embed"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/runtime_materialization_resolve_proof.sql
var runtimeMaterializationResolveProofSQL string

func runtimeMaterializationProofOperation(operation string) bool {
	return operation == runtimeMaterializationOperation || operation == assistantMaterializationOperation
}

func (repository *Repository) resolveRuntimeMaterializationProof(ctx context.Context, input platformrepo.ProofPrincipalInput) (platformrepo.ProofAuthority, error) {
	assistant := input.Operation == assistantMaterializationOperation
	decoded, digestErr := hex.DecodeString(input.RequestDigestSHA256)
	if input.CallerWorkload != "runtime-controller" || !runtimeMaterializationProofOperation(input.Operation) ||
		input.ExternalActorID != "kodex-system-subject" || input.ExternalTenantID != "kodex-installation" ||
		(assistant && input.ProjectRef != "") || (!assistant && input.ProjectRef == "") ||
		digestErr != nil || len(decoded) != 32 || strings.ToLower(input.RequestDigestSHA256) != input.RequestDigestSHA256 {
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	var authority platformrepo.ProofAuthority
	var systemActorID string
	var systemUpdatedAt time.Time
	err := repository.pool.QueryRow(ctx, queryResolveSystemWorkloadIdentity).Scan(
		&systemActorID, &authority.OrganizationID, &systemUpdatedAt, &authority.OrganizationVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	if err != nil {
		return platformrepo.ProofAuthority{}, errs.ErrUnavailable
	}
	var execution platformrepo.RuntimeExecutionProof
	var actorKind string
	var actorUpdatedAt time.Time
	err = repository.pool.QueryRow(ctx, runtimeMaterializationResolveProofSQL, pgx.StrictNamedArgs{
		"organization_id": authority.OrganizationID, "operation": input.Operation,
		"request_digest": input.RequestDigestSHA256, "project_ref": input.ProjectRef,
		"system_assistant": assistant,
	}).Scan(&authority.ActorID, &actorKind, &actorUpdatedAt, &authority.ProjectID, &authority.ProjectVersion,
		&execution.RevisionID, &execution.Generation, &execution.RevisionDigest, &execution.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	if err != nil {
		return platformrepo.ProofAuthority{}, errs.ErrUnavailable
	}
	switch actorKind {
	case "USER":
		execution.ActorKind = "HUMAN"
	case "SERVICE":
		execution.ActorKind = "SERVICE"
	default:
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	if actorUpdatedAt.UnixMicro() <= 0 {
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	authority.ActorVersion = uint64(actorUpdatedAt.UnixMicro())
	authority.RuntimeExecution = &execution
	return authority, nil
}
