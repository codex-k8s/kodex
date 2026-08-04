package resource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
		TurnID, ExecutionID, Kind, MarkdownSHA256 string
		Generation                                uint64
		Attempt, Sequence                         uint32
	}{input.TurnID, input.ExecutionID, input.Kind, hashString(input.Markdown),
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
	if execution.ID != input.ExecutionID || execution.Version < input.ExpectedExecutionVersion ||
		execution.Fence < input.ExpectedFence || execution.Version-input.ExpectedExecutionVersion != execution.Fence-input.ExpectedFence ||
		execution.GrantGeneration != input.AuthorityGeneration ||
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
	if principal.CallerWorkload != service.ownerGateDeliveryWorkload ||
		principal.CallerSPIFFEID != service.ownerGateDeliverySPIFFEID ||
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

func (service *Service) AuthorizeRuntimeOutput(ctx context.Context, principal value.Principal,
	executionID string, output RuntimeOutputMetadata) (RuntimeOutputAuthorization, error) {
	if err := authorize(principal, permissionRuntimeOutputStage); err != nil {
		return RuntimeOutputAuthorization{}, err
	}
	if !service.interactionGatewayPrincipal(principal) || value.ValidateID(executionID) != nil ||
		validateStagedRuntimeOutput(output) != nil {
		return RuntimeOutputAuthorization{}, errs.ErrPermissionDenied
	}
	var result RuntimeOutputAuthorization
	err := service.repository.Transact(ctx, domainrepo.Scope{OrganizationID: principal.OrganizationID,
		ProjectID: principal.ProjectID, ActorID: principal.ActorID}, func(tx domainrepo.Transaction) error {
		execution, err := tx.GetRuntimeExecutionForUpdate(ctx, executionID)
		if err != nil {
			return err
		}
		if err := requireExactRuntimeApplicationAuthority(execution, principal); err != nil {
			return err
		}
		if execution.State != "ADMITTED" && execution.State != "RUNNING" {
			return errs.ErrStateConflict
		}
		if err := service.requireRuntimeOwner(ctx, tx, principal, execution); err != nil {
			return err
		}
		result = RuntimeOutputAuthorization{OrganizationID: execution.OrganizationID,
			ProjectID: execution.ProjectID, ExecutionVersion: execution.Version,
			Fence: execution.Fence, GrantGeneration: execution.GrantGeneration}
		return nil
	})
	return result, err
}

func (service *Service) RegisterRuntimeOutput(ctx context.Context,
	input RegisterRuntimeOutputInput) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionRuntimeOutputStage); err != nil {
		return entity.Resource{}, err
	}
	if !service.interactionGatewayPrincipal(input.Principal) ||
		value.ValidateIdempotencyKey(input.IdempotencyKey) != nil || value.ValidateID(input.ExecutionID) != nil ||
		input.ExpectedExecutionVersion == 0 || input.ExpectedExecutionFence == 0 ||
		input.ExpectedGrantGeneration == 0 || validateStagedRuntimeOutput(input.Output) != nil ||
		len(input.StorageRef) < 8 || len(input.StorageRef) > 2048 ||
		!strings.HasPrefix(input.StorageRef, "s3://") || strings.ContainsAny(input.StorageRef, "\x00\r\n") {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	artifactID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("mattercodex:runtime-output:"+input.ExecutionID+":"+
		input.Output.Kind+":"+strconv.FormatUint(uint64(input.Output.Sequence), 10)+":"+input.Output.SHA256)).String()
	requestHash, err := semanticCommandHash(input.Principal, struct {
		ExecutionID, StorageRef, Kind, Name, MediaType, SHA256 string
		Version, Fence, Generation, SizeBytes                  uint64
		Sequence, Total                                        uint32
	}{input.ExecutionID, input.StorageRef, input.Output.Kind, input.Output.Name, input.Output.MediaType,
		input.Output.SHA256, input.ExpectedExecutionVersion, input.ExpectedExecutionFence,
		input.ExpectedGrantGeneration, input.Output.SizeBytes, input.Output.Sequence, input.Output.Total})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	var result, receipt entity.Resource
	err = service.withLifecycleReceipt(ctx, input.Principal, input.IdempotencyKey,
		"register_runtime_output", requestHash, &result,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			current, readErr := tx.GetForUpdate(ctx, input.Principal.OrganizationID,
				input.Principal.ProjectID, artifactID)
			if errors.Is(readErr, errs.ErrNotFound) {
				return lifecycleReceiptApply, nil
			}
			if readErr != nil {
				return 0, readErr
			}
			if !runtimeOutputArtifactMatches(current, input) {
				return 0, errs.ErrStateConflict
			}
			receipt = current
			return lifecycleReceiptReplay, nil
		},
		func() error {
			if result.ID != receipt.ID || result.Version != receipt.Version {
				return errs.ErrStateConflict
			}
			return nil
		},
		func(tx domainrepo.Transaction) error {
			execution, readErr := tx.GetRuntimeExecutionForUpdate(ctx, input.ExecutionID)
			if readErr != nil {
				return readErr
			}
			if requireExactRuntimeApplicationAuthority(execution, input.Principal) != nil ||
				execution.Version != input.ExpectedExecutionVersion || execution.Fence != input.ExpectedExecutionFence ||
				execution.GrantGeneration != input.ExpectedGrantGeneration ||
				(execution.State != "ADMITTED" && execution.State != "RUNNING") {
				return errs.ErrStateConflict
			}
			if readErr = service.requireRuntimeOwner(ctx, tx, input.Principal, execution); readErr != nil {
				return readErr
			}
			now, readErr := tx.CurrentTime(ctx)
			if readErr != nil {
				return readErr
			}
			evidence := hashString(strings.Join([]string{input.ExecutionID, input.Output.Kind,
				input.Output.Name, input.Output.SHA256, input.StorageRef}, "\x00"))
			artifact, createErr := entity.New(artifactID, execution.OrganizationID, execution.ProjectID,
				execution.TurnID, input.Principal.ActorID, enum.KindArtifact, input.Output.Name,
				entity.ArtifactSpec{ArtifactKind: "runtime-output-" + strings.ToLower(input.Output.Kind),
					Direction: "OUTPUT", StorageRef: input.StorageRef, SizeBytes: input.Output.SizeBytes,
					MediaType: input.Output.MediaType, SHA256: input.Output.SHA256, ScanStatus: "CLEAN",
					RetentionPolicyRef: "policy://runtime-output", ScanPolicyRevision: 1,
					ScanEvidenceSHA256: evidence, ScannerWorkloadID: service.ownerGateDeliveryWorkload,
					ScannedAt: now}, now)
			if createErr != nil {
				return errs.ErrInvalidInput
			}
			if readErr = tx.Insert(ctx, artifact); readErr != nil {
				return readErr
			}
			result = artifact
			return service.appendMutationRecords(ctx, tx, input.Principal, "register_runtime_output", artifact)
		},
	)
	return result, err
}

func (service *Service) interactionGatewayPrincipal(principal value.Principal) bool {
	return principal.CallerWorkload == service.ownerGateDeliveryWorkload &&
		principal.CallerSPIFFEID == service.ownerGateDeliverySPIFFEID
}

func validateStagedRuntimeOutput(output RuntimeOutputMetadata) error {
	if output.Kind != "FINAL_MARKDOWN" && output.Kind != "FILE" && output.Kind != "IMAGE" {
		return errs.ErrInvalidInput
	}
	if output.Name == "" || len(output.Name) > 255 || strings.ContainsAny(output.Name, "/\\\x00\r\n") ||
		output.MediaType == "" || len(output.MediaType) > 255 || output.SizeBytes == 0 || output.SizeBytes > 256<<20 ||
		!validSHA256Text(output.SHA256) || output.Sequence == 0 || output.Total == 0 ||
		output.Sequence > output.Total || output.Total > 4096 {
		return errs.ErrInvalidInput
	}
	if output.Kind == "FINAL_MARKDOWN" && output.MediaType != "text/markdown" ||
		output.Kind == "IMAGE" && !strings.HasPrefix(output.MediaType, "image/") {
		return errs.ErrInvalidInput
	}
	return nil
}

func runtimeOutputArtifactMatches(current entity.Resource, input RegisterRuntimeOutputInput) bool {
	spec, ok := current.Spec.(entity.ArtifactSpec)
	return ok && current.Kind == enum.KindArtifact && current.ParentID != "" &&
		current.Name == input.Output.Name && spec.Direction == "OUTPUT" &&
		spec.StorageRef == input.StorageRef && spec.SizeBytes == input.Output.SizeBytes &&
		spec.MediaType == input.Output.MediaType && spec.SHA256 == input.Output.SHA256
}
