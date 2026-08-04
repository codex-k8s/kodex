package resource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

func (service *Service) ReportRuntimeProgress(
	ctx context.Context,
	input ReportRuntimeProgressInput,
) (ReportRuntimeProgressResult, error) {
	if err := authorize(input.Principal, permissionRuntimeProgress); err != nil {
		return ReportRuntimeProgressResult{}, err
	}
	if input.Principal.CallerWorkload != agentRunnerWorkload ||
		input.Principal.CallerSPIFFEID != agentRunnerSPIFFEID ||
		value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.TurnID) != nil || value.ValidateID(input.ExecutionID) != nil ||
		input.ExpectedTurnVersion == 0 || input.ExpectedExecutionVersion == 0 || input.ExpectedFence == 0 ||
		input.Attempt == 0 || input.AuthorityGeneration == 0 || input.Sequence == 0 ||
		(input.Kind != "STATUS" && input.Kind != "PROGRESS") ||
		len(input.Markdown) == 0 || len(input.Markdown) > 60<<10 || !utf8.ValidString(input.Markdown) ||
		strings.ContainsRune(input.Markdown, '\x00') || len(input.LeaseToken) != 64 ||
		input.Principal.AuthorityReference != input.TurnID ||
		input.Principal.AuthorityRevision != uint64(input.Attempt) ||
		input.Principal.AuthorityGrantGeneration != input.AuthorityGeneration {
		return ReportRuntimeProgressResult{}, errs.ErrInvalidInput
	}
	requestHash, err := semanticCommandHash(input.Principal, struct {
		TurnID, ExecutionID, Kind, MarkdownSHA256, LeaseTokenSHA256 string
		TurnVersion, ExecutionVersion, Fence, Generation            uint64
		Attempt, Sequence                                           uint32
	}{input.TurnID, input.ExecutionID, input.Kind, hashString(input.Markdown), hashString(input.LeaseToken),
		input.ExpectedTurnVersion, input.ExpectedExecutionVersion, input.ExpectedFence,
		input.AuthorityGeneration, input.Attempt, input.Sequence})
	if err != nil {
		return ReportRuntimeProgressResult{}, errs.ErrInvalidInput
	}
	var result ReportRuntimeProgressResult
	err = service.withLifecycleReceipt(ctx, input.Principal, input.IdempotencyKey,
		"report_runtime_progress", requestHash, &result,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			execution, graph, checkErr := service.liveRunnerGraph(ctx, tx, input)
			if checkErr != nil {
				return 0, checkErr
			}
			result.Turn, result.Execution = graph.Turn, execution
			return lifecycleReceiptApplyOrReplay, nil
		},
		func() error {
			if result.DeliveryID == "" || result.Execution.ID != input.ExecutionID || result.Turn.ID != input.TurnID {
				return errs.ErrStateConflict
			}
			return nil
		},
		func(tx domainrepo.Transaction) error {
			execution, graph, checkErr := service.liveRunnerGraph(ctx, tx, input)
			if checkErr != nil {
				return checkErr
			}
			turnSpec, ok := graph.Turn.Spec.(entity.TurnSpec)
			if !ok {
				return errs.ErrStateConflict
			}
			digest := sha256.Sum256([]byte(input.Markdown))
			artifactID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("control-plane:runtime-progress:"+
				execution.ID+":"+input.Kind+":"+strconv.FormatUint(uint64(input.Sequence), 10))).String()
			deliveryID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("control-plane:runtime-progress-delivery:"+
				execution.ID+":"+input.Kind+":"+strconv.FormatUint(uint64(input.Sequence), 10))).String()
			work := domainrepo.InteractionDeliveryWork{ID: deliveryID,
				OrganizationID: execution.OrganizationID, ProjectID: execution.ProjectID,
				ActorID: graph.Turn.OwnerActorID, SessionID: graph.Session.ID, SessionVersion: graph.Session.Version,
				TurnID: graph.Turn.ID, TurnVersion: graph.Turn.Version, Attempt: execution.Attempt,
				RuntimeRevisionID: execution.RuntimeRevisionID, RuntimeRevisionVersion: execution.RuntimeRevisionVersion,
				ImmutableInputSHA256: execution.ImmutableInputSHA256, Kind: input.Kind,
				LifecycleState: string(graph.Turn.State), Outcome: turnSpec.Outcome,
				ArtifactID: artifactID, ArtifactVersion: 1, ArtifactSHA256: hex.EncodeToString(digest[:]),
				ArtifactName: strings.ToLower(input.Kind) + ".md", ArtifactStorageRef: "control-plane-inline:" + artifactID,
				ArtifactSizeBytes: uint64(len(input.Markdown)), ArtifactMediaType: "text/markdown",
				InlinePayload: []byte(input.Markdown)}
			if err := tx.EnqueueInteractionDelivery(ctx, work); err != nil {
				return err
			}
			result = ReportRuntimeProgressResult{DeliveryID: deliveryID, Turn: graph.Turn, Execution: execution}
			return nil
		})
	return result, err
}

func (service *Service) liveRunnerGraph(ctx context.Context, tx domainrepo.Transaction,
	input ReportRuntimeProgressInput) (RuntimeExecution, lockedOwnerGraph, error) {
	graph, err := service.lockOwnerGraphByTurn(ctx, tx, input.Principal, input.TurnID)
	if err != nil || graph.Runtime == nil {
		return RuntimeExecution{}, lockedOwnerGraph{}, err
	}
	execution := *graph.Runtime
	lease, err := tx.GetTurnLeaseForUpdate(ctx, input.TurnID)
	if err != nil {
		return RuntimeExecution{}, lockedOwnerGraph{}, err
	}
	now, err := tx.CurrentTime(ctx)
	if err != nil {
		return RuntimeExecution{}, lockedOwnerGraph{}, err
	}
	if execution.ID != input.ExecutionID || execution.Version != input.ExpectedExecutionVersion ||
		execution.Fence != input.ExpectedFence || execution.GrantGeneration != input.AuthorityGeneration ||
		(execution.State != "ADMITTED" && execution.State != "RUNNING") ||
		graph.Turn.Version != input.ExpectedTurnVersion || graph.Turn.State != enum.StateClaimed ||
		lease.Attempt != input.Attempt || lease.AuthorityGeneration != input.AuthorityGeneration ||
		lease.WorkloadID != agentRunnerWorkload || lease.TokenHash != hashString(input.LeaseToken) ||
		!lease.ExpiresAt.After(now) || !execution.LeaseExpiresAt.After(now) {
		return RuntimeExecution{}, lockedOwnerGraph{}, errs.ErrStateConflict
	}
	return execution, graph, nil
}

func (service *Service) GetRuntimeMaterialization(ctx context.Context, principal value.Principal,
	executionID, artifactID string, artifactVersion uint64, artifactSHA256 string,
) (RuntimeMaterializationResult, error) {
	if err := authorize(principal, permissionRuntimeRead); err != nil {
		return RuntimeMaterializationResult{}, err
	}
	if principal.CallerWorkload != service.integrationGatewayWorkload ||
		principal.CallerSPIFFEID != service.integrationGatewaySPIFFEID ||
		value.ValidateID(executionID) != nil || value.ValidateID(artifactID) != nil || artifactVersion == 0 ||
		!validSHA256Text(artifactSHA256) || value.ValidateID(principal.AuthorityReference) != nil ||
		principal.AuthorityRevision == 0 || principal.AuthorityGrantGeneration == 0 {
		return RuntimeMaterializationResult{}, errs.ErrPermissionDenied
	}
	var result RuntimeMaterializationResult
	err := service.repository.Transact(ctx, domainrepo.Scope{OrganizationID: principal.OrganizationID,
		ProjectID: principal.ProjectID, ActorID: principal.ActorID}, func(tx domainrepo.Transaction) error {
		execution, err := tx.GetRuntimeExecutionForUpdate(ctx, executionID)
		if err != nil {
			return err
		}
		if execution.TurnID != principal.AuthorityReference ||
			execution.Attempt != uint32(principal.AuthorityRevision) ||
			execution.GrantGeneration != principal.AuthorityGrantGeneration || runtimeTerminal(execution.State) {
			return errs.ErrNotFound
		}
		matches := 0
		for _, item := range execution.Materializations {
			if item.ArtifactID == artifactID && item.ArtifactVersion == artifactVersion && item.SHA256 == artifactSHA256 {
				result = RuntimeMaterializationResult{OrganizationID: execution.OrganizationID,
					ProjectID: execution.ProjectID, ExecutionVersion: execution.Version, Fence: execution.Fence,
					GrantGeneration: execution.GrantGeneration, Materialization: item}
				matches++
			}
		}
		if matches != 1 {
			return errs.ErrNotFound
		}
		return nil
	})
	return result, err
}
