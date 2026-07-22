package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"text/template"
	"time"
	_ "time/tzdata" // Встраиваем IANA-базу для минимального production-образа без системного tzdata.
	"unicode/utf8"

	automationsrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/automations"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/value"
	texti18n "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/i18n"
)

const (
	defaultAutomationCallbackTTL      = 24 * time.Hour
	maxAutomationScheduleName         = 120
	maxAutomationCallbackRunes        = 1000
	maxAutomationCallbackBytes        = 4000
	maxAutomationCallbackPayloadBytes = 16 * 1024
	automationSessionScope            = "automation-run"
	automationDeliveryLease           = 30 * time.Second
	automationDeliveryAttemptTimeout  = 10 * time.Second
	automationDeliveryRetainTimeout   = 2 * time.Second
	automationDeliveryRetryDelay      = 5 * time.Second
	maxAutomationDeliveryConcurrency  = 16
)

//go:embed automation_playbook.md
var automationPlaybookFiles embed.FS

type AutomationCatalog interface {
	GetProject(ctx context.Context, id int64) (entity.Project, error)
	GetAgentRole(ctx context.Context, id int64) (entity.AgentRole, error)
	GetChat(ctx context.Context, id int64) (entity.Chat, error)
	ListChatParticipants(ctx context.Context, chatID int64) ([]entity.ChatParticipant, error)
	ListProjectRepositories(ctx context.Context, projectID int64) ([]entity.ProjectRepository, error)
}

type AutomationRunDispatcher interface {
	EnqueueAgentTurn(ctx context.Context, request AgentTurnRequest) (AgentTurnQueued, error)
}

type AutomationThreadPublisher interface {
	PostThreadMessage(ctx context.Context, input MattermostThreadPostInput) (MattermostPostRef, error)
}

type AutomationServiceConfig struct {
	Localizer               *texti18n.Localizer
	Repository              automationsrepo.Repository
	Catalog                 AutomationCatalog
	Dispatcher              AutomationRunDispatcher
	Publisher               AutomationThreadPublisher
	OwnerMattermostUsername string
	StorageReady            bool
	RuntimeReady            bool
	Now                     func() time.Time
}

type AutomationService struct {
	cfg AutomationServiceConfig
}

type CreateAutomationScheduleCommand struct {
	Actor             AuthenticatedActor
	ProjectID         int64
	TargetAgentRoleID int64
	TargetChatID      int64
	Name              string
	Preset            string
	LocalTime         string
	TimeZone          string
	PlaybookKey       string
	IdempotencyKey    string
}

type RunAutomationNowCommand struct {
	Actor          AuthenticatedActor
	ProjectID      int64
	ScheduleID     string
	IdempotencyKey string
}

type RunAutomationNowResult struct {
	Run       entity.ScheduledRun
	Duplicate bool
}

type AutomationCallbackCommand struct {
	RunPublicID             string
	AuthenticatedProjectID  int64
	AuthenticatedSessionID  int64
	AuthenticatedSessionKey string
	CallbackContractVersion string
	Outcome                 string
	AgentSummary            string
	ExactPayload            []byte
}

type AutomationCallbackResult struct {
	Run                 entity.ScheduledRun
	Duplicate           bool
	OwnerAttentionID    int64
	HumanDecisionStatus string
	DeliveryStatus      string
	NextAction          string
}

type AutomationCallbackCompleter interface {
	CompleteCallback(ctx context.Context, command AutomationCallbackCommand) (AutomationCallbackResult, error)
}

type AutomationRuntimeTerminalCommand struct {
	ProjectID        int64
	RuntimeSessionID int64
	RuntimeTurnID    int64
	RuntimeRunID     string
	RuntimeStatus    string
}

type AutomationRuntimeTerminalReconciler interface {
	ReconcileRuntimeTerminal(ctx context.Context, command AutomationRuntimeTerminalCommand) error
}

type AutomationOwnerDecisionCommand struct {
	ProjectID                  int64
	ActorUserID                string
	ActorUserName              string
	MattermostChannelID        string
	MattermostRootPostID       string
	MattermostResponsePostID   string
	MattermostResponseCreateAt int64
}

type AutomationOwnerDecisionResult struct {
	Run       entity.ScheduledRun
	Handled   bool
	Duplicate bool
}

type AutomationOwnerDecisionResolver interface {
	ResolveOwnerDecision(ctx context.Context, command AutomationOwnerDecisionCommand) (AutomationOwnerDecisionResult, error)
}

func NewAutomationService(cfg AutomationServiceConfig) *AutomationService {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &AutomationService{cfg: cfg}
}

func (svc *AutomationService) Available() bool {
	return svc != nil && svc.cfg.StorageReady && svc.cfg.Repository != nil && svc.cfg.Catalog != nil
}

func (svc *AutomationService) CreateSchedule(ctx context.Context, command CreateAutomationScheduleCommand) (entity.AutomationSchedule, bool, error) {
	if !svc.Available() {
		return entity.AutomationSchedule{}, false, errors.New("automation storage is not ready")
	}
	actor, err := svc.authorizeOwner(command.Actor)
	if err != nil {
		return entity.AutomationSchedule{}, false, err
	}
	command.Name = strings.TrimSpace(command.Name)
	command.Preset = strings.TrimSpace(command.Preset)
	command.LocalTime = strings.TrimSpace(command.LocalTime)
	command.TimeZone = strings.TrimSpace(command.TimeZone)
	command.PlaybookKey = strings.TrimSpace(command.PlaybookKey)
	command.IdempotencyKey = strings.TrimSpace(command.IdempotencyKey)
	if command.ProjectID <= 0 || command.TargetAgentRoleID <= 0 || command.TargetChatID <= 0 || command.IdempotencyKey == "" {
		return entity.AutomationSchedule{}, false, errors.New("automation schedule scope and idempotency key are required")
	}
	if command.Name == "" || utf8.RuneCountInString(command.Name) > maxAutomationScheduleName {
		return entity.AutomationSchedule{}, false, errors.New("automation schedule name is invalid")
	}
	if command.Preset != string(value.AutomationSchedulePresetDaily) {
		return entity.AutomationSchedule{}, false, errors.New("unsupported automation schedule preset")
	}
	if command.PlaybookKey != value.AutomationPlaybookProjectCheckV1 {
		return entity.AutomationSchedule{}, false, errors.New("unsupported automation playbook")
	}
	project, role, chat, err := svc.validateTarget(ctx, command.ProjectID, command.TargetAgentRoleID, command.TargetChatID)
	if err != nil {
		return entity.AutomationSchedule{}, false, err
	}
	_ = project
	_ = role
	_ = chat

	now := svc.cfg.Now().UTC()
	nextRunAt, err := nextDailyAutomationRun(now, command.LocalTime, command.TimeZone)
	if err != nil {
		return entity.AutomationSchedule{}, false, err
	}
	promptSnapshot := mustAutomationPlaybook()
	promptHash := sha256.Sum256([]byte(promptSnapshot))
	commandHash, err := automationCommandHash(struct {
		ActorUserID       string `json:"actor_user_id"`
		ProjectID         int64  `json:"project_id"`
		TargetAgentRoleID int64  `json:"target_agent_role_id"`
		TargetChatID      int64  `json:"target_chat_id"`
		Name              string `json:"name"`
		Preset            string `json:"preset"`
		LocalTime         string `json:"local_time"`
		TimeZone          string `json:"time_zone"`
		PlaybookKey       string `json:"playbook_key"`
	}{actor.UserID, command.ProjectID, command.TargetAgentRoleID, command.TargetChatID, command.Name, command.Preset, command.LocalTime, command.TimeZone, command.PlaybookKey})
	if err != nil {
		return entity.AutomationSchedule{}, false, err
	}
	return svc.cfg.Repository.CreateSchedule(ctx, automationsrepo.CreateScheduleInput{
		PublicID:                newAutomationID("schedule"),
		ProjectID:               command.ProjectID,
		TargetAgentRoleID:       command.TargetAgentRoleID,
		TargetChatID:            command.TargetChatID,
		Name:                    command.Name,
		OwnerMattermostUserID:   actor.UserID,
		OwnerMattermostUserName: actor.UserName,
		Preset:                  command.Preset,
		LocalTime:               command.LocalTime,
		TimeZone:                command.TimeZone,
		NextRunAt:               nextRunAt,
		PlaybookKey:             command.PlaybookKey,
		PromptVersion:           value.AutomationPromptVersionV1,
		PromptSnapshot:          promptSnapshot,
		PromptSHA256:            promptHash[:],
		CallbackContractVersion: value.AutomationCallbackContractV1,
		IdempotencyKey:          command.IdempotencyKey,
		CommandHash:             commandHash,
		Now:                     now,
	})
}

func (svc *AutomationService) ListSchedules(ctx context.Context, actor AuthenticatedActor, projectID int64, limit int) ([]entity.AutomationSchedule, error) {
	authorized, err := svc.authorizeOwner(actor)
	if err != nil {
		return nil, err
	}
	if projectID <= 0 {
		return nil, errors.New("project is required")
	}
	return svc.cfg.Repository.ListSchedules(ctx, projectID, authorized.UserID, limit)
}

func (svc *AutomationService) GetSchedule(ctx context.Context, actor AuthenticatedActor, publicID string, projectID int64) (entity.AutomationSchedule, error) {
	authorized, err := svc.authorizeOwner(actor)
	if err != nil {
		return entity.AutomationSchedule{}, err
	}
	return svc.cfg.Repository.GetSchedule(ctx, strings.TrimSpace(publicID), projectID, authorized.UserID)
}

func (svc *AutomationService) ListHistory(ctx context.Context, limit int) ([]entity.AutomationHistoryItem, error) {
	if !svc.Available() {
		return nil, errors.New("automation storage is not ready")
	}
	owner := normalizeMattermostUsername(svc.cfg.OwnerMattermostUsername)
	if owner == "" {
		return nil, automationsrepo.ErrForbidden
	}
	return svc.cfg.Repository.ListHistory(ctx, owner, limit)
}

func (svc *AutomationService) RunNow(ctx context.Context, command RunAutomationNowCommand) (RunAutomationNowResult, error) {
	if !svc.Available() || !svc.cfg.RuntimeReady || svc.cfg.Dispatcher == nil || svc.cfg.Publisher == nil {
		return RunAutomationNowResult{}, errors.New("automation runtime is not ready")
	}
	actor, err := svc.authorizeOwner(command.Actor)
	if err != nil {
		return RunAutomationNowResult{}, err
	}
	command.ScheduleID = strings.TrimSpace(command.ScheduleID)
	command.IdempotencyKey = strings.TrimSpace(command.IdempotencyKey)
	if command.ProjectID <= 0 || command.ScheduleID == "" || command.IdempotencyKey == "" {
		return RunAutomationNowResult{}, errors.New("automation run scope and idempotency key are required")
	}
	now := svc.cfg.Now().UTC()
	occurrencePublicID := newAutomationID("occurrence")
	runPublicID := newAutomationID("scheduled-run")
	run, created, err := svc.cfg.Repository.CreateManualRun(ctx, automationsrepo.CreateManualRunInput{
		SchedulePublicID:      command.ScheduleID,
		ProjectID:             command.ProjectID,
		OwnerMattermostUserID: actor.UserID,
		IdempotencyKey:        command.IdempotencyKey,
		OccurrencePublicID:    occurrencePublicID,
		RunPublicID:           runPublicID,
		ScheduledFor:          now,
		CallbackExpiresAt:     now.Add(defaultAutomationCallbackTTL),
		RuntimeRunID:          automationRuntimeRunID(runPublicID),
	})
	if err != nil {
		return RunAutomationNowResult{}, err
	}
	duplicate := !created
	if automationRunTerminal(run.Status) || run.Status == string(value.AutomationRunStatusRunning) || run.Status == string(value.AutomationRunStatusWaitingOwner) {
		return RunAutomationNowResult{Run: run, Duplicate: duplicate}, nil
	}

	schedule, err := svc.cfg.Repository.GetSchedule(ctx, command.ScheduleID, command.ProjectID, actor.UserID)
	if err != nil {
		return RunAutomationNowResult{Run: run, Duplicate: duplicate}, err
	}
	project, role, chat, err := svc.validateTarget(ctx, schedule.ProjectID, schedule.TargetAgentRoleID, schedule.TargetChatID)
	if err != nil {
		return svc.failRun(ctx, run, actor, "Цель расписания больше недоступна", err)
	}
	if run.ProjectID != project.ID || run.TargetAgentRoleID != role.ID || run.TargetChatID != chat.ID || run.OwnerMattermostUserID != actor.UserID {
		return svc.failRun(ctx, run, actor, "Привязка запуска не совпала с расписанием", automationsrepo.ErrForbidden)
	}

	postRef := MattermostPostRef{ChannelID: run.MattermostChannelID, PostID: run.MattermostRootPostID}
	if strings.TrimSpace(postRef.PostID) == "" {
		postRef, err = svc.cfg.Publisher.PostThreadMessage(ctx, MattermostThreadPostInput{
			ChannelID:     chat.MattermostChannelID,
			Message:       agentNoTriggerMessage(fmt.Sprintf("Запущена автоматизация «%s» (%s). Ход выполнения публикуется в этом треде.", schedule.Name, run.PublicID)),
			IdempotencyID: run.PublicID,
			Props: map[string]any{
				"mattercodex_automation_run_id": run.PublicID,
				"mattercodex_system":            true,
			},
		})
		if err != nil {
			return RunAutomationNowResult{Run: run, Duplicate: duplicate}, err
		}
		if strings.TrimSpace(postRef.PostID) == "" || strings.TrimSpace(postRef.ChannelID) != strings.TrimSpace(chat.MattermostChannelID) {
			return RunAutomationNowResult{Run: run, Duplicate: duplicate}, automationsrepo.ErrForbidden
		}
		run, err = svc.cfg.Repository.RecordRunThread(ctx, automationsrepo.RecordRunThreadInput{
			RunPublicID:           run.PublicID,
			ProjectID:             run.ProjectID,
			OwnerMattermostUserID: actor.UserID,
			MattermostChannelID:   postRef.ChannelID,
			MattermostRootPostID:  postRef.PostID,
			Now:                   svc.cfg.Now().UTC(),
		})
		if err != nil {
			return RunAutomationNowResult{Run: run, Duplicate: duplicate}, err
		}
	} else if strings.TrimSpace(postRef.ChannelID) != strings.TrimSpace(chat.MattermostChannelID) {
		return svc.failRun(ctx, run, actor, "Сохранённая привязка треда не совпала с целью расписания", automationsrepo.ErrForbidden)
	}

	prompt, err := renderAutomationPrompt(schedule, run)
	if err != nil {
		return svc.failRun(ctx, run, actor, "Не удалось подготовить playbook", err)
	}
	repositories, err := svc.cfg.Catalog.ListProjectRepositories(ctx, project.ID)
	if err != nil {
		return RunAutomationNowResult{Run: run, Duplicate: duplicate}, err
	}
	queued, err := svc.cfg.Dispatcher.EnqueueAgentTurn(ctx, AgentTurnRequest{
		Project:        project,
		Chat:           chat,
		Role:           role,
		Repositories:   repositories,
		UserID:         actor.UserID,
		UserName:       actor.UserName,
		UserMessage:    "Выполнить сохранённый playbook автоматизации",
		SourcePostID:   postRef.PostID,
		ReplyRootID:    postRef.PostID,
		SessionRootID:  postRef.PostID,
		SessionScope:   automationSessionScope,
		PreparedPrompt: prompt,
		RequestedRunID: run.RuntimeRunID,
	})
	if err != nil {
		return RunAutomationNowResult{Run: run, Duplicate: duplicate}, err
	}
	if queued.RunID != run.RuntimeRunID || queued.TurnID <= 0 || strings.TrimSpace(queued.SessionKey) == "" {
		return RunAutomationNowResult{Run: run, Duplicate: duplicate}, automationsrepo.ErrConflict
	}
	bound, err := svc.cfg.Repository.BindRun(ctx, automationsrepo.BindRunInput{
		RunPublicID:           run.PublicID,
		ProjectID:             project.ID,
		OwnerMattermostUserID: actor.UserID,
		RuntimeSessionID:      queued.SessionID,
		RuntimeSessionKey:     queued.SessionKey,
		RuntimeTurnID:         queued.TurnID,
		RuntimeRunID:          queued.RunID,
		MattermostChannelID:   postRef.ChannelID,
		MattermostRootPostID:  postRef.PostID,
		Now:                   svc.cfg.Now().UTC(),
	})
	if err != nil {
		return RunAutomationNowResult{Run: run, Duplicate: duplicate}, err
	}
	return RunAutomationNowResult{Run: bound, Duplicate: duplicate}, nil
}

func (svc *AutomationService) GetRun(ctx context.Context, actor AuthenticatedActor, publicID string, projectID int64) (entity.ScheduledRun, error) {
	authorized, err := svc.authorizeOwner(actor)
	if err != nil {
		return entity.ScheduledRun{}, err
	}
	return svc.cfg.Repository.GetRun(ctx, strings.TrimSpace(publicID), projectID, authorized.UserID)
}

func (svc *AutomationService) ListRuns(ctx context.Context, actor AuthenticatedActor, schedulePublicID string, projectID int64, limit int) ([]entity.ScheduledRun, error) {
	authorized, err := svc.authorizeOwner(actor)
	if err != nil {
		return nil, err
	}
	return svc.cfg.Repository.ListRuns(ctx, strings.TrimSpace(schedulePublicID), projectID, authorized.UserID, limit)
}

func (svc *AutomationService) CompleteCallback(ctx context.Context, command AutomationCallbackCommand) (AutomationCallbackResult, error) {
	if !svc.Available() {
		return AutomationCallbackResult{}, errors.New("automation storage is not ready")
	}
	if !validAutomationPublicID(command.RunPublicID, "scheduled-run") || command.AuthenticatedProjectID <= 0 || command.AuthenticatedSessionID <= 0 || !validAutomationSessionKey(command.AuthenticatedSessionKey) {
		return AutomationCallbackResult{}, automationsrepo.ErrForbidden
	}
	if command.CallbackContractVersion != value.AutomationCallbackContractV1 {
		return AutomationCallbackResult{}, automationsrepo.ErrForbidden
	}
	status := string(value.AutomationRunStatusSucceeded)
	switch value.AutomationRunOutcome(command.Outcome) {
	case value.AutomationRunOutcomeNoAction, value.AutomationRunOutcomeActionTaken:
	case value.AutomationRunOutcomeRequiresHuman:
		status = string(value.AutomationRunStatusWaitingOwner)
	case value.AutomationRunOutcomeFailed:
		status = string(value.AutomationRunStatusFailed)
	default:
		return AutomationCallbackResult{}, errors.New("unsupported automation outcome")
	}
	if err := validateAutomationCallbackSummary(command.AgentSummary); err != nil {
		return AutomationCallbackResult{}, err
	}
	if len(command.ExactPayload) == 0 || len(command.ExactPayload) > maxAutomationCallbackPayloadBytes {
		return AutomationCallbackResult{}, errors.New("automation callback payload is missing or exceeds the contract")
	}
	payloadHash := automationCallbackPayloadHash(command.ExactPayload)
	safeSummary := automationServerSummary(command.Outcome)
	if safeSummary == "" {
		return AutomationCallbackResult{}, errors.New("automation callback summary is required")
	}
	var ownerGate *automationsrepo.OwnerGatePlanInput
	if command.Outcome == string(value.AutomationRunOutcomeRequiresHuman) {
		gateContext, err := svc.cfg.Repository.GetOwnerGateContext(ctx, automationsrepo.OwnerGateContextInput{
			RunPublicID:             command.RunPublicID,
			AuthenticatedProjectID:  command.AuthenticatedProjectID,
			AuthenticatedSessionID:  command.AuthenticatedSessionID,
			AuthenticatedSessionKey: command.AuthenticatedSessionKey,
		})
		if err != nil {
			return AutomationCallbackResult{}, err
		}
		ownerGate, err = svc.ownerGatePlan(gateContext)
		if err != nil {
			return AutomationCallbackResult{}, err
		}
	}
	run, duplicate, err := svc.cfg.Repository.CompleteCallback(ctx, automationsrepo.CompleteCallbackInput{
		RunPublicID:             command.RunPublicID,
		AuthenticatedProjectID:  command.AuthenticatedProjectID,
		AuthenticatedSessionID:  command.AuthenticatedSessionID,
		AuthenticatedSessionKey: command.AuthenticatedSessionKey,
		CallbackContractVersion: command.CallbackContractVersion,
		Status:                  status,
		Outcome:                 command.Outcome,
		SafeSummary:             safeSummary,
		PayloadSHA256:           payloadHash,
		OwnerGate:               ownerGate,
		Now:                     svc.cfg.Now().UTC(),
	})
	result := AutomationCallbackResult{Run: run, Duplicate: duplicate}
	if err != nil || ownerGate == nil {
		return result, err
	}
	delivery, deliveryErr := svc.cfg.Repository.GetOwnerAttentionDelivery(ctx, run.ID)
	if deliveryErr != nil {
		result.HumanDecisionStatus = "pending"
		result.DeliveryStatus = "pending"
		result.NextAction = "retry_same_callback"
		return result, nil
	}
	result.OwnerAttentionID = delivery.AttentionID
	result.HumanDecisionStatus = delivery.Status
	if strings.TrimSpace(delivery.MattermostPostID) != "" {
		result.DeliveryStatus = "delivered"
		result.NextAction = "wait_for_owner_response"
		if delivery.Status == "resolved" {
			result.NextAction = "none"
		}
		return result, nil
	}
	if delivery.Status != "open" {
		result.DeliveryStatus = "not_required"
		result.NextAction = "none"
		return result, nil
	}
	if _, deliveryErr = svc.deliverOwnerAttention(ctx, delivery.ScheduledRunID); deliveryErr != nil {
		result.DeliveryStatus = "pending"
		result.NextAction = "retry_same_callback"
		return result, nil
	}
	result.DeliveryStatus = "delivered"
	result.NextAction = "wait_for_owner_response"
	return result, nil
}

func (svc *AutomationService) ReconcileOwnerAttentionDeliveries(ctx context.Context, concurrency int) (int, error) {
	if !svc.Available() || svc.cfg.Publisher == nil {
		return 0, nil
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	if concurrency > maxAutomationDeliveryConcurrency {
		concurrency = maxAutomationDeliveryConcurrency
	}
	eligibleBefore := svc.cfg.Now().UTC()
	delivered := 0
	var resultErr error
	var resultMu sync.Mutex
	var workers sync.WaitGroup
	for range concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				now := svc.cfg.Now().UTC()
				delivery, err := svc.cfg.Repository.ClaimOwnerAttentionDelivery(ctx, automationsrepo.ClaimOwnerAttentionDeliveryInput{
					ClaimToken:     newAutomationID("delivery-claim"),
					Now:            now,
					LeaseUntil:     now.Add(automationDeliveryLease),
					EligibleBefore: eligibleBefore,
				})
				if errors.Is(err, automationsrepo.ErrNotFound) {
					return
				}
				if err != nil {
					resultMu.Lock()
					resultErr = errors.Join(resultErr, err)
					resultMu.Unlock()
					return
				}
				_, err = svc.deliverClaimedOwnerAttention(ctx, delivery)
				resultMu.Lock()
				if err != nil {
					resultErr = errors.Join(resultErr, err)
				} else {
					delivered++
				}
				resultMu.Unlock()
			}
		}()
	}
	workers.Wait()
	return delivered, resultErr
}

func (svc *AutomationService) deliverOwnerAttention(ctx context.Context, scheduledRunID int64) (entity.AutomationOwnerAttentionDelivery, error) {
	now := svc.cfg.Now().UTC()
	delivery, err := svc.cfg.Repository.ClaimOwnerAttentionDelivery(ctx, automationsrepo.ClaimOwnerAttentionDeliveryInput{
		ScheduledRunID: scheduledRunID,
		ClaimToken:     newAutomationID("delivery-claim"),
		Now:            now,
		LeaseUntil:     now.Add(automationDeliveryLease),
		EligibleBefore: now,
	})
	if err != nil {
		return entity.AutomationOwnerAttentionDelivery{}, err
	}
	return svc.deliverClaimedOwnerAttention(ctx, delivery)
}

func (svc *AutomationService) deliverClaimedOwnerAttention(ctx context.Context, delivery entity.AutomationOwnerAttentionDelivery) (entity.AutomationOwnerAttentionDelivery, error) {
	if len(strings.TrimSpace(delivery.ClaimToken)) < 16 || delivery.Fence <= 0 || !delivery.LeaseExpiresAt.After(delivery.ClaimedAt) {
		return entity.AutomationOwnerAttentionDelivery{}, automationsrepo.ErrConflict
	}
	var props map[string]any
	if err := json.Unmarshal(delivery.DeliveryPropsJSON, &props); err != nil {
		return entity.AutomationOwnerAttentionDelivery{}, svc.deferOwnerAttentionDelivery(ctx, delivery, fmt.Errorf("decode automation owner attention props: %w", err))
	}
	payloadHash, err := callbackDeliveryPayloadHash(delivery.MattermostChannelID, delivery.MattermostRootPostID, delivery.DeliveryMessage, props)
	if err != nil || !bytes.Equal(payloadHash, delivery.DeliveryPayloadSHA256) {
		return entity.AutomationOwnerAttentionDelivery{}, svc.deferOwnerAttentionDelivery(ctx, delivery, automationsrepo.ErrConflict)
	}
	postInput := MattermostThreadPostInput{
		ChannelID:     delivery.MattermostChannelID,
		RootPostID:    delivery.MattermostRootPostID,
		Message:       delivery.DeliveryMessage,
		Props:         props,
		IdempotencyID: delivery.DeliveryID,
	}
	attemptCtx, cancel := context.WithTimeout(ctx, automationDeliveryAttemptTimeout)
	defer cancel()
	var ref MattermostPostRef
	if delivery.ConfirmationPending {
		reconciler, ok := svc.cfg.Publisher.(MattermostIdempotentThreadReconciler)
		if !ok {
			return entity.AutomationOwnerAttentionDelivery{}, errors.New("idempotent automation owner attention reconciliation is not configured")
		}
		found := false
		var err error
		ref, found, err = reconciler.ReconcileThreadMessage(attemptCtx, postInput)
		if err != nil {
			return entity.AutomationOwnerAttentionDelivery{}, svc.retainOwnerAttentionDelivery(ctx, delivery, err)
		}
		if !found {
			return entity.AutomationOwnerAttentionDelivery{}, svc.retainOwnerAttentionDelivery(ctx, delivery, fmt.Errorf("%w: exact post is not visible", ErrMattermostPostConfirmationAmbiguous))
		}
	} else {
		publisher, ok := svc.cfg.Publisher.(MattermostIdempotentThreadPublisher)
		if !ok {
			return entity.AutomationOwnerAttentionDelivery{}, errors.New("idempotent automation owner attention publisher is not configured")
		}
		// Confirmation-only состояние фиксируется до сетевой попытки. После этой записи
		// любой новый процесс имеет право только сверять уже возможный внешний эффект.
		if err := svc.fenceOwnerAttentionDelivery(ctx, delivery); err != nil {
			return entity.AutomationOwnerAttentionDelivery{}, err
		}
		delivery.ConfirmationPending = true
		var err error
		ref, err = publisher.ReconcileOrPostThreadMessage(attemptCtx, postInput)
		if err != nil {
			return entity.AutomationOwnerAttentionDelivery{}, svc.retainOwnerAttentionDelivery(ctx, delivery, errors.Join(ErrMattermostPostConfirmationAmbiguous, err))
		}
	}
	if strings.TrimSpace(ref.PostID) == "" || strings.TrimSpace(ref.ChannelID) != delivery.MattermostChannelID || ref.CreateAt <= 0 {
		return entity.AutomationOwnerAttentionDelivery{}, svc.retainOwnerAttentionDelivery(ctx, delivery, automationsrepo.ErrForbidden)
	}
	stored, err := svc.cfg.Repository.SetOwnerAttentionPost(ctx, automationsrepo.SetOwnerAttentionPostInput{
		AttentionID:            delivery.AttentionID,
		ScheduledRunID:         delivery.ScheduledRunID,
		DeliveryID:             delivery.DeliveryID,
		MattermostChannelID:    delivery.MattermostChannelID,
		MattermostRootPostID:   delivery.MattermostRootPostID,
		MattermostPostID:       ref.PostID,
		MattermostPostCreateAt: ref.CreateAt,
		ClaimToken:             delivery.ClaimToken,
		Fence:                  delivery.Fence,
		Now:                    svc.cfg.Now().UTC(),
	})
	if err != nil {
		return entity.AutomationOwnerAttentionDelivery{}, svc.retainOwnerAttentionDelivery(ctx, delivery, err)
	}
	return stored, nil
}

func (svc *AutomationService) fenceOwnerAttentionDelivery(ctx context.Context, delivery entity.AutomationOwnerAttentionDelivery) error {
	now := svc.cfg.Now().UTC()
	fenceCtx, cancel := context.WithTimeout(ctx, automationDeliveryRetainTimeout)
	defer cancel()
	return svc.cfg.Repository.RetainOwnerAttentionDelivery(fenceCtx, automationsrepo.RetainOwnerAttentionDeliveryInput{
		AttentionID:    delivery.AttentionID,
		ScheduledRunID: delivery.ScheduledRunID,
		DeliveryID:     delivery.DeliveryID,
		ClaimToken:     delivery.ClaimToken,
		Fence:          delivery.Fence,
		LeaseUntil:     now.Add(automationDeliveryLease),
		Now:            now,
	})
}

func (svc *AutomationService) deferOwnerAttentionDelivery(ctx context.Context, delivery entity.AutomationOwnerAttentionDelivery, cause error) error {
	now := svc.cfg.Now().UTC()
	deferErr := svc.cfg.Repository.DeferOwnerAttentionDelivery(ctx, automationsrepo.DeferOwnerAttentionDeliveryInput{
		AttentionID:    delivery.AttentionID,
		ScheduledRunID: delivery.ScheduledRunID,
		DeliveryID:     delivery.DeliveryID,
		ClaimToken:     delivery.ClaimToken,
		Fence:          delivery.Fence,
		RetryAt:        now.Add(automationDeliveryRetryDelay),
		Now:            now,
	})
	return errors.Join(cause, deferErr)
}

func (svc *AutomationService) retainOwnerAttentionDelivery(ctx context.Context, delivery entity.AutomationOwnerAttentionDelivery, cause error) error {
	now := svc.cfg.Now().UTC()
	retainCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), automationDeliveryRetainTimeout)
	defer cancel()
	retainErr := svc.cfg.Repository.RetainOwnerAttentionDelivery(retainCtx, automationsrepo.RetainOwnerAttentionDeliveryInput{
		AttentionID:    delivery.AttentionID,
		ScheduledRunID: delivery.ScheduledRunID,
		DeliveryID:     delivery.DeliveryID,
		ClaimToken:     delivery.ClaimToken,
		Fence:          delivery.Fence,
		LeaseUntil:     now.Add(automationDeliveryLease),
		Now:            now,
	})
	return errors.Join(cause, retainErr)
}

func (svc *AutomationService) ResolveOwnerDecision(ctx context.Context, command AutomationOwnerDecisionCommand) (AutomationOwnerDecisionResult, error) {
	if !svc.Available() {
		return AutomationOwnerDecisionResult{}, errors.New("automation storage is not ready")
	}
	run, duplicate, err := svc.cfg.Repository.ResolveOwnerGate(ctx, automationsrepo.ResolveOwnerGateInput{
		ProjectID:                  command.ProjectID,
		ActorUserID:                strings.TrimSpace(command.ActorUserID),
		ActorUserName:              normalizeMattermostUsername(command.ActorUserName),
		MattermostChannelID:        strings.TrimSpace(command.MattermostChannelID),
		MattermostRootPostID:       strings.TrimSpace(command.MattermostRootPostID),
		MattermostResponsePostID:   strings.TrimSpace(command.MattermostResponsePostID),
		MattermostResponseCreateAt: command.MattermostResponseCreateAt,
		Now:                        svc.cfg.Now().UTC(),
	})
	if errors.Is(err, automationsrepo.ErrNotFound) {
		return AutomationOwnerDecisionResult{}, nil
	}
	if err != nil {
		return AutomationOwnerDecisionResult{}, err
	}
	return AutomationOwnerDecisionResult{Run: run, Handled: true, Duplicate: duplicate}, nil
}

func (svc *AutomationService) ownerGatePlan(gateContext entity.AutomationOwnerGateContext) (*automationsrepo.OwnerGatePlanInput, error) {
	deliveryID := automationOwnerAttentionDeliveryID(gateContext)
	ownerMention := ""
	if owner := mentionableMattermostUsername(gateContext.RootInitiatorName); owner != "" {
		ownerMention = "@" + owner + " "
	}
	message := agentNoTriggerMessage(svc.t("automation.owner_gate.message", map[string]any{
		"OwnerMention": ownerMention,
		"RunID":        gateContext.ScheduledRunPublicID,
	}))
	props := map[string]any{
		"matter_codex_event":                 "automation_owner_attention",
		"matter_codex_callback_delivery_id":  deliveryID,
		"matter_codex_automation_run_id":     gateContext.ScheduledRunPublicID,
		"matter_codex_process_run_id":        gateContext.ProcessPublicID,
		"matter_codex_human_decision_status": "pending",
	}
	payloadHash, err := callbackDeliveryPayloadHash(gateContext.MattermostChannelID, gateContext.MattermostRootPostID, message, props)
	if err != nil {
		return nil, err
	}
	propsJSON, err := json.Marshal(props)
	if err != nil {
		return nil, fmt.Errorf("encode automation owner attention props: %w", err)
	}
	return &automationsrepo.OwnerGatePlanInput{
		ProcessRunID:            gateContext.ProcessRunID,
		PolicyRevisionID:        gateContext.PolicyRevisionID,
		RootInitiatorUserID:     gateContext.RootInitiatorUserID,
		RootInitiatorName:       gateContext.RootInitiatorName,
		AttentionSummary:        svc.t("automation.owner_gate.summary", nil),
		AttentionRecommendation: svc.t("automation.owner_gate.recommendation", nil),
		DeliveryID:              deliveryID,
		DeliveryMessage:         message,
		DeliveryPropsJSON:       propsJSON,
		DeliveryPayloadSHA256:   payloadHash,
	}, nil
}

func automationOwnerAttentionDeliveryID(gateContext entity.AutomationOwnerGateContext) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("mattercodex-automation-owner-attention-v1\x00%d\x00%d\x00%d", gateContext.ScheduledRunID, gateContext.ProcessRunID, gateContext.RuntimeTurnID)))
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(digest[:])
	return strings.ToLower(encoded[:26])
}

func (svc *AutomationService) ReconcileRuntimeTerminal(ctx context.Context, command AutomationRuntimeTerminalCommand) error {
	if !svc.Available() {
		return errors.New("automation storage is not ready")
	}
	if command.ProjectID <= 0 || command.RuntimeSessionID <= 0 || command.RuntimeTurnID <= 0 || strings.TrimSpace(command.RuntimeRunID) == "" {
		return automationsrepo.ErrForbidden
	}
	summary := automationRuntimeTerminalSummary(command.RuntimeStatus)
	if summary == "" {
		return errors.New("automation runtime status is not terminal")
	}
	_, _, err := svc.cfg.Repository.ReconcileRuntimeTerminal(ctx, automationsrepo.ReconcileRuntimeTerminalInput{
		ProjectID:        command.ProjectID,
		RuntimeSessionID: command.RuntimeSessionID,
		RuntimeTurnID:    command.RuntimeTurnID,
		RuntimeRunID:     command.RuntimeRunID,
		RuntimeStatus:    command.RuntimeStatus,
		SafeSummary:      summary,
		Now:              svc.cfg.Now().UTC(),
	})
	return err
}

func (svc *AutomationService) validateTarget(ctx context.Context, projectID int64, roleID int64, chatID int64) (entity.Project, entity.AgentRole, entity.Chat, error) {
	project, err := svc.cfg.Catalog.GetProject(ctx, projectID)
	if err != nil {
		return entity.Project{}, entity.AgentRole{}, entity.Chat{}, fmt.Errorf("get automation project: %w", err)
	}
	role, err := svc.cfg.Catalog.GetAgentRole(ctx, roleID)
	if err != nil {
		return entity.Project{}, entity.AgentRole{}, entity.Chat{}, fmt.Errorf("get automation agent role: %w", err)
	}
	chat, err := svc.cfg.Catalog.GetChat(ctx, chatID)
	if err != nil {
		return entity.Project{}, entity.AgentRole{}, entity.Chat{}, fmt.Errorf("get automation chat: %w", err)
	}
	if role.ProjectID != project.ID || chat.ProjectID != project.ID || !role.Enabled || strings.TrimSpace(chat.MattermostChannelID) == "" {
		return entity.Project{}, entity.AgentRole{}, entity.Chat{}, automationsrepo.ErrForbidden
	}
	participants, err := svc.cfg.Catalog.ListChatParticipants(ctx, chat.ID)
	if err != nil {
		return entity.Project{}, entity.AgentRole{}, entity.Chat{}, fmt.Errorf("list automation chat participants: %w", err)
	}
	for _, participant := range participants {
		if participant.RoleID == role.ID && participant.Enabled {
			return project, role, chat, nil
		}
	}
	return entity.Project{}, entity.AgentRole{}, entity.Chat{}, automationsrepo.ErrForbidden
}

func (svc *AutomationService) authorizeOwner(actor AuthenticatedActor) (AuthenticatedActor, error) {
	if svc == nil || !svc.Available() {
		return AuthenticatedActor{}, errors.New("automation storage is not ready")
	}
	actor.UserID = strings.TrimSpace(actor.UserID)
	actor.UserName = normalizeMattermostUsername(actor.UserName)
	owner := normalizeMattermostUsername(svc.cfg.OwnerMattermostUsername)
	if actor.UserID == "" || actor.UserName == "" || owner == "" || actor.UserName != owner {
		return AuthenticatedActor{}, automationsrepo.ErrForbidden
	}
	return actor, nil
}

func (svc *AutomationService) failRun(ctx context.Context, run entity.ScheduledRun, actor AuthenticatedActor, safeSummary string, cause error) (RunAutomationNowResult, error) {
	failed, failErr := svc.cfg.Repository.FailRun(ctx, automationsrepo.FailRunInput{
		RunPublicID:           run.PublicID,
		ProjectID:             run.ProjectID,
		OwnerMattermostUserID: actor.UserID,
		SafeSummary:           safeSummary,
		Now:                   svc.cfg.Now().UTC(),
	})
	if failErr != nil {
		return RunAutomationNowResult{}, errors.Join(cause, failErr)
	}
	return RunAutomationNowResult{Run: failed}, cause
}

func nextDailyAutomationRun(now time.Time, localTime string, timeZone string) (time.Time, error) {
	parsed, err := time.Parse("15:04", localTime)
	if err != nil {
		return time.Time{}, errors.New("automation local time must use HH:MM")
	}
	location, err := loadAutomationTimeZone(timeZone)
	if err != nil {
		return time.Time{}, errors.New("automation time zone is invalid")
	}
	localNow := now.In(location)
	next := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), parsed.Hour(), parsed.Minute(), 0, 0, location)
	if !next.After(localNow) {
		next = time.Date(localNow.Year(), localNow.Month(), localNow.Day()+1, parsed.Hour(), parsed.Minute(), 0, 0, location)
	}
	return next.UTC(), nil
}

func loadAutomationTimeZone(name string) (*time.Location, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "Local" || len(name) > 100 || (name != "UTC" && !strings.Contains(name, "/")) {
		return nil, errors.New("unsupported IANA time zone")
	}
	return time.LoadLocation(name)
}

func renderAutomationPrompt(schedule entity.AutomationSchedule, run entity.ScheduledRun) (string, error) {
	expectedHash := sha256.Sum256([]byte(strings.TrimSpace(schedule.PromptSnapshot)))
	if !bytes.Equal(expectedHash[:], schedule.PromptSHA256) {
		return "", errors.New("automation prompt snapshot checksum mismatch")
	}
	playbookTemplate, err := template.New("automation_playbook_snapshot").Parse(schedule.PromptSnapshot)
	if err != nil {
		return "", fmt.Errorf("parse automation playbook snapshot: %w", err)
	}
	var body bytes.Buffer
	err = playbookTemplate.Execute(&body, map[string]string{
		"RunPublicID":             run.PublicID,
		"ScheduleName":            schedule.Name,
		"ProjectName":             schedule.ProjectName,
		"PlaybookKey":             schedule.PlaybookKey,
		"CallbackContractVersion": schedule.CallbackContractVersion,
	})
	if err != nil {
		return "", fmt.Errorf("render automation playbook: %w", err)
	}
	return strings.TrimSpace(body.String()), nil
}

func validateAutomationCallbackSummary(summary string) error {
	if !utf8.ValidString(summary) || strings.TrimSpace(summary) == "" {
		return errors.New("automation callback summary is required")
	}
	if len(summary) > maxAutomationCallbackBytes || utf8.RuneCountInString(summary) > maxAutomationCallbackRunes {
		return errors.New("automation callback summary exceeds the contract")
	}
	if strings.ContainsRune(summary, '\x00') || strings.ContainsRune(summary, '\r') {
		return errors.New("automation callback summary contains a disallowed control character")
	}
	return nil
}

func automationServerSummary(outcome string) string {
	switch value.AutomationRunOutcome(outcome) {
	case value.AutomationRunOutcomeNoAction:
		return "Автоматизация завершена: действий не требуется."
	case value.AutomationRunOutcomeActionTaken:
		return "Автоматизация завершена: действие выполнено."
	case value.AutomationRunOutcomeRequiresHuman:
		return "Автоматизация ожидает решения владельца."
	case value.AutomationRunOutcomeFailed:
		return "Автоматизация завершилась ошибкой."
	default:
		return ""
	}
}

func automationRuntimeTerminalSummary(status string) string {
	switch status {
	case agentSessionTurnSucceeded:
		return "Среда выполнения завершилась без принятого результата автоматизации."
	case agentSessionTurnFailed:
		return "Среда выполнения автоматизации завершилась ошибкой."
	case agentSessionTurnBlocked:
		return "Среда выполнения автоматизации была заблокирована."
	case agentSessionTurnCanceled:
		return "Выполнение автоматизации было остановлено."
	default:
		return ""
	}
}

func automationCallbackPayloadHash(exactPayload []byte) []byte {
	hash := sha256.New()
	_, _ = hash.Write([]byte("mattercodex.automation.callback.v1\x00"))
	_, _ = hash.Write(exactPayload)
	return hash.Sum(nil)
}

func validAutomationPublicID(publicID string, prefix string) bool {
	expectedPrefix := prefix + "-"
	if len(publicID) != len(expectedPrefix)+32 || !strings.HasPrefix(publicID, expectedPrefix) {
		return false
	}
	_, err := hex.DecodeString(publicID[len(expectedPrefix):])
	return err == nil
}

func validAutomationSessionKey(sessionKey string) bool {
	return sessionKey != "" && len(sessionKey) <= 512 && strings.TrimSpace(sessionKey) == sessionKey && !strings.ContainsAny(sessionKey, "\x00\r\n")
}

func automationRunTerminal(status string) bool {
	return status == string(value.AutomationRunStatusSucceeded) || status == string(value.AutomationRunStatusFailed)
}

func automationRuntimeRunID(runPublicID string) string {
	hash := sha256.Sum256([]byte(runPublicID))
	return "automation-runtime-" + hex.EncodeToString(hash[:])
}

func automationCommandHash(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode automation command hash: %w", err)
	}
	hash := sha256.Sum256(encoded)
	return hash[:], nil
}

func newAutomationID(prefix string) string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		panic(fmt.Sprintf("generate automation id: %v", err))
	}
	return prefix + "-" + hex.EncodeToString(raw)
}

func normalizeMattermostUsername(username string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(username), "@"))
}

func mustAutomationPlaybook() string {
	body, err := automationPlaybookFiles.ReadFile("automation_playbook.md")
	if err != nil {
		panic(err)
	}
	return strings.TrimSpace(string(body))
}

func (svc *AutomationService) t(messageID string, data map[string]any) string {
	if svc != nil && svc.cfg.Localizer != nil {
		return svc.cfg.Localizer.T(messageID, data)
	}
	switch messageID {
	case "automation.owner_gate.message":
		return fmt.Sprintf("%vТребуется решение человека по автоматизации.\n\nЗапуск: `%v`\nСостояние: ожидается решение владельца.\nСледующее действие: ответьте в этом треде.", data["OwnerMention"], data["RunID"])
	case "automation.owner_gate.summary":
		return "Автоматизация ожидает решения владельца."
	case "automation.owner_gate.recommendation":
		return "Ответьте в точном треде запуска, чтобы завершить ручной шлюз."
	default:
		return messageID
	}
}
