package grpc

import (
	"context"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

func (server *Server) ListProviderAccountBlockers(ctx context.Context, request *cp.ListProviderAccountBlockersRequest) (*cp.ListProviderAccountBlockersResponse, error) {
	p, err := principal(ctx, cp.PlatformQueryService_ListProviderAccountBlockers_FullMethodName)
	if err != nil {
		return nil, err
	}
	kind := ""
	if request.GetKind() != cp.ProviderAccountBlockerKind_PROVIDER_ACCOUNT_BLOCKER_KIND_UNSPECIFIED {
		name, known := cp.ProviderAccountBlockerKind_name[int32(request.GetKind())]
		if !known {
			return nil, transportError(errs.ErrInvalid)
		}
		kind = strings.TrimPrefix(name, "PROVIDER_ACCOUNT_BLOCKER_KIND_")
	}
	result, err := server.service.ListProviderAccountBlockers(ctx, p, query.ProviderAccountBlockers{
		AccountRef: request.GetAccountRef(), Kind: kind, Query: request.GetQuery(), Page: page(request.GetPage()),
	})
	if err != nil {
		return nil, transportError(err)
	}
	response := &cp.ListProviderAccountBlockersResponse{Total: result.Total, HiddenCount: result.HiddenCount,
		AccountVersion: result.AccountVersion, DeletionIntentVersion: result.DeletionIntentVersion,
		ContextDigest: result.ContextDigest, Page: &cp.PageInfo{NextPageToken: result.NextPageToken}}
	for _, item := range result.Items {
		kind := cp.ProviderAccountBlockerKind_value["PROVIDER_ACCOUNT_BLOCKER_KIND_"+item.Kind]
		if kind == 0 || item.Version < 1 || item.CanCancel && item.Kind != "QUEUED_TURN" {
			return nil, transportError(errs.ErrUnavailable)
		}
		response.Items = append(response.Items, &cp.ProviderAccountBlocker{
			Kind: cp.ProviderAccountBlockerKind(kind), Ref: item.Ref, Version: item.Version,
			Name: item.Name, ProjectRef: item.ProjectRef, CanCancel: item.CanCancel,
		})
	}
	return response, nil
}

func (server *Server) CancelProviderAccountQueuedWork(ctx context.Context, request *cp.CancelProviderAccountQueuedWorkRequest) (*cp.CancelProviderAccountQueuedWorkResponse, error) {
	result, err := execute(ctx, server.service, cp.PlatformCommandService_CancelProviderAccountQueuedWork_FullMethodName,
		command.CancelProviderAccountQueuedWork, request.GetMutation(), command.ProviderAccountInput{
			AccountRef: request.GetAccountRef(), SelectedRunRefs: request.GetSelectedRunRefs(), BlockersDigest: request.GetBlockersDigest(),
		})
	if err != nil {
		return nil, err
	}
	if result.ProviderAccount == nil || len(result.ProviderQueuedWorkResults) != len(request.GetSelectedRunRefs()) {
		return nil, transportError(errs.ErrUnavailable)
	}
	response := &cp.CancelProviderAccountQueuedWorkResponse{Account: castProviderAccount(*result.ProviderAccount)}
	for index, item := range result.ProviderQueuedWorkResults {
		outcome := cp.ProviderAccountQueuedWorkOutcome_value["PROVIDER_ACCOUNT_QUEUED_WORK_OUTCOME_"+item.Outcome]
		if outcome == 0 || item.RunRef != request.GetSelectedRunRefs()[index] {
			return nil, transportError(errs.ErrUnavailable)
		}
		response.Outcomes = append(response.Outcomes, &cp.ProviderAccountQueuedWorkResult{RunRef: item.RunRef, Outcome: cp.ProviderAccountQueuedWorkOutcome(outcome)})
	}
	return response, nil
}

func castProviderAccountDeletion(value *entity.ProviderAccountDeletion) *cp.ProviderAccountDeletion {
	if value == nil {
		return nil
	}
	result := &cp.ProviderAccountDeletion{Ref: value.Ref, Version: value.Version,
		State:          cp.ProviderAccountDeletionState(cp.ProviderAccountDeletionState_value["PROVIDER_ACCOUNT_DELETION_STATE_"+value.State]),
		PendingCleanup: value.PendingCleanup, RequestedAt: timestamp(value.RequestedAt),
		CompletedAt: optionalTimestamp(value.CompletedAt), SafeReason: value.SafeReason}
	for _, item := range value.Blockers {
		result.Blockers = append(result.Blockers, &cp.ProviderAccountBlockerCount{
			Kind: cp.ProviderAccountBlockerKind(cp.ProviderAccountBlockerKind_value["PROVIDER_ACCOUNT_BLOCKER_KIND_"+item.Kind]), Total: item.Total,
		})
	}
	return result
}

func castProviderAccountVerification(value *entity.ProviderAccountVerification) *cp.ProviderAccountVerification {
	if value == nil {
		return nil
	}
	return &cp.ProviderAccountVerification{Ref: value.Ref, AccountVersion: value.AccountVersion, CredentialRevision: value.CredentialRevision,
		State:       cp.ProviderAccountVerificationState(cp.ProviderAccountVerificationState_value["PROVIDER_ACCOUNT_VERIFICATION_STATE_"+value.State]),
		Scope:       cp.ProviderAccountVerificationScope(cp.ProviderAccountVerificationScope_value["PROVIDER_ACCOUNT_VERIFICATION_SCOPE_"+value.Scope]),
		RequestedAt: timestamp(value.RequestedAt), CompletedAt: optionalTimestamp(value.CompletedAt), SafeReason: value.SafeReason}
}
