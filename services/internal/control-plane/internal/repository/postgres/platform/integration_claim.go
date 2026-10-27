package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// integrationInvocationCandidate keeps only the existing owner-selected claim row, including
// its immutable SSH snapshot. It does not derive actor authority from transported payloads.
// integrationInvocationCandidate хранит существующую owner-selected claim row вместе с
// immutable SSH snapshot; authority не выводится из транспортного payload.
type integrationInvocationCandidate struct {
	id, ref, state, connectionRef, definitionKey, capabilityKey, nodeID, runID string
	initiatorRef                                                               string
	definitionVersion, definitionDigest, operation, risk, approvalPolicy       string
	resourceKind, resourceScopeDigest, effectKey, inputDigest                  string
	approvalScopeDigest, approvalSchemaDigest                                  string
	approvalScopePaths                                                         []string
	grantRef                                                                   string
	grantVersion                                                               int64
	generation                                                                 int64
	configuration, boundedInput, resourceScope                                 []byte
	sshSnapshot                                                                []byte
	credential                                                                 entity.IntegrationCredentialRevision
	credentialCreatedAt                                                        *time.Time
}

// claimIntegrationInvocationCandidateTx preserves the common adapter validation/claim pipeline.
// The SSH caller owns a savepoint around every check, including target actor ACL, so a permanent
// denial can become a durable admission block after all partial lease writes are rolled back.
// Ordinary adapters retain their previous skip/error/lease behavior.
// claimIntegrationInvocationCandidateTx сохраняет общий adapter validation/claim pipeline.
// SSH caller открывает savepoint до всех проверок, включая target actor ACL; permanent denial
// становится durable admission block после отката частичных lease writes. Ordinary adapters
// сохраняют прежние skip/error/lease переходы.
func (repository *Repository) claimIntegrationInvocationCandidateTx(ctx context.Context, tx pgx.Tx, scope scope, principal value.Principal, instance string, sshLane bool, item integrationInvocationCandidate) (map[string]any, error) {
	initiatorScope := scope
	initiatorScope.actorRef = item.initiatorRef
	if err := repository.requireAccess(ctx, tx, initiatorScope, "integration.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: item.connectionRef}); err != nil {
		if sshLane {
			return nil, err
		}
		return nil, nil
	}
	if item.definitionKey == "context7" {
		if err := requireCurrentManagedMCPHealth(ctx, tx, scope.organizationID, item.nodeID, item.connectionRef); err != nil {
			return nil, err
		}
	}
	definition, err := repository.integrationPackage(ctx, tx, scope.organizationID, item.connectionRef, item.definitionKey, item.definitionVersion, item.definitionDigest)
	if err != nil {
		return nil, err
	}
	capability, capabilityOK := definition.Capability(item.capabilityKey)
	if !capabilityOK || item.grantVersion < 1 || item.grantRef == "" ||
		capability.Risk != item.risk || capability.Operation != item.operation ||
		definition.ValidateInvocationApprovalPolicy(capability, item.approvalPolicy, item.boundedInput) != nil {
		return nil, errs.ErrForbidden
	}
	if item.approvalPolicy == string(integrationpackage.ApprovalHumanScoped) {
		resolvedScope, scopeErr := capability.ResolveApprovalScope(item.approvalScopePaths, item.boundedInput)
		schemaDigest, digestErr := capability.InputSchemaDigest()
		if scopeErr != nil || digestErr != nil || resolvedScope.Digest != item.approvalScopeDigest ||
			schemaDigest != item.approvalSchemaDigest {
			return nil, errs.ErrForbidden
		}
	}
	leaseRef, _ := newRef("lea")
	fence, _ := newRef("eff")
	digest := sha256.Sum256([]byte(fence))
	generation := item.generation + 1
	expiresAt := time.Now().UTC().Add(30 * time.Second)
	deadline, expired, deadlineErr := readRuntimeExecutionDeadline(ctx, tx, scope.organizationID, item.runID)
	if deadlineErr != nil {
		return nil, deadlineErr
	}
	if expired {
		return nil, nil
	}
	if deadline != nil && deadline.EffectiveDeadlineAt.Before(expiresAt) {
		expiresAt = deadline.EffectiveDeadlineAt
	}
	tag, err := tx.Exec(ctx, queryWorkersClaimintegrationinvocationsClaimInvocationLease, item.id, leaseRef, hex.EncodeToString(digest[:]), generation, instance, expiresAt, principal.CallerWorkload, item.state)
	if err != nil || tag.RowsAffected() != 1 {
		return nil, errs.ErrConflict
	}
	configuration, bounded := map[string]any{}, map[string]any{}
	resourceScope := map[string]string{}
	_ = json.Unmarshal(item.configuration, &configuration)
	_ = json.Unmarshal(item.boundedInput, &bounded)
	_ = json.Unmarshal(item.resourceScope, &resourceScope)
	workMode := "EXECUTE"
	if item.state == "UNKNOWN_OUTCOME" {
		workMode = "RECOVER_READ_ONLY"
	}
	claim := map[string]any{
		"invocationRef": item.ref, "connectionRef": item.connectionRef, "definitionKey": item.definitionKey,
		"definitionPackage": asJSON(definition),
		"capabilityKey":     item.capabilityKey, "configuration": configuration, "boundedInput": bounded,
		"definitionVersion": item.definitionVersion, "definitionDigest": item.definitionDigest,
		"operation": item.operation, "risk": item.risk, "approvalPolicy": item.approvalPolicy,
		"grantRef": item.grantRef, "grantVersion": item.grantVersion,
		"resourceKind": item.resourceKind, "resourceScope": resourceScope,
		"resourceScopeDigest": item.resourceScopeDigest, "effectKey": item.effectKey, "inputDigest": item.inputDigest,
		"workMode": workMode,
		"leaseRef": leaseRef, "fence": fence, "generation": generation, "expiresAt": expiresAt,
	}
	if sshLane {
		purpose := ""
		if workMode == "RECOVER_READ_ONLY" {
			purpose = "STATUS"
			if _, err := tx.Exec(ctx, querySSHComposeRecovery, pgx.StrictNamedArgs{"invocation_id": item.id}); err != nil {
				return nil, errs.ErrUnavailable
			}
		}
		prepared, prepareErr := repository.issueSSHComposePermit(ctx, tx, scope, platformrepo.SSHComposeLeaseBinding{
			WorkloadInstance: instance, InvocationRef: item.ref, LeaseRef: leaseRef, Fence: fence, Generation: generation,
		}, purpose, false)
		if prepareErr != nil {
			return nil, prepareErr
		}
		claim["sshSnapshot"], claim["sshPermit"], claim["expiresAt"] = prepared["snapshot"], prepared["permit"], prepared["expiresAt"]
	}
	if definition.RequiresConnectionCredential() && item.credential.Ref != "" && item.credentialCreatedAt != nil {
		item.credential.CreatedAt = *item.credentialCreatedAt
		claim["credential"] = item.credential
	}
	return claim, nil
}
