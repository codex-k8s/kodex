package service

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

const (
	activityIDLimit        = 256
	activityNameLimit      = 128
	activitySummaryLimit   = 1000
	activityErrorLimit     = 2000
	activityJSONLimit      = 8192
	activityStringValueMax = 512
)

type activityCommandPayload struct {
	Activity entity.AgentActivity `json:"activity"`
}

type activityDetailsObject map[string]json.RawMessage

var (
	validActivityKinds = map[enum.AgentActivityKind]struct{}{
		enum.AgentActivityKindLifecycle:      {},
		enum.AgentActivityKindToolUse:        {},
		enum.AgentActivityKindToolResult:     {},
		enum.AgentActivityKindPermission:     {},
		enum.AgentActivityKindProviderSignal: {},
		enum.AgentActivityKindRuntimeSignal:  {},
		enum.AgentActivityKindCheckpoint:     {},
		enum.AgentActivityKindOther:          {},
	}
	validActivityStatuses = map[enum.AgentActivityStatus]struct{}{
		enum.AgentActivityStatusPlanned:   {},
		enum.AgentActivityStatusStarted:   {},
		enum.AgentActivityStatusSucceeded: {},
		enum.AgentActivityStatusFailed:    {},
		enum.AgentActivityStatusDenied:    {},
		enum.AgentActivityStatusWaiting:   {},
		enum.AgentActivityStatusCancelled: {},
		enum.AgentActivityStatusSkipped:   {},
	}
)

func (s *Service) RecordAgentActivity(ctx context.Context, input RecordAgentActivityInput) (entity.AgentActivity, error) {
	if err := s.requireRepository(); err != nil {
		return entity.AgentActivity{}, err
	}
	if err := validateID(input.SessionID); err != nil {
		return entity.AgentActivity{}, err
	}
	idempotencyKey, err := activityIdempotencyKey(input.Meta)
	if err != nil {
		return entity.AgentActivity{}, err
	}
	session, err := s.repository.GetAgentSession(ctx, input.SessionID)
	if err != nil {
		return entity.AgentActivity{}, err
	}
	activity, err := s.normalizeAgentActivity(ctx, session, input, idempotencyKey)
	if err != nil {
		return entity.AgentActivity{}, err
	}
	if replay, ok, err := findReplay(ctx, s, input.Meta, operationRecordAgentActivity, enum.CommandAggregateTypeActivity, activityFromPayload, verifyActivityReplay(activity, s.repository.GetAgentActivity)); ok || err != nil {
		return replay, err
	}
	now := s.clock.Now()
	activity.ID = s.idGenerator.New()
	activity.Version = 1
	activity.CreatedAt = now
	activity.UpdatedAt = now
	payload, err := marshalCommandPayload(activityCommandPayload{Activity: activity})
	if err != nil {
		return entity.AgentActivity{}, err
	}
	result, err := commandResult(input.Meta, operationRecordAgentActivity, enum.CommandAggregateTypeActivity, activity.ID, payload, now)
	if err != nil {
		return entity.AgentActivity{}, err
	}
	return activity, s.repository.RecordAgentActivityWithResult(ctx, activity, result)
}

func (s *Service) ListAgentActivities(ctx context.Context, filter query.AgentActivityFilter) ([]entity.AgentActivity, value.PageResult, error) {
	if err := s.requireRepository(); err != nil {
		return nil, value.PageResult{}, err
	}
	if filter.SessionID == uuid.Nil && filter.RunID == uuid.Nil {
		return nil, value.PageResult{}, errs.ErrInvalidArgument
	}
	if filter.SessionID != uuid.Nil {
		if _, err := s.repository.GetAgentSession(ctx, filter.SessionID); err != nil {
			return nil, value.PageResult{}, err
		}
	}
	if filter.RunID != uuid.Nil {
		run, err := s.repository.GetAgentRun(ctx, filter.RunID)
		if err != nil {
			return nil, value.PageResult{}, err
		}
		if filter.SessionID != uuid.Nil && run.SessionID != filter.SessionID {
			return nil, value.PageResult{}, errs.ErrConflict
		}
	}
	if filter.ActivityKind != nil {
		if _, err := normalizeActivityKind(*filter.ActivityKind); err != nil {
			return nil, value.PageResult{}, err
		}
	}
	if filter.Status != nil {
		if _, err := normalizeActivityStatus(*filter.Status); err != nil {
			return nil, value.PageResult{}, err
		}
	}
	return s.repository.ListAgentActivities(ctx, filter)
}

func (s *Service) normalizeAgentActivity(ctx context.Context, session entity.AgentSession, input RecordAgentActivityInput, idempotencyKey string) (entity.AgentActivity, error) {
	runID := input.RunID
	if runID != nil {
		run, err := s.repository.GetAgentRun(ctx, *runID)
		if err != nil {
			return entity.AgentActivity{}, err
		}
		if run.SessionID != session.ID {
			return entity.AgentActivity{}, errs.ErrConflict
		}
	}
	kind, err := normalizeActivityKind(input.ActivityKind)
	if err != nil {
		return entity.AgentActivity{}, err
	}
	status, err := normalizeActivityStatus(input.Status)
	if err != nil {
		return entity.AgentActivity{}, err
	}
	turnID, err := normalizeActivityToken(input.TurnID)
	if err != nil {
		return entity.AgentActivity{}, err
	}
	toolUseID, err := normalizeActivityToken(input.ToolUseID)
	if err != nil {
		return entity.AgentActivity{}, err
	}
	toolName, err := normalizeActivityName(input.ToolName)
	if err != nil {
		return entity.AgentActivity{}, err
	}
	toolCategory, err := normalizeActivityName(input.ToolCategory)
	if err != nil {
		return entity.AgentActivity{}, err
	}
	if toolActivityKind(kind) && toolName == "" && toolCategory == "" {
		return entity.AgentActivity{}, errs.ErrInvalidArgument
	}
	startedAt, err := normalizeActivityStartedAt(input.StartedAt)
	if err != nil {
		return entity.AgentActivity{}, err
	}
	finishedAt, err := normalizeActivityFinishedAt(startedAt, input.FinishedAt)
	if err != nil {
		return entity.AgentActivity{}, err
	}
	duration, err := normalizeActivityDuration(startedAt, finishedAt, input.DurationMs)
	if err != nil {
		return entity.AgentActivity{}, err
	}
	summary, err := normalizeActivityText(input.SafeSummary, activitySummaryLimit, false)
	if err != nil {
		return entity.AgentActivity{}, err
	}
	digest, err := normalizeSHA256Digest(input.PayloadDigest)
	if err != nil {
		return entity.AgentActivity{}, err
	}
	boundedError, err := normalizeActivityText(input.BoundedError, activityErrorLimit, false)
	if err != nil {
		return entity.AgentActivity{}, err
	}
	refs, err := normalizeActivityJSON(input.SafeRefsJSON, true)
	if err != nil {
		return entity.AgentActivity{}, err
	}
	details, err := normalizeActivityJSON(input.SafeDetailsJSON, false)
	if err != nil {
		return entity.AgentActivity{}, err
	}
	correlationID, err := normalizeActivityToken(input.CorrelationID)
	if err != nil {
		return entity.AgentActivity{}, err
	}
	return entity.AgentActivity{
		SessionID:       session.ID,
		RunID:           runID,
		TurnID:          turnID,
		ToolUseID:       toolUseID,
		ActivityKind:    kind,
		ToolName:        toolName,
		ToolCategory:    toolCategory,
		Status:          status,
		StartedAt:       startedAt,
		FinishedAt:      finishedAt,
		DurationMs:      duration,
		SafeSummary:     summary,
		PayloadDigest:   digest,
		BoundedError:    boundedError,
		SafeRefsJSON:    refs,
		SafeDetailsJSON: details,
		CorrelationID:   correlationID,
		IdempotencyKey:  idempotencyKey,
	}, nil
}

func activityIdempotencyKey(meta value.CommandMeta) (string, error) {
	return safeCommandResultKey(meta, operationRecordAgentActivity, unsafeActivityText)
}

func normalizeActivityKind(kind enum.AgentActivityKind) (enum.AgentActivityKind, error) {
	if _, ok := validActivityKinds[kind]; !ok {
		return "", errs.ErrInvalidArgument
	}
	return kind, nil
}

func normalizeActivityStatus(status enum.AgentActivityStatus) (enum.AgentActivityStatus, error) {
	if _, ok := validActivityStatuses[status]; !ok {
		return "", errs.ErrInvalidArgument
	}
	return status, nil
}

func toolActivityKind(kind enum.AgentActivityKind) bool {
	return kind == enum.AgentActivityKindToolUse || kind == enum.AgentActivityKindToolResult || kind == enum.AgentActivityKindPermission
}

func normalizeActivityStartedAt(value *time.Time) (time.Time, error) {
	if value == nil || value.IsZero() {
		return time.Time{}, errs.ErrInvalidArgument
	}
	return value.UTC(), nil
}

func normalizeActivityFinishedAt(startedAt time.Time, value *time.Time) (*time.Time, error) {
	if value == nil {
		return nil, nil
	}
	finishedAt := value.UTC()
	if finishedAt.Before(startedAt) {
		return nil, errs.ErrInvalidArgument
	}
	return &finishedAt, nil
}

func normalizeActivityDuration(startedAt time.Time, finishedAt *time.Time, value *int64) (*int64, error) {
	if value != nil {
		if *value < 0 {
			return nil, errs.ErrInvalidArgument
		}
		duration := *value
		return &duration, nil
	}
	if finishedAt == nil {
		return nil, nil
	}
	duration := finishedAt.Sub(startedAt).Milliseconds()
	return &duration, nil
}

func normalizeActivityToken(value string) (string, error) {
	return normalizeActivityIdentifier(value, activityIDLimit)
}

func normalizeActivityName(value string) (string, error) {
	return normalizeActivityIdentifier(value, activityNameLimit)
}

func normalizeActivityIdentifier(value string, limit int) (string, error) {
	return normalizeSafeIdentifier(value, limit, unsafeActivityText)
}

func normalizeSafeIdentifier(value string, limit int, unsafeText func(string) bool) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	if len(trimmed) > limit || unsafeText(trimmed) {
		return "", errs.ErrInvalidArgument
	}
	for _, char := range trimmed {
		if !safeAcceptanceRefChar(char) {
			return "", errs.ErrInvalidArgument
		}
	}
	return trimmed, nil
}

func normalizeActivityText(value string, limit int, required bool) (string, error) {
	return normalizeBoundedSafeText(value, limit, required, unsafeActivityText)
}

func normalizeSHA256Digest(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	const prefix = "sha256:"
	if len(trimmed) != len(prefix)+64 || !strings.HasPrefix(trimmed, prefix) {
		return "", errs.ErrInvalidArgument
	}
	for _, char := range trimmed[len(prefix):] {
		if !asciiHex(char) {
			return "", errs.ErrInvalidArgument
		}
	}
	return trimmed, nil
}

func normalizeActivityJSON(payload []byte, refsOnly bool) ([]byte, error) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return []byte("{}"), nil
	}
	if len(trimmed) > activityJSONLimit {
		return nil, errs.ErrInvalidArgument
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, trimmed); err != nil {
		return nil, errs.ErrInvalidArgument
	}
	if compact.Len() > activityJSONLimit {
		return nil, errs.ErrInvalidArgument
	}
	var object activityDetailsObject
	if err := json.Unmarshal(compact.Bytes(), &object); err != nil {
		return nil, errs.ErrInvalidArgument
	}
	if err := validateActivityJSONObject(object, refsOnly); err != nil {
		return nil, err
	}
	return compact.Bytes(), nil
}

func validateActivityJSONObject(object activityDetailsObject, refsOnly bool) error {
	if object == nil {
		return errs.ErrInvalidArgument
	}
	for key, raw := range object {
		if unsafeActivityDetailKey(key) {
			return errs.ErrInvalidArgument
		}
		if err := validateActivityJSONValue(raw, refsOnly); err != nil {
			return err
		}
	}
	return nil
}

func validateActivityJSONValue(raw json.RawMessage, refsOnly bool) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}
	switch trimmed[0] {
	case '{':
		var object activityDetailsObject
		if err := json.Unmarshal(trimmed, &object); err != nil {
			return errs.ErrInvalidArgument
		}
		return validateActivityJSONObject(object, refsOnly)
	case '[':
		var items []json.RawMessage
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return errs.ErrInvalidArgument
		}
		for _, item := range items {
			if err := validateActivityJSONValue(item, refsOnly); err != nil {
				return err
			}
		}
	case '"':
		var value string
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return errs.ErrInvalidArgument
		}
		if refsOnly {
			_, err := normalizeAcceptanceTargetRef(value)
			if err != nil || strings.TrimSpace(value) == "" {
				return errs.ErrInvalidArgument
			}
			return nil
		}
		if _, err := normalizeActivityText(value, activityStringValueMax, false); err != nil {
			return err
		}
	default:
		if refsOnly {
			return errs.ErrInvalidArgument
		}
	}
	return nil
}

func unsafeActivityDetailKey(key string) bool {
	return unsafeActivityText(key) || unsafeAcceptanceDetailKey(key)
}

func unsafeActivityText(value string) bool {
	lower := strings.ToLower(value)
	markers := []string{
		"raw_tool_input",
		"raw_tool_response",
		"tool_input",
		"tool_response",
		"raw_provider_payload",
		"provider_payload",
		"workspace_path",
		"workspace_file",
		"workspace_files",
		"/workspace",
		"/home/",
		"kubeconfig",
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
		"logs",
		"secret",
		"token",
		"authorization",
		"-----begin",
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

func activityFromPayload(payload []byte) (entity.AgentActivity, error) {
	var result activityCommandPayload
	err := json.Unmarshal(payload, &result)
	return result.Activity, err
}

func verifyActivityReplay(expected entity.AgentActivity, load func(context.Context, uuid.UUID) (entity.AgentActivity, error)) func(context.Context, entity.CommandResult, entity.AgentActivity) error {
	return verifyReplay(uuid.Nil, load, agentActivityID, func(stored entity.AgentActivity) error {
		if !sameActivityRequest(stored, expected) {
			return errs.ErrConflict
		}
		return nil
	})
}

func agentActivityID(activity entity.AgentActivity) uuid.UUID { return activity.ID }

func sameActivityRequest(stored entity.AgentActivity, expected entity.AgentActivity) bool {
	return stored.SessionID == expected.SessionID &&
		sameOptionalUUID(stored.RunID, expected.RunID) &&
		stored.TurnID == expected.TurnID &&
		stored.ToolUseID == expected.ToolUseID &&
		stored.ActivityKind == expected.ActivityKind &&
		stored.ToolName == expected.ToolName &&
		stored.ToolCategory == expected.ToolCategory &&
		stored.Status == expected.Status &&
		stored.StartedAt.Equal(expected.StartedAt) &&
		sameOptionalTime(stored.FinishedAt, expected.FinishedAt) &&
		sameOptionalInt64(stored.DurationMs, expected.DurationMs) &&
		stored.SafeSummary == expected.SafeSummary &&
		stored.PayloadDigest == expected.PayloadDigest &&
		stored.BoundedError == expected.BoundedError &&
		bytes.Equal(stored.SafeRefsJSON, expected.SafeRefsJSON) &&
		bytes.Equal(stored.SafeDetailsJSON, expected.SafeDetailsJSON) &&
		stored.CorrelationID == expected.CorrelationID &&
		stored.IdempotencyKey == expected.IdempotencyKey
}

func sameOptionalTime(left *time.Time, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func sameOptionalInt64(left *int64, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
