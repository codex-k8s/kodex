package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

const (
	followUpTitleLimit   = 200
	followUpSummaryLimit = 1000
	followUpHintLimit    = 128
	followUpTypeLimit    = 64
	followUpBodyLimit    = 4000
	followUpRefTextLimit = 512
)

var followUpProviderCommandNamespace = uuid.MustParse("9f3d6f38-d3f3-4c07-aeb6-3f49e862f0c4")

type followUpIntentCommandPayload struct {
	FollowUpIntent entity.FollowUpIntent `json:"follow_up_intent"`
}

type followUpProviderCommandPayload struct {
	FollowUpIntent entity.FollowUpIntent    `json:"follow_up_intent"`
	Dispatch       followUpDispatchSnapshot `json:"dispatch"`
}

type followUpProviderCommand struct {
	Kind               FollowUpDispatchKind
	CreateIssue        *ProviderCreateIssueInput
	UpdateIssue        *ProviderUpdateIssueInput
	CreateComment      *ProviderCreateCommentInput
	UpdateComment      *ProviderUpdateCommentInput
	UpdatePullRequest  *ProviderUpdatePullRequestInput
	CreateReviewSignal *ProviderCreateReviewSignalInput
}

type followUpDispatchSnapshot struct {
	Kind                   string                             `json:"kind"`
	FollowUpIntentID       string                             `json:"follow_up_intent_id"`
	ProviderCommandID      string                             `json:"provider_command_id"`
	ProviderIdempotencyKey string                             `json:"provider_idempotency_key"`
	OperationPolicyContext ProviderOperationPolicyContext     `json:"operation_policy_context"`
	ApprovalGateRef        ProviderApprovalGateReference      `json:"approval_gate_ref,omitempty"`
	CreateIssue            *followUpCreateIssueSnapshot       `json:"create_issue,omitempty"`
	UpdateIssue            *followUpUpdateIssueSnapshot       `json:"update_issue,omitempty"`
	CreateComment          *followUpCommentSnapshot           `json:"create_comment,omitempty"`
	UpdateComment          *followUpUpdateCommentSnapshot     `json:"update_comment,omitempty"`
	UpdatePullRequest      *followUpUpdatePullRequestSnapshot `json:"update_pull_request,omitempty"`
	CreateReviewSignal     *followUpReviewSignalSnapshot      `json:"create_review_signal,omitempty"`
}

type followUpCreateIssueSnapshot struct {
	ProjectID              string                         `json:"project_id"`
	RepositoryID           string                         `json:"repository_id"`
	ProviderSlug           string                         `json:"provider_slug"`
	ExternalAccountID      string                         `json:"external_account_id"`
	RepositoryTarget       ProviderCommandTarget          `json:"repository_target"`
	Title                  string                         `json:"title"`
	BodyDigest             string                         `json:"body_digest"`
	Labels                 []string                       `json:"labels,omitempty"`
	AssigneeProviderLogins []string                       `json:"assignee_provider_logins,omitempty"`
	Milestone              string                         `json:"milestone,omitempty"`
	WorkItemType           string                         `json:"work_item_type"`
	WatermarkJSON          string                         `json:"watermark_json,omitempty"`
	OperationPolicyContext ProviderOperationPolicyContext `json:"operation_policy_context"`
	ApprovalGateRef        ProviderApprovalGateReference  `json:"approval_gate_ref,omitempty"`
}

type followUpUpdateIssueSnapshot struct {
	ExternalAccountID       string                         `json:"external_account_id"`
	Target                  ProviderCommandTarget          `json:"target"`
	Title                   string                         `json:"title,omitempty"`
	TitleSet                bool                           `json:"title_set,omitempty"`
	BodyDigest              string                         `json:"body_digest,omitempty"`
	BodySet                 bool                           `json:"body_set,omitempty"`
	Labels                  *ProviderStringListPatch       `json:"labels,omitempty"`
	AssigneeProviderLogins  *ProviderStringListPatch       `json:"assignee_provider_logins,omitempty"`
	Milestone               string                         `json:"milestone,omitempty"`
	MilestoneSet            bool                           `json:"milestone_set,omitempty"`
	State                   string                         `json:"state,omitempty"`
	StateSet                bool                           `json:"state_set,omitempty"`
	WorkItemType            string                         `json:"work_item_type,omitempty"`
	WorkItemTypeSet         bool                           `json:"work_item_type_set,omitempty"`
	WatermarkJSON           string                         `json:"watermark_json,omitempty"`
	WatermarkJSONSet        bool                           `json:"watermark_json_set,omitempty"`
	ExpectedProviderVersion string                         `json:"expected_provider_version,omitempty"`
	OperationPolicyContext  ProviderOperationPolicyContext `json:"operation_policy_context"`
	ApprovalGateRef         ProviderApprovalGateReference  `json:"approval_gate_ref,omitempty"`
}

type followUpCommentSnapshot struct {
	ExternalAccountID      string                         `json:"external_account_id"`
	Target                 ProviderCommandTarget          `json:"target"`
	BodyDigest             string                         `json:"body_digest"`
	OperationPolicyContext ProviderOperationPolicyContext `json:"operation_policy_context"`
	ApprovalGateRef        ProviderApprovalGateReference  `json:"approval_gate_ref,omitempty"`
}

type followUpUpdateCommentSnapshot struct {
	ExternalAccountID       string                         `json:"external_account_id"`
	Target                  ProviderCommandTarget          `json:"target"`
	ProviderCommentID       string                         `json:"provider_comment_id"`
	BodyDigest              string                         `json:"body_digest"`
	ExpectedProviderVersion string                         `json:"expected_provider_version,omitempty"`
	OperationPolicyContext  ProviderOperationPolicyContext `json:"operation_policy_context"`
	ApprovalGateRef         ProviderApprovalGateReference  `json:"approval_gate_ref,omitempty"`
}

type followUpUpdatePullRequestSnapshot struct {
	ExternalAccountID       string                         `json:"external_account_id"`
	Target                  ProviderCommandTarget          `json:"target"`
	Title                   string                         `json:"title,omitempty"`
	TitleSet                bool                           `json:"title_set,omitempty"`
	BodyDigest              string                         `json:"body_digest,omitempty"`
	BodySet                 bool                           `json:"body_set,omitempty"`
	Labels                  *ProviderStringListPatch       `json:"labels,omitempty"`
	AssigneeProviderLogins  *ProviderStringListPatch       `json:"assignee_provider_logins,omitempty"`
	Milestone               string                         `json:"milestone,omitempty"`
	MilestoneSet            bool                           `json:"milestone_set,omitempty"`
	State                   string                         `json:"state,omitempty"`
	StateSet                bool                           `json:"state_set,omitempty"`
	BaseBranch              string                         `json:"base_branch,omitempty"`
	BaseBranchSet           bool                           `json:"base_branch_set,omitempty"`
	MaintainerCanModify     bool                           `json:"maintainer_can_modify,omitempty"`
	MaintainerCanModifySet  bool                           `json:"maintainer_can_modify_set,omitempty"`
	WatermarkJSON           string                         `json:"watermark_json,omitempty"`
	WatermarkJSONSet        bool                           `json:"watermark_json_set,omitempty"`
	ExpectedProviderVersion string                         `json:"expected_provider_version"`
	OperationPolicyContext  ProviderOperationPolicyContext `json:"operation_policy_context"`
	ApprovalGateRef         ProviderApprovalGateReference  `json:"approval_gate_ref,omitempty"`
}

type followUpReviewSignalSnapshot struct {
	ExternalAccountID      string                         `json:"external_account_id"`
	Target                 ProviderCommandTarget          `json:"target"`
	Kind                   string                         `json:"kind"`
	BodyDigest             string                         `json:"body_digest,omitempty"`
	BodySet                bool                           `json:"body_set,omitempty"`
	InlineComments         []followUpReviewInlineSnapshot `json:"inline_comments,omitempty"`
	OperationPolicyContext ProviderOperationPolicyContext `json:"operation_policy_context"`
	ApprovalGateRef        ProviderApprovalGateReference  `json:"approval_gate_ref,omitempty"`
}

type followUpReviewInlineSnapshot struct {
	PathDigest                 string `json:"path_digest"`
	BodyDigest                 string `json:"body_digest"`
	Line                       int64  `json:"line,omitempty"`
	LineSet                    bool   `json:"line_set,omitempty"`
	StartLine                  int64  `json:"start_line,omitempty"`
	StartLineSet               bool   `json:"start_line_set,omitempty"`
	Side                       string `json:"side,omitempty"`
	StartSide                  string `json:"start_side,omitempty"`
	InReplyToProviderCommentID string `json:"in_reply_to_provider_comment_id,omitempty"`
}

func (s *Service) CreateFollowUpIntent(ctx context.Context, input CreateFollowUpIntentInput) (entity.FollowUpIntent, error) {
	if err := s.requireRepository(); err != nil {
		return entity.FollowUpIntent{}, err
	}
	if err := validateID(input.SessionID); err != nil {
		return entity.FollowUpIntent{}, err
	}
	idempotencyKey, err := followUpIntentIdempotencyKey(input.Meta)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	session, err := s.repository.GetAgentSession(ctx, input.SessionID)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	intent, err := s.normalizeFollowUpIntent(ctx, session, input, idempotencyKey)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	if replay, ok, err := findReplay(ctx, s, input.Meta, operationCreateFollowUpIntent, enum.CommandAggregateTypeFollowUp, followUpIntentFromPayload, verifyFollowUpIntentReplay(intent, s.repository.GetFollowUpIntent)); ok || err != nil {
		return replay, err
	}
	if isTerminalSessionStatus(session.Status) {
		return entity.FollowUpIntent{}, errs.ErrPreconditionFailed
	}
	now := s.clock.Now()
	intent.ID = s.idGenerator.New()
	intent.Version = 1
	intent.CreatedAt = now
	intent.UpdatedAt = now
	payload, err := marshalCommandPayload(followUpIntentCommandPayload{FollowUpIntent: intent})
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	result, err := commandResult(input.Meta, operationCreateFollowUpIntent, enum.CommandAggregateTypeFollowUp, intent.ID, payload, now)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	event, err := followUpRequestedEvent(s.idGenerator.New(), intent, now)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	return intent, s.repository.CreateFollowUpIntentWithResult(ctx, intent, result, event)
}

func (s *Service) DispatchFollowUpIntent(ctx context.Context, input DispatchFollowUpIntentInput) (entity.FollowUpIntent, error) {
	if err := s.requireRepository(); err != nil {
		return entity.FollowUpIntent{}, err
	}
	if err := validateID(input.FollowUpIntentID); err != nil {
		return entity.FollowUpIntent{}, err
	}
	if _, err := expectedVersion(input.Meta); err != nil {
		return entity.FollowUpIntent{}, err
	}
	intent, err := s.repository.GetFollowUpIntent(ctx, input.FollowUpIntentID)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	command, snapshot, err := normalizeFollowUpDispatchCommand(intent, input)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	if replay, ok, err := findReplay(ctx, s, input.Meta, operationDispatchFollowUpIntent, enum.CommandAggregateTypeFollowUp, followUpProviderIntentFromPayload, verifyFollowUpProviderReplay(snapshot, s.repository.GetFollowUpIntent)); ok || err != nil {
		return replay, err
	}
	previousVersion := *input.Meta.ExpectedVersion
	if intent.Version != previousVersion {
		return entity.FollowUpIntent{}, errs.ErrConflict
	}
	if !dispatchableFollowUpStatus(intent.Status) {
		return entity.FollowUpIntent{}, errs.ErrPreconditionFailed
	}
	reserved, err := reserveFollowUpDispatch(ctx, s, intent, previousVersion, snapshot)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	providerResult, err := dispatchFollowUpProviderCommand(ctx, s.providerFollowUpDispatcher, command)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	next, err := applyFollowUpProviderResult(reserved, providerResult, command.Kind)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	now := s.clock.Now()
	previousStatus := string(reserved.Status)
	next.Version++
	next.UpdatedAt = now
	payload, err := marshalCommandPayload(followUpProviderCommandPayload{FollowUpIntent: next, Dispatch: snapshot})
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	result, err := commandResult(input.Meta, operationDispatchFollowUpIntent, enum.CommandAggregateTypeFollowUp, next.ID, payload, now)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	event, err := followUpResultEvent(s.idGenerator.New(), previousStatus, next, now)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	return next, s.repository.UpdateFollowUpIntentWithResult(ctx, next, reserved.Version, result, event)
}

func (s *Service) normalizeFollowUpIntent(ctx context.Context, session entity.AgentSession, input CreateFollowUpIntentInput, idempotencyKey string) (entity.FollowUpIntent, error) {
	runID := input.RunID
	fromStageID := input.FromStageID
	toStageID := input.ToStageID
	acceptanceID := input.AcceptanceResultID
	var run *entity.AgentRun
	if acceptanceID != nil {
		acceptance, err := s.repository.GetAcceptanceResult(ctx, *acceptanceID)
		if err != nil {
			return entity.FollowUpIntent{}, err
		}
		if acceptance.SessionID != session.ID {
			return entity.FollowUpIntent{}, errs.ErrConflict
		}
		if !followUpAcceptanceStatus(acceptance.Status) {
			return entity.FollowUpIntent{}, errs.ErrPreconditionFailed
		}
		if err := bindOptionalUUID(&runID, acceptance.RunID); err != nil {
			return entity.FollowUpIntent{}, err
		}
		if err := bindOptionalUUID(&fromStageID, acceptance.StageID); err != nil {
			return entity.FollowUpIntent{}, err
		}
	}
	if runID != nil {
		loaded, err := s.repository.GetAgentRun(ctx, *runID)
		if err != nil {
			return entity.FollowUpIntent{}, err
		}
		if loaded.SessionID != session.ID {
			return entity.FollowUpIntent{}, errs.ErrConflict
		}
		if !followUpRunStatus(loaded.Status) {
			return entity.FollowUpIntent{}, errs.ErrPreconditionFailed
		}
		if err := bindOptionalUUID(&fromStageID, loaded.StageID); err != nil {
			return entity.FollowUpIntent{}, err
		}
		run = &loaded
	}
	providerTarget, err := normalizeFollowUpProviderTarget(input.ProviderTarget)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	if run != nil {
		runTarget, err := normalizeFollowUpProviderTarget(run.ProviderTarget)
		if err != nil {
			return entity.FollowUpIntent{}, err
		}
		providerTarget = mergeProviderTarget(providerTarget, runTarget)
	}
	if providerTarget.WorkItemRef == "" && session.ProviderWorkItemRef != "" {
		ref, err := normalizeAcceptanceTargetRef(session.ProviderWorkItemRef)
		if err != nil {
			return entity.FollowUpIntent{}, err
		}
		providerTarget.WorkItemRef = ref
	}
	if providerTargetEmpty(providerTarget) {
		return entity.FollowUpIntent{}, errs.ErrInvalidArgument
	}
	providerOperationRef, err := normalizeFollowUpOptionalRef(input.ProviderOperationRef)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	instructionDigest, err := normalizeFollowUpDigest(input.InstructionBodyDigest)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	safeTitle, err := normalizeFollowUpText(input.SafeTitle, followUpTitleLimit, true)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	safeSummary, err := normalizeFollowUpText(input.SafeSummary, followUpSummaryLimit, false)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	roleHint, err := normalizeFollowUpHint(input.RoleHint)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	stageHint, err := normalizeFollowUpHint(input.StageHint)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	flowVersionID := session.FlowVersionID
	if run != nil {
		flowVersionID = chooseUUID(run.FlowVersionID, flowVersionID)
	}
	providerWorkItemType, err := s.normalizeFollowUpTypeForStages(ctx, flowVersionID, fromStageID, toStageID, input.ProviderWorkItemType)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	return entity.FollowUpIntent{
		SessionID:             session.ID,
		RunID:                 runID,
		FromStageID:           fromStageID,
		ToStageID:             toStageID,
		AcceptanceResultID:    acceptanceID,
		ProviderTarget:        providerTarget,
		ProviderWorkItemType:  providerWorkItemType,
		ProviderOperationRef:  providerOperationRef,
		InstructionBodyDigest: instructionDigest,
		SafeTitle:             safeTitle,
		SafeSummary:           safeSummary,
		RoleHint:              roleHint,
		StageHint:             stageHint,
		IdempotencyKey:        idempotencyKey,
		Status:                enum.FollowUpIntentStatusRequested,
	}, nil
}

func followUpIntentIdempotencyKey(meta value.CommandMeta) (string, error) {
	identity, err := commandIdentity(meta, operationCreateFollowUpIntent)
	if err != nil {
		return "", err
	}
	return commandResultKey(identity), nil
}

func dispatchableFollowUpStatus(status enum.FollowUpIntentStatus) bool {
	return status == enum.FollowUpIntentStatusPlanned || status == enum.FollowUpIntentStatusRequested
}

func normalizeFollowUpDispatchCommand(intent entity.FollowUpIntent, input DispatchFollowUpIntentInput) (followUpProviderCommand, followUpDispatchSnapshot, error) {
	if !validFollowUpDispatchKind(input.DispatchKind) {
		return followUpProviderCommand{}, followUpDispatchSnapshot{}, errs.ErrInvalidArgument
	}
	providerMeta := followUpProviderCommandMeta(input.Meta, intent.ID, input.DispatchKind)
	snapshot := followUpDispatchSnapshot{
		Kind:                   string(input.DispatchKind),
		FollowUpIntentID:       intent.ID.String(),
		ProviderCommandID:      providerMeta.CommandID.String(),
		ProviderIdempotencyKey: providerMeta.IdempotencyKey,
	}
	command := followUpProviderCommand{Kind: input.DispatchKind}
	if selectedKind, ok := selectedFollowUpDispatchKind(input); !ok || selectedKind != input.DispatchKind {
		return followUpProviderCommand{}, followUpDispatchSnapshot{}, errs.ErrInvalidArgument
	}
	var err error
	var providerCommand any
	var providerDetail any
	switch input.DispatchKind {
	case FollowUpDispatchKindCreateIssue:
		createIssue, detail, normalizeErr := normalizeFollowUpCreateIssueCommand(intent, input.Meta, providerMeta, input.OperationPolicyContext, input.ApprovalGateRef, *input.CreateIssue)
		providerCommand, providerDetail, err = createIssue, detail, normalizeErr
	case FollowUpDispatchKindUpdateIssue:
		updateIssue, detail, normalizeErr := normalizeFollowUpUpdateIssueCommand(input.Meta, providerMeta, input.OperationPolicyContext, input.ApprovalGateRef, *input.UpdateIssue)
		providerCommand, providerDetail, err = updateIssue, detail, normalizeErr
	case FollowUpDispatchKindCreateComment:
		createComment, detail, normalizeErr := normalizeFollowUpCreateCommentCommand(input.Meta, providerMeta, input.OperationPolicyContext, input.ApprovalGateRef, *input.CreateComment)
		providerCommand, providerDetail, err = createComment, detail, normalizeErr
	case FollowUpDispatchKindUpdateComment:
		updateComment, detail, normalizeErr := normalizeFollowUpUpdateCommentCommand(input.Meta, providerMeta, input.OperationPolicyContext, input.ApprovalGateRef, *input.UpdateComment)
		providerCommand, providerDetail, err = updateComment, detail, normalizeErr
	case FollowUpDispatchKindUpdatePullRequest:
		updatePullRequest, detail, normalizeErr := normalizeFollowUpUpdatePullRequestCommand(input.Meta, providerMeta, input.OperationPolicyContext, input.ApprovalGateRef, *input.UpdatePullRequest)
		providerCommand, providerDetail, err = updatePullRequest, detail, normalizeErr
	case FollowUpDispatchKindCreateReviewSignal:
		reviewSignal, detail, normalizeErr := normalizeFollowUpCreateReviewSignalCommand(input.Meta, providerMeta, input.OperationPolicyContext, input.ApprovalGateRef, *input.CreateReviewSignal)
		providerCommand, providerDetail, err = reviewSignal, detail, normalizeErr
	default:
		return followUpProviderCommand{}, followUpDispatchSnapshot{}, errs.ErrInvalidArgument
	}
	if err != nil {
		return followUpProviderCommand{}, followUpDispatchSnapshot{}, err
	}
	if err := applyFollowUpDispatchCommand(&command, &snapshot, providerCommand, providerDetail); err != nil {
		return followUpProviderCommand{}, followUpDispatchSnapshot{}, err
	}
	if err := validateFollowUpDispatchTarget(intent, command); err != nil {
		return followUpProviderCommand{}, followUpDispatchSnapshot{}, err
	}
	policy, approval, err := followUpDispatchPolicy(providerCommand)
	if err != nil {
		return followUpProviderCommand{}, followUpDispatchSnapshot{}, err
	}
	snapshot.OperationPolicyContext = policy
	snapshot.ApprovalGateRef = approval
	return command, snapshot, nil
}

func applyFollowUpDispatchCommand(command *followUpProviderCommand, snapshot *followUpDispatchSnapshot, providerCommand any, detail any) error {
	switch typed := providerCommand.(type) {
	case ProviderCreateIssueInput:
		typedDetail, ok := detail.(followUpCreateIssueSnapshot)
		if !ok {
			return errs.ErrInvalidArgument
		}
		command.CreateIssue = &typed
		snapshot.CreateIssue = &typedDetail
	case ProviderUpdateIssueInput:
		typedDetail, ok := detail.(followUpUpdateIssueSnapshot)
		if !ok {
			return errs.ErrInvalidArgument
		}
		command.UpdateIssue = &typed
		snapshot.UpdateIssue = &typedDetail
	case ProviderCreateCommentInput:
		typedDetail, ok := detail.(followUpCommentSnapshot)
		if !ok {
			return errs.ErrInvalidArgument
		}
		command.CreateComment = &typed
		snapshot.CreateComment = &typedDetail
	case ProviderUpdateCommentInput:
		typedDetail, ok := detail.(followUpUpdateCommentSnapshot)
		if !ok {
			return errs.ErrInvalidArgument
		}
		command.UpdateComment = &typed
		snapshot.UpdateComment = &typedDetail
	case ProviderUpdatePullRequestInput:
		typedDetail, ok := detail.(followUpUpdatePullRequestSnapshot)
		if !ok {
			return errs.ErrInvalidArgument
		}
		command.UpdatePullRequest = &typed
		snapshot.UpdatePullRequest = &typedDetail
	case ProviderCreateReviewSignalInput:
		typedDetail, ok := detail.(followUpReviewSignalSnapshot)
		if !ok {
			return errs.ErrInvalidArgument
		}
		command.CreateReviewSignal = &typed
		snapshot.CreateReviewSignal = &typedDetail
	default:
		return errs.ErrInvalidArgument
	}
	return nil
}

func validateFollowUpDispatchTarget(intent entity.FollowUpIntent, command followUpProviderCommand) error {
	switch command.Kind {
	case FollowUpDispatchKindCreateIssue:
		return nil
	case FollowUpDispatchKindUpdateIssue:
		if command.UpdateIssue == nil {
			return errs.ErrInvalidArgument
		}
		return requireFollowUpIntentTargetMatch(intent.ProviderTarget.WorkItemRef, followUpCommandTargetRefCandidates(command.UpdateIssue.Target), true)
	case FollowUpDispatchKindCreateComment:
		if command.CreateComment == nil {
			return errs.ErrInvalidArgument
		}
		return requireFollowUpIntentTargetMatch(intent.ProviderTarget.WorkItemRef, followUpCommandTargetRefCandidates(command.CreateComment.Target), true)
	case FollowUpDispatchKindUpdateComment:
		if command.UpdateComment == nil {
			return errs.ErrInvalidArgument
		}
		if err := requireFollowUpIntentTargetMatch(intent.ProviderTarget.CommentRef, followUpCommentRefCandidates(command.UpdateComment.Target, command.UpdateComment.ProviderCommentID), true); err != nil {
			return err
		}
		return requireFollowUpIntentTargetMatch(intent.ProviderTarget.WorkItemRef, followUpCommandTargetRefCandidates(command.UpdateComment.Target), false)
	case FollowUpDispatchKindUpdatePullRequest:
		if command.UpdatePullRequest == nil {
			return errs.ErrInvalidArgument
		}
		return requireFollowUpIntentTargetMatch(intent.ProviderTarget.PullRequestRef, followUpCommandTargetRefCandidates(command.UpdatePullRequest.Target), true)
	case FollowUpDispatchKindCreateReviewSignal:
		if command.CreateReviewSignal == nil {
			return errs.ErrInvalidArgument
		}
		return requireFollowUpIntentTargetMatch(intent.ProviderTarget.PullRequestRef, followUpCommandTargetRefCandidates(command.CreateReviewSignal.Target), true)
	default:
		return errs.ErrInvalidArgument
	}
}

func requireFollowUpIntentTargetMatch(storedRef string, candidates []string, required bool) error {
	stored, err := normalizeFollowUpRefText(storedRef, required)
	if err != nil {
		return err
	}
	if stored == "" {
		return nil
	}
	for _, candidate := range candidates {
		normalized, err := normalizeFollowUpRefText(candidate, false)
		if err != nil {
			return err
		}
		if normalized == stored {
			return nil
		}
	}
	return errs.ErrInvalidArgument
}

func followUpCommandTargetRefCandidates(target ProviderCommandTarget) []string {
	candidates := []string{followUpProviderCommandTargetRef(target)}
	if target.ProviderSlug != "" && target.Number > 0 {
		kind := target.WorkItemKind
		if kind == "" {
			kind = "work_item"
		}
		candidates = append(candidates, target.ProviderSlug+":"+kind+":"+strconv.FormatInt(target.Number, 10))
		if kind != "work_item" {
			candidates = append(candidates, target.ProviderSlug+":work_item:"+strconv.FormatInt(target.Number, 10))
		}
	}
	if target.ProviderSlug != "" && target.ProviderObjectID != "" {
		candidates = append(candidates, target.ProviderSlug+":object:"+target.ProviderObjectID)
	}
	if target.WebURL != "" {
		candidates = append(candidates, target.WebURL)
		if target.ProviderSlug != "" {
			candidates = append(candidates, target.ProviderSlug+":url:"+target.WebURL)
		}
	}
	return candidates
}

func followUpCommentRefCandidates(target ProviderCommandTarget, providerCommentID string) []string {
	commentID := strings.TrimSpace(providerCommentID)
	candidates := []string{commentID}
	if commentID == "" {
		return candidates
	}
	candidates = append(candidates, "comment:"+commentID)
	if target.ProviderSlug != "" {
		candidates = append(candidates, target.ProviderSlug+":comment:"+commentID, target.ProviderSlug+":object:"+commentID)
	}
	return candidates
}

func followUpDispatchPolicy(providerCommand any) (ProviderOperationPolicyContext, ProviderApprovalGateReference, error) {
	switch typed := providerCommand.(type) {
	case ProviderCreateIssueInput:
		return typed.OperationPolicyContext, typed.ApprovalGateRef, nil
	case ProviderUpdateIssueInput:
		return typed.OperationPolicyContext, typed.ApprovalGateRef, nil
	case ProviderCreateCommentInput:
		return typed.OperationPolicyContext, typed.ApprovalGateRef, nil
	case ProviderUpdateCommentInput:
		return typed.OperationPolicyContext, typed.ApprovalGateRef, nil
	case ProviderUpdatePullRequestInput:
		return typed.OperationPolicyContext, typed.ApprovalGateRef, nil
	case ProviderCreateReviewSignalInput:
		return typed.OperationPolicyContext, typed.ApprovalGateRef, nil
	default:
		return ProviderOperationPolicyContext{}, ProviderApprovalGateReference{}, errs.ErrInvalidArgument
	}
}

func selectedFollowUpDispatchKind(input DispatchFollowUpIntentInput) (FollowUpDispatchKind, bool) {
	var selected FollowUpDispatchKind
	count := 0
	candidates := []struct {
		kind    FollowUpDispatchKind
		present bool
	}{
		{kind: FollowUpDispatchKindCreateIssue, present: input.CreateIssue != nil},
		{kind: FollowUpDispatchKindUpdateIssue, present: input.UpdateIssue != nil},
		{kind: FollowUpDispatchKindCreateComment, present: input.CreateComment != nil},
		{kind: FollowUpDispatchKindUpdateComment, present: input.UpdateComment != nil},
		{kind: FollowUpDispatchKindUpdatePullRequest, present: input.UpdatePullRequest != nil},
		{kind: FollowUpDispatchKindCreateReviewSignal, present: input.CreateReviewSignal != nil},
	}
	for _, candidate := range candidates {
		if !candidate.present {
			continue
		}
		selected = candidate.kind
		count++
	}
	return selected, count == 1
}

func normalizeFollowUpCreateIssueCommand(intent entity.FollowUpIntent, callerMeta value.CommandMeta, providerMeta value.CommandMeta, policyContext ProviderOperationPolicyContext, approvalRef ProviderApprovalGateReference, input FollowUpCreateIssueCommand) (ProviderCreateIssueInput, followUpCreateIssueSnapshot, error) {
	if input.ProjectID == uuid.Nil || input.RepositoryID == uuid.Nil || input.ExternalAccountID == uuid.Nil {
		return ProviderCreateIssueInput{}, followUpCreateIssueSnapshot{}, errs.ErrInvalidArgument
	}
	providerSlug, err := normalizeFollowUpWorkItemType(input.ProviderSlug)
	if err != nil || providerSlug == "" {
		return ProviderCreateIssueInput{}, followUpCreateIssueSnapshot{}, errs.ErrInvalidArgument
	}
	repositoryTarget, err := normalizeFollowUpRepositoryTarget(providerSlug, input.RepositoryTarget)
	if err != nil {
		return ProviderCreateIssueInput{}, followUpCreateIssueSnapshot{}, err
	}
	labels, err := normalizeFollowUpStringSet(input.Labels)
	if err != nil {
		return ProviderCreateIssueInput{}, followUpCreateIssueSnapshot{}, err
	}
	assignees, err := normalizeFollowUpStringSet(input.AssigneeProviderLogins)
	if err != nil {
		return ProviderCreateIssueInput{}, followUpCreateIssueSnapshot{}, err
	}
	milestone, err := normalizeFollowUpText(input.Milestone, followUpHintLimit, false)
	if err != nil {
		return ProviderCreateIssueInput{}, followUpCreateIssueSnapshot{}, err
	}
	bodyHint, err := normalizeFollowUpText(input.SafeBodyHint, followUpBodyLimit, false)
	if err != nil {
		return ProviderCreateIssueInput{}, followUpCreateIssueSnapshot{}, err
	}
	body := followUpIssueBody(intent, bodyHint)
	watermarkJSON, err := normalizeFollowUpCommandJSON(input.WatermarkJSON)
	if err != nil {
		return ProviderCreateIssueInput{}, followUpCreateIssueSnapshot{}, err
	}
	changedFields := followUpCreateIssueChangedFields(milestone != "", intent.ProviderWorkItemType != "", len(watermarkJSON) > 0)
	policy, err := normalizeFollowUpProviderPolicy(policyContext, followUpProviderPolicyInput{
		ProjectID:     input.ProjectID.String(),
		RepositoryID:  input.RepositoryID.String(),
		OperationType: ProviderOperationTypeCreateIssue,
		TargetRef:     providerSlug + ":repository:" + input.RepositoryID.String(),
		ChangedFields: changedFields,
	})
	if err != nil {
		return ProviderCreateIssueInput{}, followUpCreateIssueSnapshot{}, err
	}
	approval, err := normalizeFollowUpApprovalGateRef(approvalRef)
	if err != nil {
		return ProviderCreateIssueInput{}, followUpCreateIssueSnapshot{}, err
	}
	providerMeta.Actor = callerMeta.Actor
	command := ProviderCreateIssueInput{
		Meta:                   providerMeta,
		ProjectID:              input.ProjectID,
		RepositoryID:           input.RepositoryID,
		ProviderSlug:           providerSlug,
		RepositoryTarget:       repositoryTarget,
		Title:                  intent.SafeTitle,
		Body:                   body,
		Labels:                 labels,
		AssigneeProviderLogins: assignees,
		Milestone:              milestone,
		WorkItemType:           intent.ProviderWorkItemType,
		WatermarkJSON:          watermarkJSON,
		OperationPolicyContext: policy,
		ApprovalGateRef:        approval,
		ExternalAccountID:      input.ExternalAccountID,
	}
	snapshot := followUpCreateIssueSnapshot{
		ProjectID:              input.ProjectID.String(),
		RepositoryID:           input.RepositoryID.String(),
		ProviderSlug:           providerSlug,
		ExternalAccountID:      input.ExternalAccountID.String(),
		RepositoryTarget:       repositoryTarget,
		Title:                  command.Title,
		BodyDigest:             followUpBodyDigest(command.Body),
		Labels:                 append([]string(nil), labels...),
		AssigneeProviderLogins: append([]string(nil), assignees...),
		Milestone:              milestone,
		WorkItemType:           command.WorkItemType,
		WatermarkJSON:          string(watermarkJSON),
		OperationPolicyContext: policy,
		ApprovalGateRef:        approval,
	}
	return command, snapshot, nil
}

func normalizeFollowUpUpdateIssueCommand(callerMeta value.CommandMeta, providerMeta value.CommandMeta, policyContext ProviderOperationPolicyContext, approvalRef ProviderApprovalGateReference, input FollowUpUpdateIssueCommand) (ProviderUpdateIssueInput, followUpUpdateIssueSnapshot, error) {
	if input.ExternalAccountID == uuid.Nil {
		return ProviderUpdateIssueInput{}, followUpUpdateIssueSnapshot{}, errs.ErrInvalidArgument
	}
	target, err := normalizeFollowUpProviderCommandTarget(input.Target, true)
	if err != nil {
		return ProviderUpdateIssueInput{}, followUpUpdateIssueSnapshot{}, err
	}
	title, err := normalizeOptionalFollowUpText(input.SafeTitle, followUpTitleLimit, true)
	if err != nil {
		return ProviderUpdateIssueInput{}, followUpUpdateIssueSnapshot{}, err
	}
	body, err := normalizeOptionalFollowUpText(input.SafeBodyHint, followUpBodyLimit, true)
	if err != nil {
		return ProviderUpdateIssueInput{}, followUpUpdateIssueSnapshot{}, err
	}
	labels, err := normalizeFollowUpStringListPatch(input.Labels)
	if err != nil {
		return ProviderUpdateIssueInput{}, followUpUpdateIssueSnapshot{}, err
	}
	assignees, err := normalizeFollowUpStringListPatch(input.AssigneeProviderLogins)
	if err != nil {
		return ProviderUpdateIssueInput{}, followUpUpdateIssueSnapshot{}, err
	}
	milestone, err := normalizeOptionalFollowUpText(input.Milestone, followUpHintLimit, false)
	if err != nil {
		return ProviderUpdateIssueInput{}, followUpUpdateIssueSnapshot{}, err
	}
	state, err := normalizeOptionalFollowUpHint(input.State, true)
	if err != nil {
		return ProviderUpdateIssueInput{}, followUpUpdateIssueSnapshot{}, err
	}
	workItemType, err := normalizeOptionalFollowUpWorkItemType(input.ProviderWorkItemType)
	if err != nil {
		return ProviderUpdateIssueInput{}, followUpUpdateIssueSnapshot{}, err
	}
	watermarkJSON, err := normalizeOptionalFollowUpCommandJSON(input.WatermarkJSON)
	if err != nil {
		return ProviderUpdateIssueInput{}, followUpUpdateIssueSnapshot{}, err
	}
	expectedProviderVersion, err := normalizeFollowUpRefText(input.ExpectedProviderVersion, false)
	if err != nil {
		return ProviderUpdateIssueInput{}, followUpUpdateIssueSnapshot{}, err
	}
	changedFields := followUpUpdateIssueChangedFields(title, body, labels, assignees, milestone, state, workItemType, watermarkJSON)
	if len(changedFields) == 0 {
		return ProviderUpdateIssueInput{}, followUpUpdateIssueSnapshot{}, errs.ErrInvalidArgument
	}
	policy, err := normalizeFollowUpProviderPolicy(policyContext, followUpProviderPolicyInput{
		OperationType: ProviderOperationTypeUpdateIssue,
		TargetRef:     followUpProviderCommandTargetRef(target),
		ChangedFields: changedFields,
	})
	if err != nil {
		return ProviderUpdateIssueInput{}, followUpUpdateIssueSnapshot{}, err
	}
	approval, err := normalizeFollowUpApprovalGateRef(approvalRef)
	if err != nil {
		return ProviderUpdateIssueInput{}, followUpUpdateIssueSnapshot{}, err
	}
	providerMeta.Actor = callerMeta.Actor
	command := ProviderUpdateIssueInput{
		Meta:                    providerMeta,
		Target:                  target,
		Title:                   title,
		Body:                    body,
		Labels:                  labels,
		AssigneeProviderLogins:  assignees,
		Milestone:               milestone,
		State:                   state,
		WorkItemType:            workItemType,
		WatermarkJSON:           watermarkJSON,
		ExpectedProviderVersion: expectedProviderVersion,
		OperationPolicyContext:  policy,
		ApprovalGateRef:         approval,
		ExternalAccountID:       input.ExternalAccountID,
	}
	snapshot := followUpUpdateIssueSnapshot{
		ExternalAccountID:       input.ExternalAccountID.String(),
		Target:                  target,
		Title:                   optionalStringValue(command.Title),
		TitleSet:                command.Title != nil,
		BodyDigest:              optionalBodyDigest(command.Body),
		BodySet:                 command.Body != nil,
		Labels:                  cloneProviderStringListPatch(labels),
		AssigneeProviderLogins:  cloneProviderStringListPatch(assignees),
		Milestone:               optionalStringValue(command.Milestone),
		MilestoneSet:            command.Milestone != nil,
		State:                   optionalStringValue(command.State),
		StateSet:                command.State != nil,
		WorkItemType:            optionalStringValue(command.WorkItemType),
		WorkItemTypeSet:         command.WorkItemType != nil,
		WatermarkJSON:           optionalBytesString(command.WatermarkJSON),
		WatermarkJSONSet:        command.WatermarkJSON != nil,
		ExpectedProviderVersion: expectedProviderVersion,
		OperationPolicyContext:  policy,
		ApprovalGateRef:         approval,
	}
	return command, snapshot, nil
}

func normalizeFollowUpCreateCommentCommand(callerMeta value.CommandMeta, providerMeta value.CommandMeta, policyContext ProviderOperationPolicyContext, approvalRef ProviderApprovalGateReference, input FollowUpCreateCommentCommand) (ProviderCreateCommentInput, followUpCommentSnapshot, error) {
	if input.ExternalAccountID == uuid.Nil {
		return ProviderCreateCommentInput{}, followUpCommentSnapshot{}, errs.ErrInvalidArgument
	}
	target, err := normalizeFollowUpProviderCommandTarget(input.Target, true)
	if err != nil {
		return ProviderCreateCommentInput{}, followUpCommentSnapshot{}, err
	}
	body, err := normalizeFollowUpText(input.SafeBodyHint, followUpBodyLimit, true)
	if err != nil {
		return ProviderCreateCommentInput{}, followUpCommentSnapshot{}, err
	}
	policy, err := normalizeFollowUpProviderPolicy(policyContext, followUpProviderPolicyInput{
		OperationType: ProviderOperationTypeCreateComment,
		TargetRef:     followUpProviderCommandTargetRef(target),
		ChangedFields: []string{"body"},
	})
	if err != nil {
		return ProviderCreateCommentInput{}, followUpCommentSnapshot{}, err
	}
	approval, err := normalizeFollowUpApprovalGateRef(approvalRef)
	if err != nil {
		return ProviderCreateCommentInput{}, followUpCommentSnapshot{}, err
	}
	providerMeta.Actor = callerMeta.Actor
	command := ProviderCreateCommentInput{
		Meta:                   providerMeta,
		Target:                 target,
		Body:                   body,
		OperationPolicyContext: policy,
		ApprovalGateRef:        approval,
		ExternalAccountID:      input.ExternalAccountID,
	}
	snapshot := followUpCommentSnapshot{
		ExternalAccountID:      input.ExternalAccountID.String(),
		Target:                 target,
		BodyDigest:             followUpBodyDigest(body),
		OperationPolicyContext: policy,
		ApprovalGateRef:        approval,
	}
	return command, snapshot, nil
}

func normalizeFollowUpUpdateCommentCommand(callerMeta value.CommandMeta, providerMeta value.CommandMeta, policyContext ProviderOperationPolicyContext, approvalRef ProviderApprovalGateReference, input FollowUpUpdateCommentCommand) (ProviderUpdateCommentInput, followUpUpdateCommentSnapshot, error) {
	if input.ExternalAccountID == uuid.Nil {
		return ProviderUpdateCommentInput{}, followUpUpdateCommentSnapshot{}, errs.ErrInvalidArgument
	}
	target, err := normalizeFollowUpProviderCommandTarget(input.Target, true)
	if err != nil {
		return ProviderUpdateCommentInput{}, followUpUpdateCommentSnapshot{}, err
	}
	providerCommentID, err := normalizeFollowUpRefText(input.ProviderCommentID, true)
	if err != nil {
		return ProviderUpdateCommentInput{}, followUpUpdateCommentSnapshot{}, err
	}
	body, err := normalizeFollowUpText(input.SafeBodyHint, followUpBodyLimit, true)
	if err != nil {
		return ProviderUpdateCommentInput{}, followUpUpdateCommentSnapshot{}, err
	}
	expectedProviderVersion, err := normalizeFollowUpRefText(input.ExpectedProviderVersion, false)
	if err != nil {
		return ProviderUpdateCommentInput{}, followUpUpdateCommentSnapshot{}, err
	}
	policy, err := normalizeFollowUpProviderPolicy(policyContext, followUpProviderPolicyInput{
		OperationType: ProviderOperationTypeUpdateComment,
		TargetRef:     followUpProviderCommandTargetRef(target),
		ChangedFields: []string{"body"},
	})
	if err != nil {
		return ProviderUpdateCommentInput{}, followUpUpdateCommentSnapshot{}, err
	}
	approval, err := normalizeFollowUpApprovalGateRef(approvalRef)
	if err != nil {
		return ProviderUpdateCommentInput{}, followUpUpdateCommentSnapshot{}, err
	}
	providerMeta.Actor = callerMeta.Actor
	command := ProviderUpdateCommentInput{
		Meta:                    providerMeta,
		Target:                  target,
		ProviderCommentID:       providerCommentID,
		Body:                    body,
		ExpectedProviderVersion: expectedProviderVersion,
		OperationPolicyContext:  policy,
		ApprovalGateRef:         approval,
		ExternalAccountID:       input.ExternalAccountID,
	}
	snapshot := followUpUpdateCommentSnapshot{
		ExternalAccountID:       input.ExternalAccountID.String(),
		Target:                  target,
		ProviderCommentID:       providerCommentID,
		BodyDigest:              followUpBodyDigest(body),
		ExpectedProviderVersion: expectedProviderVersion,
		OperationPolicyContext:  policy,
		ApprovalGateRef:         approval,
	}
	return command, snapshot, nil
}

func normalizeFollowUpUpdatePullRequestCommand(callerMeta value.CommandMeta, providerMeta value.CommandMeta, policyContext ProviderOperationPolicyContext, approvalRef ProviderApprovalGateReference, input FollowUpUpdatePullRequestCommand) (ProviderUpdatePullRequestInput, followUpUpdatePullRequestSnapshot, error) {
	if input.ExternalAccountID == uuid.Nil {
		return ProviderUpdatePullRequestInput{}, followUpUpdatePullRequestSnapshot{}, errs.ErrInvalidArgument
	}
	target, err := normalizeFollowUpPullRequestTarget(input.Target)
	if err != nil {
		return ProviderUpdatePullRequestInput{}, followUpUpdatePullRequestSnapshot{}, err
	}
	title, err := normalizeOptionalFollowUpText(input.SafeTitle, followUpTitleLimit, true)
	if err != nil {
		return ProviderUpdatePullRequestInput{}, followUpUpdatePullRequestSnapshot{}, err
	}
	body, err := normalizeOptionalFollowUpText(input.SafeBodyHint, followUpBodyLimit, true)
	if err != nil {
		return ProviderUpdatePullRequestInput{}, followUpUpdatePullRequestSnapshot{}, err
	}
	labels, err := normalizeFollowUpStringListPatch(input.Labels)
	if err != nil {
		return ProviderUpdatePullRequestInput{}, followUpUpdatePullRequestSnapshot{}, err
	}
	assignees, err := normalizeFollowUpStringListPatch(input.AssigneeProviderLogins)
	if err != nil {
		return ProviderUpdatePullRequestInput{}, followUpUpdatePullRequestSnapshot{}, err
	}
	milestone, err := normalizeOptionalFollowUpText(input.Milestone, followUpHintLimit, false)
	if err != nil {
		return ProviderUpdatePullRequestInput{}, followUpUpdatePullRequestSnapshot{}, err
	}
	state, err := normalizeOptionalFollowUpHint(input.State, true)
	if err != nil {
		return ProviderUpdatePullRequestInput{}, followUpUpdatePullRequestSnapshot{}, err
	}
	baseBranch, err := normalizeOptionalFollowUpHint(input.BaseBranch, true)
	if err != nil {
		return ProviderUpdatePullRequestInput{}, followUpUpdatePullRequestSnapshot{}, err
	}
	watermarkJSON, err := normalizeOptionalFollowUpCommandJSON(input.WatermarkJSON)
	if err != nil {
		return ProviderUpdatePullRequestInput{}, followUpUpdatePullRequestSnapshot{}, err
	}
	expectedProviderVersion, err := normalizeFollowUpRefText(input.ExpectedProviderVersion, true)
	if err != nil {
		return ProviderUpdatePullRequestInput{}, followUpUpdatePullRequestSnapshot{}, err
	}
	changedFields := followUpUpdatePullRequestChangedFields(title, body, labels, assignees, milestone, state, baseBranch, input.MaintainerCanModify, watermarkJSON)
	if len(changedFields) == 0 {
		return ProviderUpdatePullRequestInput{}, followUpUpdatePullRequestSnapshot{}, errs.ErrInvalidArgument
	}
	policy, err := normalizeFollowUpProviderPolicy(policyContext, followUpProviderPolicyInput{
		OperationType: ProviderOperationTypeUpdatePullRequest,
		TargetRef:     followUpProviderCommandTargetRef(target),
		ChangedFields: changedFields,
	})
	if err != nil {
		return ProviderUpdatePullRequestInput{}, followUpUpdatePullRequestSnapshot{}, err
	}
	approval, err := normalizeFollowUpApprovalGateRef(approvalRef)
	if err != nil {
		return ProviderUpdatePullRequestInput{}, followUpUpdatePullRequestSnapshot{}, err
	}
	providerMeta.Actor = callerMeta.Actor
	command := ProviderUpdatePullRequestInput{
		Meta:                    providerMeta,
		Target:                  target,
		Title:                   title,
		Body:                    body,
		Labels:                  labels,
		AssigneeProviderLogins:  assignees,
		Milestone:               milestone,
		State:                   state,
		BaseBranch:              baseBranch,
		MaintainerCanModify:     input.MaintainerCanModify,
		WatermarkJSON:           watermarkJSON,
		ExpectedProviderVersion: expectedProviderVersion,
		OperationPolicyContext:  policy,
		ApprovalGateRef:         approval,
		ExternalAccountID:       input.ExternalAccountID,
	}
	snapshot := followUpUpdatePullRequestSnapshot{
		ExternalAccountID:       input.ExternalAccountID.String(),
		Target:                  target,
		Title:                   optionalStringValue(command.Title),
		TitleSet:                command.Title != nil,
		BodyDigest:              optionalBodyDigest(command.Body),
		BodySet:                 command.Body != nil,
		Labels:                  cloneProviderStringListPatch(labels),
		AssigneeProviderLogins:  cloneProviderStringListPatch(assignees),
		Milestone:               optionalStringValue(command.Milestone),
		MilestoneSet:            command.Milestone != nil,
		State:                   optionalStringValue(command.State),
		StateSet:                command.State != nil,
		BaseBranch:              optionalStringValue(command.BaseBranch),
		BaseBranchSet:           command.BaseBranch != nil,
		MaintainerCanModify:     optionalBoolValue(command.MaintainerCanModify),
		MaintainerCanModifySet:  command.MaintainerCanModify != nil,
		WatermarkJSON:           optionalBytesString(command.WatermarkJSON),
		WatermarkJSONSet:        command.WatermarkJSON != nil,
		ExpectedProviderVersion: expectedProviderVersion,
		OperationPolicyContext:  policy,
		ApprovalGateRef:         approval,
	}
	return command, snapshot, nil
}

func normalizeFollowUpCreateReviewSignalCommand(callerMeta value.CommandMeta, providerMeta value.CommandMeta, policyContext ProviderOperationPolicyContext, approvalRef ProviderApprovalGateReference, input FollowUpCreateReviewSignalCommand) (ProviderCreateReviewSignalInput, followUpReviewSignalSnapshot, error) {
	if input.ExternalAccountID == uuid.Nil {
		return ProviderCreateReviewSignalInput{}, followUpReviewSignalSnapshot{}, errs.ErrInvalidArgument
	}
	target, err := normalizeFollowUpPullRequestTarget(input.Target)
	if err != nil {
		return ProviderCreateReviewSignalInput{}, followUpReviewSignalSnapshot{}, err
	}
	kind, err := normalizeProviderReviewSignalKind(input.Kind)
	if err != nil {
		return ProviderCreateReviewSignalInput{}, followUpReviewSignalSnapshot{}, err
	}
	body, bodySet, err := normalizeFollowUpReviewSignalBody(input.SafeBodyHint)
	if err != nil {
		return ProviderCreateReviewSignalInput{}, followUpReviewSignalSnapshot{}, err
	}
	inlineComments, inlineSnapshot, err := normalizeFollowUpReviewInlineComments(input.InlineComments)
	if err != nil {
		return ProviderCreateReviewSignalInput{}, followUpReviewSignalSnapshot{}, err
	}
	if kind != ProviderReviewSignalKindApproval && body == "" && len(inlineComments) == 0 {
		return ProviderCreateReviewSignalInput{}, followUpReviewSignalSnapshot{}, errs.ErrInvalidArgument
	}
	changedFields := followUpCreateReviewSignalChangedFields(bodySet, len(inlineComments) > 0)
	policy, err := normalizeFollowUpProviderPolicy(policyContext, followUpProviderPolicyInput{
		OperationType: ProviderOperationTypeCreateReviewSignal,
		TargetRef:     followUpProviderCommandTargetRef(target),
		ChangedFields: changedFields,
	})
	if err != nil {
		return ProviderCreateReviewSignalInput{}, followUpReviewSignalSnapshot{}, err
	}
	approval, err := normalizeFollowUpApprovalGateRef(approvalRef)
	if err != nil {
		return ProviderCreateReviewSignalInput{}, followUpReviewSignalSnapshot{}, err
	}
	providerMeta.Actor = callerMeta.Actor
	command := ProviderCreateReviewSignalInput{
		Meta:                   providerMeta,
		Target:                 target,
		Kind:                   kind,
		Body:                   body,
		InlineComments:         inlineComments,
		OperationPolicyContext: policy,
		ApprovalGateRef:        approval,
		ExternalAccountID:      input.ExternalAccountID,
	}
	snapshot := followUpReviewSignalSnapshot{
		ExternalAccountID:      input.ExternalAccountID.String(),
		Target:                 target,
		Kind:                   string(kind),
		BodyDigest:             optionalReviewBodyDigest(body, bodySet),
		BodySet:                bodySet,
		InlineComments:         inlineSnapshot,
		OperationPolicyContext: policy,
		ApprovalGateRef:        approval,
	}
	return command, snapshot, nil
}

func reserveFollowUpDispatch(ctx context.Context, service *Service, intent entity.FollowUpIntent, previousVersion int64, snapshot followUpDispatchSnapshot) (entity.FollowUpIntent, error) {
	reserved := intent
	reserved.ProviderOperationRef = followUpProviderCommandRef(snapshot.ProviderCommandID)
	reserved.Version = previousVersion + 1
	reserved.UpdatedAt = service.clock.Now()
	if err := service.repository.ReserveFollowUpIntentDispatch(ctx, reserved, previousVersion); err != nil {
		return entity.FollowUpIntent{}, err
	}
	return reserved, nil
}

func normalizeFollowUpRepositoryTarget(providerSlug string, target ProviderCommandTarget) (ProviderCommandTarget, error) {
	target.ProviderSlug = strings.TrimSpace(target.ProviderSlug)
	if target.ProviderSlug == "" {
		target.ProviderSlug = providerSlug
	}
	if target.ProviderSlug != providerSlug || target.WorkItemKind != "" || target.Number != 0 || target.ProviderObjectID != "" {
		return ProviderCommandTarget{}, errs.ErrInvalidArgument
	}
	var err error
	if target.RepositoryFullName, err = normalizeFollowUpRefText(target.RepositoryFullName, false); err != nil {
		return ProviderCommandTarget{}, err
	}
	if target.ProviderRepositoryID, err = normalizeFollowUpRefText(target.ProviderRepositoryID, false); err != nil {
		return ProviderCommandTarget{}, err
	}
	if target.WebURL, err = normalizeFollowUpRefText(target.WebURL, false); err != nil {
		return ProviderCommandTarget{}, err
	}
	if target.RepositoryFullName == "" && target.ProviderRepositoryID == "" && target.WebURL == "" {
		return ProviderCommandTarget{}, errs.ErrInvalidArgument
	}
	return target, nil
}

func normalizeFollowUpProviderCommandTarget(target ProviderCommandTarget, requireArtifact bool) (ProviderCommandTarget, error) {
	var err error
	if target.ProviderSlug, err = normalizeFollowUpWorkItemType(target.ProviderSlug); err != nil || target.ProviderSlug == "" {
		return ProviderCommandTarget{}, errs.ErrInvalidArgument
	}
	if target.RepositoryFullName, err = normalizeFollowUpRefText(target.RepositoryFullName, false); err != nil {
		return ProviderCommandTarget{}, err
	}
	if target.ProviderRepositoryID, err = normalizeFollowUpRefText(target.ProviderRepositoryID, false); err != nil {
		return ProviderCommandTarget{}, err
	}
	if target.ProviderObjectID, err = normalizeFollowUpRefText(target.ProviderObjectID, false); err != nil {
		return ProviderCommandTarget{}, err
	}
	if target.WebURL, err = normalizeFollowUpRefText(target.WebURL, false); err != nil {
		return ProviderCommandTarget{}, err
	}
	if target.WorkItemKind, err = normalizeFollowUpWorkItemKind(target.WorkItemKind); err != nil {
		return ProviderCommandTarget{}, err
	}
	if target.Number < 0 {
		return ProviderCommandTarget{}, errs.ErrInvalidArgument
	}
	if target.RepositoryFullName == "" && target.ProviderRepositoryID == "" && target.ProviderObjectID == "" && target.WebURL == "" {
		return ProviderCommandTarget{}, errs.ErrInvalidArgument
	}
	if requireArtifact && target.Number == 0 && target.ProviderObjectID == "" && target.WebURL == "" {
		return ProviderCommandTarget{}, errs.ErrInvalidArgument
	}
	return target, nil
}

func normalizeFollowUpPullRequestTarget(target ProviderCommandTarget) (ProviderCommandTarget, error) {
	normalized, err := normalizeFollowUpProviderCommandTarget(target, true)
	if err != nil {
		return ProviderCommandTarget{}, err
	}
	switch normalized.WorkItemKind {
	case "pull_request", "merge_request":
		return normalized, nil
	default:
		return ProviderCommandTarget{}, errs.ErrInvalidArgument
	}
}

func normalizeFollowUpWorkItemKind(kind string) (string, error) {
	trimmed := strings.TrimSpace(kind)
	switch trimmed {
	case "", "issue", "pull_request", "merge_request":
		return trimmed, nil
	default:
		return "", errs.ErrInvalidArgument
	}
}

func normalizeFollowUpRefText(value string, required bool) (string, error) {
	trimmed := strings.TrimSpace(value)
	if required && trimmed == "" {
		return "", errs.ErrInvalidArgument
	}
	if trimmed == "" {
		return "", nil
	}
	if len(trimmed) > followUpRefTextLimit || unsafeFollowUpText(trimmed) {
		return "", errs.ErrInvalidArgument
	}
	for _, char := range trimmed {
		if char < 32 || char == 127 {
			return "", errs.ErrInvalidArgument
		}
		if !safeFollowUpProviderChar(char) {
			return "", errs.ErrInvalidArgument
		}
	}
	return trimmed, nil
}

func safeFollowUpProviderChar(char rune) bool {
	if asciiLetter(char) || asciiDigit(char) {
		return true
	}
	switch char {
	case '-', '.', '_', ':', '/', '#', '@', '+', '=', ',', '%':
		return true
	default:
		return false
	}
}

func normalizeFollowUpStringSet(values []string) ([]string, error) {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		item, err := normalizeFollowUpHint(value)
		if err != nil {
			return nil, err
		}
		if item == "" {
			return nil, errs.ErrInvalidArgument
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	sort.Strings(result)
	return result, nil
}

func normalizeFollowUpStringListPatch(patch *ProviderStringListPatch) (*ProviderStringListPatch, error) {
	if patch == nil {
		return nil, nil
	}
	values, err := normalizeFollowUpStringSet(patch.Values)
	if err != nil {
		return nil, err
	}
	return &ProviderStringListPatch{Values: values}, nil
}

func normalizeFollowUpCommandJSON(payload []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return nil, nil
	}
	normalized, err := normalizeActivityJSON(trimmed, false)
	if err != nil {
		return nil, err
	}
	if string(normalized) == "{}" {
		return nil, nil
	}
	return normalized, nil
}

func normalizeOptionalFollowUpCommandJSON(payload *[]byte) (*[]byte, error) {
	if payload == nil {
		return nil, nil
	}
	normalized, err := normalizeFollowUpCommandJSON(*payload)
	if err != nil {
		return nil, err
	}
	if normalized == nil {
		empty := []byte{}
		return &empty, nil
	}
	return &normalized, nil
}

func normalizeOptionalFollowUpText(value *string, limit int, required bool) (*string, error) {
	if value == nil {
		return nil, nil
	}
	normalized, err := normalizeFollowUpText(*value, limit, required)
	if err != nil {
		return nil, err
	}
	return &normalized, nil
}

func normalizeOptionalFollowUpHint(value *string, required bool) (*string, error) {
	if value == nil {
		return nil, nil
	}
	normalized, err := normalizeFollowUpHint(*value)
	if err != nil {
		return nil, err
	}
	if required && normalized == "" {
		return nil, errs.ErrInvalidArgument
	}
	return &normalized, nil
}

func normalizeOptionalFollowUpWorkItemType(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	normalized, err := normalizeFollowUpWorkItemType(*value)
	if err != nil {
		return nil, err
	}
	if normalized == "" {
		return nil, errs.ErrInvalidArgument
	}
	return &normalized, nil
}

func followUpIssueBody(intent entity.FollowUpIntent, bodyHint string) string {
	if bodyHint != "" {
		return bodyHint
	}
	if intent.SafeSummary != "" {
		return intent.SafeSummary
	}
	return intent.SafeTitle
}

func followUpBodyDigest(body string) string {
	sum := sha256.Sum256([]byte(body))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func optionalBodyDigest(body *string) string {
	if body == nil {
		return ""
	}
	return followUpBodyDigest(*body)
}

func optionalStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func optionalBytesString(value *[]byte) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func optionalBoolValue(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}

func cloneProviderStringListPatch(patch *ProviderStringListPatch) *ProviderStringListPatch {
	if patch == nil {
		return nil
	}
	return &ProviderStringListPatch{Values: append([]string(nil), patch.Values...)}
}

func normalizeProviderReviewSignalKind(kind ProviderReviewSignalKind) (ProviderReviewSignalKind, error) {
	switch ProviderReviewSignalKind(strings.TrimSpace(string(kind))) {
	case ProviderReviewSignalKindComment:
		return ProviderReviewSignalKindComment, nil
	case ProviderReviewSignalKindApproval:
		return ProviderReviewSignalKindApproval, nil
	case ProviderReviewSignalKindChangesRequested:
		return ProviderReviewSignalKindChangesRequested, nil
	default:
		return "", errs.ErrInvalidArgument
	}
}

func normalizeFollowUpReviewSignalBody(value *string) (string, bool, error) {
	if value == nil {
		return "", false, nil
	}
	body, err := normalizeFollowUpText(*value, followUpBodyLimit, false)
	if err != nil {
		return "", false, err
	}
	return body, true, nil
}

func normalizeFollowUpReviewInlineComments(comments []ProviderReviewInlineComment) ([]ProviderReviewInlineComment, []followUpReviewInlineSnapshot, error) {
	if len(comments) == 0 {
		return nil, nil, nil
	}
	result := make([]ProviderReviewInlineComment, 0, len(comments))
	snapshot := make([]followUpReviewInlineSnapshot, 0, len(comments))
	for _, comment := range comments {
		path, err := normalizeFollowUpText(comment.Path, followUpRefTextLimit, true)
		if err != nil {
			return nil, nil, err
		}
		body, err := normalizeFollowUpText(comment.Body, followUpBodyLimit, true)
		if err != nil {
			return nil, nil, err
		}
		line, err := normalizeOptionalPositiveFollowUpLine(comment.Line)
		if err != nil {
			return nil, nil, err
		}
		startLine, err := normalizeOptionalPositiveFollowUpLine(comment.StartLine)
		if err != nil {
			return nil, nil, err
		}
		side, err := normalizeFollowUpHint(comment.Side)
		if err != nil {
			return nil, nil, err
		}
		startSide, err := normalizeFollowUpHint(comment.StartSide)
		if err != nil {
			return nil, nil, err
		}
		replyID, err := normalizeFollowUpRefText(comment.InReplyToProviderCommentID, false)
		if err != nil {
			return nil, nil, err
		}
		normalized := ProviderReviewInlineComment{
			Path:                       path,
			Body:                       body,
			Line:                       line,
			StartLine:                  startLine,
			Side:                       side,
			StartSide:                  startSide,
			InReplyToProviderCommentID: replyID,
		}
		result = append(result, normalized)
		snapshot = append(snapshot, followUpReviewInlineSnapshot{
			PathDigest:                 followUpBodyDigest(path),
			BodyDigest:                 followUpBodyDigest(body),
			Line:                       optionalInt64Value(line),
			LineSet:                    line != nil,
			StartLine:                  optionalInt64Value(startLine),
			StartLineSet:               startLine != nil,
			Side:                       side,
			StartSide:                  startSide,
			InReplyToProviderCommentID: replyID,
		})
	}
	return result, snapshot, nil
}

func normalizeOptionalPositiveFollowUpLine(value *int64) (*int64, error) {
	if value == nil {
		return nil, nil
	}
	if *value <= 0 {
		return nil, errs.ErrInvalidArgument
	}
	line := *value
	return &line, nil
}

func optionalInt64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func optionalReviewBodyDigest(body string, set bool) string {
	if !set {
		return ""
	}
	return followUpBodyDigest(body)
}

func followUpProviderCommandMeta(meta value.CommandMeta, intentID uuid.UUID, kind FollowUpDispatchKind) value.CommandMeta {
	result := meta
	result.CommandID = followUpProviderCommandID(intentID, kind)
	result.IdempotencyKey = followUpProviderIdempotencyKey(intentID, kind)
	result.ExpectedVersion = nil
	return result
}

func followUpProviderCommandID(intentID uuid.UUID, kind FollowUpDispatchKind) uuid.UUID {
	return uuid.NewSHA1(followUpProviderCommandNamespace, []byte("provider-"+string(kind)+":"+intentID.String()))
}

func followUpProviderIdempotencyKey(intentID uuid.UUID, kind FollowUpDispatchKind) string {
	return "agent-follow-up:" + string(kind) + ":" + intentID.String()
}

func followUpProviderCommandRef(commandID string) string {
	return "provider_command:" + strings.TrimSpace(commandID)
}

func followUpCreateIssueChangedFields(hasMilestone bool, hasWorkItemType bool, hasWatermark bool) []string {
	fields := []string{"assignee_provider_logins", "body", "labels", "title"}
	if hasMilestone {
		fields = append(fields, "milestone")
	}
	if hasWorkItemType {
		fields = append(fields, "work_item_type")
	}
	if hasWatermark {
		fields = append(fields, "watermark_json")
	}
	sort.Strings(fields)
	return fields
}

func followUpUpdateIssueChangedFields(title *string, body *string, labels *ProviderStringListPatch, assignees *ProviderStringListPatch, milestone *string, state *string, workItemType *string, watermarkJSON *[]byte) []string {
	fields := make([]string, 0, 8)
	if title != nil {
		fields = append(fields, "title")
	}
	if body != nil {
		fields = append(fields, "body")
	}
	if labels != nil {
		fields = append(fields, "labels")
	}
	if assignees != nil {
		fields = append(fields, "assignee_provider_logins")
	}
	if milestone != nil {
		fields = append(fields, "milestone")
	}
	if state != nil {
		fields = append(fields, "state")
	}
	if workItemType != nil {
		fields = append(fields, "work_item_type")
	}
	if watermarkJSON != nil {
		fields = append(fields, "watermark_json")
	}
	sort.Strings(fields)
	return fields
}

func followUpUpdatePullRequestChangedFields(title *string, body *string, labels *ProviderStringListPatch, assignees *ProviderStringListPatch, milestone *string, state *string, baseBranch *string, maintainerCanModify *bool, watermarkJSON *[]byte) []string {
	fields := make([]string, 0, 9)
	if title != nil {
		fields = append(fields, "title")
	}
	if body != nil {
		fields = append(fields, "body")
	}
	if labels != nil {
		fields = append(fields, "labels")
	}
	if assignees != nil {
		fields = append(fields, "assignee_provider_logins")
	}
	if milestone != nil {
		fields = append(fields, "milestone")
	}
	if state != nil {
		fields = append(fields, "state")
	}
	if baseBranch != nil {
		fields = append(fields, "base_branch")
	}
	if maintainerCanModify != nil {
		fields = append(fields, "maintainer_can_modify")
	}
	if watermarkJSON != nil {
		fields = append(fields, "watermark_json")
	}
	sort.Strings(fields)
	return fields
}

func followUpCreateReviewSignalChangedFields(bodySet bool, hasInlineComments bool) []string {
	fields := []string{"kind"}
	if bodySet {
		fields = append(fields, "body")
	}
	if hasInlineComments {
		fields = append(fields, "inline_comments")
	}
	sort.Strings(fields)
	return fields
}

func followUpProviderCommandTargetRef(target ProviderCommandTarget) string {
	if target.ProviderSlug != "" && target.RepositoryFullName != "" && target.Number > 0 {
		kind := target.WorkItemKind
		if kind == "" {
			kind = "work_item"
		}
		return target.ProviderSlug + ":repo:" + target.RepositoryFullName + ":" + kind + ":" + strconv.FormatInt(target.Number, 10)
	}
	if target.ProviderSlug != "" && target.ProviderObjectID != "" {
		return target.ProviderSlug + ":object:" + target.ProviderObjectID
	}
	if target.ProviderSlug != "" && target.WebURL != "" {
		return target.ProviderSlug + ":url:" + target.WebURL
	}
	return target.ProviderSlug + ":repository:" + firstNonEmptyString(target.RepositoryFullName, target.ProviderRepositoryID)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func validFollowUpDispatchKind(kind FollowUpDispatchKind) bool {
	switch kind {
	case FollowUpDispatchKindCreateIssue,
		FollowUpDispatchKindUpdateIssue,
		FollowUpDispatchKindCreateComment,
		FollowUpDispatchKindUpdateComment,
		FollowUpDispatchKindUpdatePullRequest,
		FollowUpDispatchKindCreateReviewSignal:
		return true
	default:
		return false
	}
}

type followUpProviderPolicyInput struct {
	ProjectID     string
	RepositoryID  string
	OperationType string
	TargetRef     string
	ChangedFields []string
}

func normalizeFollowUpProviderPolicy(policy ProviderOperationPolicyContext, input followUpProviderPolicyInput) (ProviderOperationPolicyContext, error) {
	if policy.RiskLevel == "" {
		return ProviderOperationPolicyContext{}, errs.ErrInvalidArgument
	}
	var err error
	policy.ProjectID, err = normalizeFollowUpPolicyScope(policy.ProjectID, input.ProjectID)
	if err != nil {
		return ProviderOperationPolicyContext{}, err
	}
	policy.RepositoryID, err = normalizeFollowUpPolicyScope(policy.RepositoryID, input.RepositoryID)
	if err != nil {
		return ProviderOperationPolicyContext{}, err
	}
	policy.OperationType = input.OperationType
	policy.TargetRef, err = normalizeFollowUpPolicyTargetRef(policy.TargetRef, input.TargetRef)
	if err != nil {
		return ProviderOperationPolicyContext{}, err
	}
	changedFields := append([]string(nil), input.ChangedFields...)
	sort.Strings(changedFields)
	policy.ChangedFields, err = normalizeFollowUpPolicyList(policy.ChangedFields)
	if err != nil {
		return ProviderOperationPolicyContext{}, err
	}
	if len(policy.ChangedFields) == 0 {
		policy.ChangedFields = append([]string(nil), changedFields...)
	}
	sort.Strings(policy.ChangedFields)
	if !sameStringSet(policy.ChangedFields, changedFields) {
		return ProviderOperationPolicyContext{}, errs.ErrInvalidArgument
	}
	policy.RiskTags, err = normalizeFollowUpPolicyList(policy.RiskTags)
	if err != nil {
		return ProviderOperationPolicyContext{}, err
	}
	if policy.Stage, err = normalizeFollowUpHint(policy.Stage); err != nil {
		return ProviderOperationPolicyContext{}, err
	}
	if policy.RoleID, err = normalizeFollowUpRefText(policy.RoleID, false); err != nil {
		return ProviderOperationPolicyContext{}, err
	}
	if policy.RoleKey, err = normalizeFollowUpHint(policy.RoleKey); err != nil {
		return ProviderOperationPolicyContext{}, err
	}
	if policy.PolicyVersion, err = normalizeFollowUpRefText(policy.PolicyVersion, false); err != nil {
		return ProviderOperationPolicyContext{}, err
	}
	if policy.PolicySnapshotRef, err = normalizeFollowUpOptionalRef(policy.PolicySnapshotRef); err != nil {
		return ProviderOperationPolicyContext{}, err
	}
	if !validFollowUpRiskLevel(policy.RiskLevel) {
		return ProviderOperationPolicyContext{}, errs.ErrInvalidArgument
	}
	return policy, nil
}

func normalizeFollowUpPolicyScope(candidate string, fallback string) (string, error) {
	trimmed := strings.TrimSpace(candidate)
	if trimmed == "" {
		trimmed = strings.TrimSpace(fallback)
	}
	if trimmed == "" {
		return "", errs.ErrInvalidArgument
	}
	if _, err := uuid.Parse(trimmed); err != nil {
		return "", errs.ErrInvalidArgument
	}
	return trimmed, nil
}

func normalizeFollowUpPolicyTargetRef(candidate string, fallback string) (string, error) {
	trimmed := strings.TrimSpace(candidate)
	if trimmed == "" {
		trimmed = strings.TrimSpace(fallback)
	}
	return normalizeFollowUpRefText(trimmed, true)
}

func normalizeFollowUpPolicyList(values []string) ([]string, error) {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		item, err := normalizeFollowUpHint(value)
		if err != nil {
			return nil, err
		}
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	sort.Strings(result)
	return result, nil
}

func validFollowUpRiskLevel(level string) bool {
	switch strings.TrimSpace(level) {
	case ProviderRiskLevelLow, ProviderRiskLevelMedium, ProviderRiskLevelHigh, ProviderRiskLevelCritical:
		return true
	default:
		return false
	}
}

func normalizeFollowUpApprovalGateRef(reference ProviderApprovalGateReference) (ProviderApprovalGateReference, error) {
	var err error
	if reference.ApprovalID, err = normalizeFollowUpRefText(reference.ApprovalID, false); err != nil {
		return ProviderApprovalGateReference{}, err
	}
	if reference.GateType, err = normalizeFollowUpHint(reference.GateType); err != nil {
		return ProviderApprovalGateReference{}, err
	}
	if reference.Decision, err = normalizeFollowUpHint(reference.Decision); err != nil {
		return ProviderApprovalGateReference{}, err
	}
	if reference.DecidedByActorID, err = normalizeFollowUpRefText(reference.DecidedByActorID, false); err != nil {
		return ProviderApprovalGateReference{}, err
	}
	if reference.DecidedAt, err = normalizeFollowUpRefText(reference.DecidedAt, false); err != nil {
		return ProviderApprovalGateReference{}, err
	}
	if reference.EvidenceRef, err = normalizeFollowUpOptionalRef(reference.EvidenceRef); err != nil {
		return ProviderApprovalGateReference{}, err
	}
	if reference.PolicyVersion, err = normalizeFollowUpRefText(reference.PolicyVersion, false); err != nil {
		return ProviderApprovalGateReference{}, err
	}
	empty := reference.ApprovalID == "" && reference.GateType == "" && reference.Decision == "" && reference.DecidedByActorID == "" && reference.DecidedAt == "" && reference.EvidenceRef == "" && reference.PolicyVersion == ""
	if empty {
		return ProviderApprovalGateReference{}, nil
	}
	if reference.ApprovalID == "" || reference.GateType == "" || reference.Decision == "" {
		return ProviderApprovalGateReference{}, errs.ErrInvalidArgument
	}
	return reference, nil
}

func sameStringSet(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if left[idx] != right[idx] {
			return false
		}
	}
	return true
}

func dispatchFollowUpProviderCommand(ctx context.Context, dispatcher ProviderFollowUpDispatcher, command followUpProviderCommand) (ProviderCommandResult, error) {
	switch command.Kind {
	case FollowUpDispatchKindCreateIssue:
		if command.CreateIssue == nil {
			return ProviderCommandResult{}, errs.ErrInvalidArgument
		}
		return dispatcher.CreateIssue(ctx, *command.CreateIssue)
	case FollowUpDispatchKindUpdateIssue:
		if command.UpdateIssue == nil {
			return ProviderCommandResult{}, errs.ErrInvalidArgument
		}
		return dispatcher.UpdateIssue(ctx, *command.UpdateIssue)
	case FollowUpDispatchKindCreateComment:
		if command.CreateComment == nil {
			return ProviderCommandResult{}, errs.ErrInvalidArgument
		}
		return dispatcher.CreateComment(ctx, *command.CreateComment)
	case FollowUpDispatchKindUpdateComment:
		if command.UpdateComment == nil {
			return ProviderCommandResult{}, errs.ErrInvalidArgument
		}
		return dispatcher.UpdateComment(ctx, *command.UpdateComment)
	case FollowUpDispatchKindUpdatePullRequest:
		if command.UpdatePullRequest == nil {
			return ProviderCommandResult{}, errs.ErrInvalidArgument
		}
		return dispatcher.UpdatePullRequest(ctx, *command.UpdatePullRequest)
	case FollowUpDispatchKindCreateReviewSignal:
		if command.CreateReviewSignal == nil {
			return ProviderCommandResult{}, errs.ErrInvalidArgument
		}
		return dispatcher.CreateReviewSignal(ctx, *command.CreateReviewSignal)
	default:
		return ProviderCommandResult{}, errs.ErrInvalidArgument
	}
}

func applyFollowUpProviderResult(intent entity.FollowUpIntent, result ProviderCommandResult, kind FollowUpDispatchKind) (entity.FollowUpIntent, error) {
	operationRef, err := normalizeFollowUpOptionalRef(result.ProviderOperationRef)
	if err != nil || operationRef == "" {
		return entity.FollowUpIntent{}, errs.ErrDependencyUnavailable
	}
	intent.ProviderOperationRef = operationRef
	switch result.Status {
	case ProviderOperationStatusSucceeded:
		resultRef, err := followUpProviderResultRef(result, kind)
		if err != nil {
			return entity.FollowUpIntent{}, err
		}
		switch kind {
		case FollowUpDispatchKindCreateIssue:
			intent.ProviderTarget.WorkItemRef = resultRef
			intent.Status = enum.FollowUpIntentStatusCreated
		case FollowUpDispatchKindUpdateIssue:
			intent.ProviderTarget.WorkItemRef = resultRef
			intent.Status = enum.FollowUpIntentStatusUpdated
		case FollowUpDispatchKindCreateComment, FollowUpDispatchKindUpdateComment:
			intent.ProviderTarget.CommentRef = resultRef
			intent.Status = enum.FollowUpIntentStatusCommented
		case FollowUpDispatchKindUpdatePullRequest:
			intent.ProviderTarget.PullRequestRef = resultRef
			intent.Status = enum.FollowUpIntentStatusUpdated
		case FollowUpDispatchKindCreateReviewSignal:
			intent.ProviderTarget.ReviewSignalRef = resultRef
			intent.Status = enum.FollowUpIntentStatusReviewSignaled
		default:
			return entity.FollowUpIntent{}, errs.ErrDependencyUnavailable
		}
	case ProviderOperationStatusFailed, ProviderOperationStatusRetryableFailed, ProviderOperationStatusDenied:
		intent.Status = enum.FollowUpIntentStatusFailed
	default:
		return entity.FollowUpIntent{}, errs.ErrDependencyUnavailable
	}
	return intent, nil
}

func followUpProviderResultRef(result ProviderCommandResult, kind FollowUpDispatchKind) (string, error) {
	candidates := []string{
		result.ResultRef,
		result.Target.WebURL,
	}
	if result.Target.ProviderSlug != "" && result.Target.RepositoryFullName != "" && result.Target.Number > 0 {
		refKind := followUpResultRefKind(kind)
		if kind == FollowUpDispatchKindUpdatePullRequest && result.Target.WorkItemKind != "" {
			refKind = result.Target.WorkItemKind
		}
		candidates = append(candidates, result.Target.ProviderSlug+":repo:"+result.Target.RepositoryFullName+":"+refKind+":"+strconv.FormatInt(result.Target.Number, 10))
	}
	if result.Target.ProviderSlug != "" && result.Target.ProviderObjectID != "" {
		candidates = append(candidates, result.Target.ProviderSlug+":object:"+result.Target.ProviderObjectID)
	}
	for _, candidate := range candidates {
		ref, err := normalizeFollowUpOptionalRef(candidate)
		if err == nil && ref != "" {
			return ref, nil
		}
	}
	return "", errs.ErrDependencyUnavailable
}

func followUpResultRefKind(kind FollowUpDispatchKind) string {
	switch kind {
	case FollowUpDispatchKindCreateComment, FollowUpDispatchKindUpdateComment:
		return "comment"
	case FollowUpDispatchKindUpdatePullRequest:
		return "pull_request"
	case FollowUpDispatchKindCreateReviewSignal:
		return "review_signal"
	default:
		return "issue"
	}
}

func bindOptionalUUID(target **uuid.UUID, candidate *uuid.UUID) error {
	if candidate == nil {
		return nil
	}
	if *target != nil && **target != *candidate {
		return errs.ErrConflict
	}
	*target = candidate
	return nil
}

func followUpRunStatus(status enum.AgentRunStatus) bool {
	return status == enum.AgentRunStatusCompleted || status == enum.AgentRunStatusFailed
}

func followUpAcceptanceStatus(status enum.AcceptanceStatus) bool {
	return status == enum.AcceptanceStatusPassed || status == enum.AcceptanceStatusSkipped
}

func (s *Service) normalizeFollowUpTypeForStages(ctx context.Context, flowVersionID *uuid.UUID, fromStageID *uuid.UUID, toStageID *uuid.UUID, requestedType string) (string, error) {
	workItemType, err := normalizeFollowUpWorkItemType(requestedType)
	if err != nil {
		return "", err
	}
	if fromStageID == nil && toStageID == nil {
		if workItemType == "" {
			return "", errs.ErrInvalidArgument
		}
		return workItemType, nil
	}
	if flowVersionID == nil {
		return "", errs.ErrInvalidArgument
	}
	version, err := s.repository.GetFlowVersion(ctx, *flowVersionID)
	if err != nil {
		return "", err
	}
	if !flowVersionHasStage(version, fromStageID) || !flowVersionHasStage(version, toStageID) {
		return "", errs.ErrInvalidArgument
	}
	transitionType, transitionFound := followUpTransitionType(version, fromStageID, toStageID)
	if fromStageID != nil && toStageID != nil && !transitionFound {
		return "", errs.ErrPreconditionFailed
	}
	if workItemType == "" {
		workItemType, err = normalizeFollowUpWorkItemType(transitionType)
		if err != nil {
			return "", err
		}
	}
	if workItemType == "" {
		return "", errs.ErrInvalidArgument
	}
	if transitionType != "" && workItemType != transitionType {
		return "", errs.ErrConflict
	}
	return workItemType, nil
}

func flowVersionHasStage(version entity.FlowVersion, stageID *uuid.UUID) bool {
	if stageID == nil {
		return true
	}
	for _, stage := range version.Stages {
		if stage.ID == *stageID {
			return true
		}
	}
	return false
}

func followUpTransitionType(version entity.FlowVersion, fromStageID *uuid.UUID, toStageID *uuid.UUID) (string, bool) {
	if toStageID == nil {
		return "", false
	}
	for _, transition := range version.Transitions {
		if sameOptionalUUID(transition.FromStageID, fromStageID) && transition.ToStageID == *toStageID {
			return transition.FollowUpType, true
		}
	}
	return "", false
}

func normalizeFollowUpProviderTarget(target value.ProviderTargetRef) (value.ProviderTargetRef, error) {
	var result value.ProviderTargetRef
	var err error
	if result.WorkItemRef, err = normalizeFollowUpOptionalRef(target.WorkItemRef); err != nil {
		return value.ProviderTargetRef{}, err
	}
	if result.PullRequestRef, err = normalizeFollowUpOptionalRef(target.PullRequestRef); err != nil {
		return value.ProviderTargetRef{}, err
	}
	if result.CommentRef, err = normalizeFollowUpOptionalRef(target.CommentRef); err != nil {
		return value.ProviderTargetRef{}, err
	}
	if result.ReviewSignalRef, err = normalizeFollowUpOptionalRef(target.ReviewSignalRef); err != nil {
		return value.ProviderTargetRef{}, err
	}
	return result, nil
}

func normalizeFollowUpOptionalRef(ref string) (string, error) {
	return normalizeAcceptanceTargetRef(ref)
}

func mergeProviderTarget(primary value.ProviderTargetRef, fallback value.ProviderTargetRef) value.ProviderTargetRef {
	if primary.WorkItemRef == "" {
		primary.WorkItemRef = fallback.WorkItemRef
	}
	if primary.PullRequestRef == "" {
		primary.PullRequestRef = fallback.PullRequestRef
	}
	if primary.CommentRef == "" {
		primary.CommentRef = fallback.CommentRef
	}
	if primary.ReviewSignalRef == "" {
		primary.ReviewSignalRef = fallback.ReviewSignalRef
	}
	return primary
}

func providerTargetEmpty(target value.ProviderTargetRef) bool {
	return target.WorkItemRef == "" && target.PullRequestRef == "" && target.CommentRef == "" && target.ReviewSignalRef == ""
}

func normalizeFollowUpWorkItemType(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	if len(trimmed) > followUpTypeLimit {
		return "", errs.ErrInvalidArgument
	}
	for idx, char := range trimmed {
		if idx == 0 && !asciiLetter(char) {
			return "", errs.ErrInvalidArgument
		}
		if !asciiLetter(char) && !asciiDigit(char) && char != '-' && char != '_' && char != '.' {
			return "", errs.ErrInvalidArgument
		}
	}
	return trimmed, nil
}

func normalizeFollowUpDigest(value string) (string, error) {
	return normalizeSHA256Digest(value)
}

func normalizeFollowUpText(value string, limit int, required bool) (string, error) {
	trimmed := strings.TrimSpace(value)
	if required && trimmed == "" {
		return "", errs.ErrInvalidArgument
	}
	if utf8.RuneCountInString(trimmed) > limit {
		return "", errs.ErrInvalidArgument
	}
	for _, char := range trimmed {
		if char < 32 || char == 127 {
			return "", errs.ErrInvalidArgument
		}
	}
	if unsafeFollowUpText(trimmed) {
		return "", errs.ErrInvalidArgument
	}
	return trimmed, nil
}

func normalizeFollowUpHint(value string) (string, error) {
	return normalizeSafeIdentifier(value, followUpHintLimit, unsafeFollowUpText)
}

func unsafeFollowUpText(value string) bool {
	lower := strings.ToLower(value)
	markers := []string{
		"raw_provider_payload",
		"provider_payload",
		"workspace_file",
		"workspace_files",
		"prompt_text",
		"prompt_template",
		"flow_file",
		"transcript",
		"session_dump",
		"large_report",
		"report_body",
		"raw_report",
		"stdout",
		"stderr",
		"-----begin",
		"authorization:",
		"bearer ",
		"ghp_",
		"glpat-",
		"xoxb-",
		"akia",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func asciiLetter(char rune) bool {
	return (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')
}

func asciiDigit(char rune) bool {
	return char >= '0' && char <= '9'
}

func asciiHex(char rune) bool {
	return asciiDigit(char) || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')
}

func followUpIntentFromPayload(payload []byte) (entity.FollowUpIntent, error) {
	var result followUpIntentCommandPayload
	err := json.Unmarshal(payload, &result)
	return result.FollowUpIntent, err
}

func followUpProviderIntentFromPayload(payload []byte) (entity.FollowUpIntent, error) {
	var result followUpProviderCommandPayload
	err := json.Unmarshal(payload, &result)
	return result.FollowUpIntent, err
}

func verifyFollowUpIntentReplay(expected entity.FollowUpIntent, load func(context.Context, uuid.UUID) (entity.FollowUpIntent, error)) func(context.Context, entity.CommandResult, entity.FollowUpIntent) error {
	return func(ctx context.Context, result entity.CommandResult, replay entity.FollowUpIntent) error {
		if replay.ID != result.AggregateID {
			return errs.ErrConflict
		}
		stored, err := load(ctx, result.AggregateID)
		if err != nil {
			return err
		}
		if stored.ID != replay.ID || !sameFollowUpIntentRequest(stored, expected) {
			return errs.ErrConflict
		}
		return nil
	}
}

func verifyFollowUpProviderReplay(expected followUpDispatchSnapshot, load func(context.Context, uuid.UUID) (entity.FollowUpIntent, error)) func(context.Context, entity.CommandResult, entity.FollowUpIntent) error {
	return func(ctx context.Context, result entity.CommandResult, replay entity.FollowUpIntent) error {
		if replay.ID != result.AggregateID || replay.ID.String() != expected.FollowUpIntentID {
			return errs.ErrConflict
		}
		stored, err := load(ctx, result.AggregateID)
		if err != nil {
			return err
		}
		var payload followUpProviderCommandPayload
		if err := json.Unmarshal(result.ResultPayload, &payload); err != nil {
			return err
		}
		if stored.ID != replay.ID || !sameFollowUpDispatchSnapshot(payload.Dispatch, expected) {
			return errs.ErrConflict
		}
		return nil
	}
}

func sameFollowUpIntentRequest(stored entity.FollowUpIntent, expected entity.FollowUpIntent) bool {
	return stored.SessionID == expected.SessionID &&
		sameOptionalUUID(stored.RunID, expected.RunID) &&
		sameOptionalUUID(stored.FromStageID, expected.FromStageID) &&
		sameOptionalUUID(stored.ToStageID, expected.ToStageID) &&
		sameOptionalUUID(stored.AcceptanceResultID, expected.AcceptanceResultID) &&
		stored.ProviderTarget == expected.ProviderTarget &&
		stored.ProviderWorkItemType == expected.ProviderWorkItemType &&
		stored.ProviderOperationRef == expected.ProviderOperationRef &&
		stored.InstructionBodyDigest == expected.InstructionBodyDigest &&
		stored.SafeTitle == expected.SafeTitle &&
		stored.SafeSummary == expected.SafeSummary &&
		stored.RoleHint == expected.RoleHint &&
		stored.StageHint == expected.StageHint &&
		stored.IdempotencyKey == expected.IdempotencyKey &&
		stored.Status == expected.Status
}

func sameFollowUpDispatchSnapshot(left followUpDispatchSnapshot, right followUpDispatchSnapshot) bool {
	normalizeFollowUpDispatchSnapshot(&left)
	normalizeFollowUpDispatchSnapshot(&right)
	return reflect.DeepEqual(left, right)
}

func normalizeFollowUpDispatchSnapshot(snapshot *followUpDispatchSnapshot) {
	if snapshot == nil {
		return
	}
	normalizeFollowUpPolicySnapshot(&snapshot.OperationPolicyContext)
	if snapshot.CreateIssue != nil {
		sort.Strings(snapshot.CreateIssue.Labels)
		sort.Strings(snapshot.CreateIssue.AssigneeProviderLogins)
		normalizeFollowUpPolicySnapshot(&snapshot.CreateIssue.OperationPolicyContext)
	}
	if snapshot.UpdateIssue != nil {
		normalizeProviderStringListPatch(snapshot.UpdateIssue.Labels)
		normalizeProviderStringListPatch(snapshot.UpdateIssue.AssigneeProviderLogins)
		normalizeFollowUpPolicySnapshot(&snapshot.UpdateIssue.OperationPolicyContext)
	}
	if snapshot.CreateComment != nil {
		normalizeFollowUpPolicySnapshot(&snapshot.CreateComment.OperationPolicyContext)
	}
	if snapshot.UpdateComment != nil {
		normalizeFollowUpPolicySnapshot(&snapshot.UpdateComment.OperationPolicyContext)
	}
	if snapshot.UpdatePullRequest != nil {
		normalizeProviderStringListPatch(snapshot.UpdatePullRequest.Labels)
		normalizeProviderStringListPatch(snapshot.UpdatePullRequest.AssigneeProviderLogins)
		normalizeFollowUpPolicySnapshot(&snapshot.UpdatePullRequest.OperationPolicyContext)
	}
	if snapshot.CreateReviewSignal != nil {
		normalizeFollowUpPolicySnapshot(&snapshot.CreateReviewSignal.OperationPolicyContext)
	}
}

func normalizeFollowUpPolicySnapshot(policy *ProviderOperationPolicyContext) {
	sort.Strings(policy.ChangedFields)
	sort.Strings(policy.RiskTags)
}

func normalizeProviderStringListPatch(patch *ProviderStringListPatch) {
	if patch != nil {
		sort.Strings(patch.Values)
	}
}

func sameOptionalUUID(left *uuid.UUID, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
