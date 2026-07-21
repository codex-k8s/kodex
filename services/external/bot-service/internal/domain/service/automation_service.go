package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"text/template"
	"time"
	_ "time/tzdata" // Встраиваем IANA-базу для минимального production-образа без системного tzdata.
	"unicode/utf8"

	automationsrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/automations"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/value"
)

const (
	defaultAutomationCallbackTTL = 24 * time.Hour
	maxAutomationScheduleName    = 120
	maxAutomationSafeSummary     = 1000
	automationSessionScope       = "automation-run"
)

//go:embed automation_playbook.md
var automationPlaybookFiles embed.FS

var automationSensitiveSummaryPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password|authorization)\s*[:=]\s*\S+`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{16,}\b`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}\b`),
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
}

type AutomationCatalog interface {
	GetProject(ctx context.Context, id int64) (entity.Project, error)
	GetAgentRole(ctx context.Context, id int64) (entity.AgentRole, error)
	GetChat(ctx context.Context, id int64) (entity.Chat, error)
	ListChatParticipants(ctx context.Context, chatID int64) ([]entity.ChatParticipant, error)
	ListProjectRepositories(ctx context.Context, projectID int64) ([]entity.ProjectRepository, error)
	GetAgentSession(ctx context.Context, sessionKey string) (entity.AgentSession, error)
}

type AutomationRunDispatcher interface {
	EnqueueAgentTurn(ctx context.Context, request AgentTurnRequest) (AgentTurnQueued, error)
}

type AutomationThreadPublisher interface {
	PostThreadMessage(ctx context.Context, input MattermostThreadPostInput) (MattermostPostRef, error)
}

type AutomationServiceConfig struct {
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
	ProjectID               int64
	RuntimeSessionID        int64
	RuntimeTurnID           int64
	RuntimeRunID            string
	CallbackContractVersion string
	Outcome                 string
	SafeSummary             string
}

type AutomationCallbackResult struct {
	Run       entity.ScheduledRun
	Duplicate bool
}

type AutomationCallbackCompleter interface {
	CompleteCallback(ctx context.Context, command AutomationCallbackCommand) (AutomationCallbackResult, error)
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
	run, created, err := svc.cfg.Repository.CreateManualRun(ctx, automationsrepo.CreateManualRunInput{
		SchedulePublicID:      command.ScheduleID,
		ProjectID:             command.ProjectID,
		OwnerMattermostUserID: actor.UserID,
		IdempotencyKey:        command.IdempotencyKey,
		OccurrencePublicID:    newAutomationID("occurrence"),
		RunPublicID:           newAutomationID("scheduled-run"),
		ScheduledFor:          now,
		CallbackExpiresAt:     now.Add(defaultAutomationCallbackTTL),
	})
	if err != nil {
		return RunAutomationNowResult{}, err
	}
	if !created {
		return RunAutomationNowResult{Run: run, Duplicate: true}, nil
	}

	schedule, err := svc.cfg.Repository.GetSchedule(ctx, command.ScheduleID, command.ProjectID, actor.UserID)
	if err != nil {
		return svc.failRun(ctx, run, actor, "Не удалось повторно проверить расписание перед запуском", err)
	}
	project, role, chat, err := svc.validateTarget(ctx, schedule.ProjectID, schedule.TargetAgentRoleID, schedule.TargetChatID)
	if err != nil {
		return svc.failRun(ctx, run, actor, "Цель расписания больше недоступна", err)
	}
	if run.ProjectID != project.ID || run.TargetAgentRoleID != role.ID || run.TargetChatID != chat.ID || run.OwnerMattermostUserID != actor.UserID {
		return svc.failRun(ctx, run, actor, "Привязка запуска не совпала с расписанием", automationsrepo.ErrForbidden)
	}

	postRef, err := svc.cfg.Publisher.PostThreadMessage(ctx, MattermostThreadPostInput{
		ChannelID:     chat.MattermostChannelID,
		Message:       agentNoTriggerMessage(fmt.Sprintf("Запущена автоматизация «%s» (%s). Ход выполнения публикуется в этом треде.", schedule.Name, run.PublicID)),
		IdempotencyID: run.PublicID,
		Props: map[string]any{
			"mattercodex_automation_run_id": run.PublicID,
			"mattercodex_system":            true,
		},
	})
	if err != nil {
		return svc.failRun(ctx, run, actor, "Не удалось создать тред выполнения", err)
	}
	if strings.TrimSpace(postRef.PostID) == "" || strings.TrimSpace(postRef.ChannelID) != strings.TrimSpace(chat.MattermostChannelID) {
		return svc.failRun(ctx, run, actor, "Mattermost вернул несовпадающую привязку треда", automationsrepo.ErrForbidden)
	}

	prompt, err := renderAutomationPrompt(schedule, run)
	if err != nil {
		return svc.failRun(ctx, run, actor, "Не удалось подготовить playbook", err)
	}
	repositories, err := svc.cfg.Catalog.ListProjectRepositories(ctx, project.ID)
	if err != nil {
		return svc.failRun(ctx, run, actor, "Не удалось проверить репозитории проекта", err)
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
	})
	if err != nil {
		return svc.failRun(ctx, run, actor, "Не удалось поставить playbook в очередь", err)
	}
	session, err := svc.cfg.Catalog.GetAgentSession(ctx, queued.SessionKey)
	if err != nil {
		return svc.failRun(ctx, run, actor, "Не удалось проверить созданную сессию", err)
	}
	if session.ProjectID != project.ID || session.ChatID != chat.ID || session.RoleID != role.ID || session.ActiveTurnID != queued.TurnID || session.ActiveRunID != queued.RunID {
		return svc.failRun(ctx, run, actor, "Созданная сессия не совпала с запуском", automationsrepo.ErrForbidden)
	}
	bound, err := svc.cfg.Repository.BindRun(ctx, automationsrepo.BindRunInput{
		RunPublicID:           run.PublicID,
		ProjectID:             project.ID,
		OwnerMattermostUserID: actor.UserID,
		RuntimeSessionID:      session.ID,
		RuntimeSessionKey:     session.SessionKey,
		RuntimeTurnID:         queued.TurnID,
		RuntimeRunID:          queued.RunID,
		MattermostChannelID:   postRef.ChannelID,
		MattermostRootPostID:  postRef.PostID,
		Now:                   svc.cfg.Now().UTC(),
	})
	if err != nil {
		return svc.failRun(ctx, run, actor, "Не удалось сохранить привязку выполнения", err)
	}
	return RunAutomationNowResult{Run: bound}, nil
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
	command.RunPublicID = strings.TrimSpace(command.RunPublicID)
	command.RuntimeRunID = strings.TrimSpace(command.RuntimeRunID)
	command.CallbackContractVersion = strings.TrimSpace(command.CallbackContractVersion)
	command.Outcome = strings.TrimSpace(command.Outcome)
	command.SafeSummary = sanitizeAutomationSummary(command.SafeSummary)
	if command.RunPublicID == "" || command.ProjectID <= 0 || command.RuntimeSessionID <= 0 || command.RuntimeTurnID <= 0 || command.RuntimeRunID == "" {
		return AutomationCallbackResult{}, automationsrepo.ErrForbidden
	}
	if command.CallbackContractVersion != value.AutomationCallbackContractV1 {
		return AutomationCallbackResult{}, automationsrepo.ErrForbidden
	}
	status := string(value.AutomationRunStatusSucceeded)
	switch value.AutomationRunOutcome(command.Outcome) {
	case value.AutomationRunOutcomeNoAction, value.AutomationRunOutcomeActionTaken, value.AutomationRunOutcomeRequiresHuman:
	case value.AutomationRunOutcomeFailed:
		status = string(value.AutomationRunStatusFailed)
	default:
		return AutomationCallbackResult{}, errors.New("unsupported automation outcome")
	}
	if command.SafeSummary == "" {
		return AutomationCallbackResult{}, errors.New("automation callback summary is required")
	}
	if !automationSummaryIsSafe(command.SafeSummary) {
		return AutomationCallbackResult{}, errors.New("automation callback summary contains disallowed sensitive content")
	}
	payloadHash, err := automationCommandHash(struct {
		RunPublicID             string `json:"schedule_run_id"`
		ProjectID               int64  `json:"project_id"`
		RuntimeSessionID        int64  `json:"runtime_session_id"`
		RuntimeTurnID           int64  `json:"runtime_turn_id"`
		RuntimeRunID            string `json:"runtime_run_id"`
		CallbackContractVersion string `json:"callback_contract"`
		Outcome                 string `json:"outcome"`
		SafeSummary             string `json:"summary"`
	}{command.RunPublicID, command.ProjectID, command.RuntimeSessionID, command.RuntimeTurnID, command.RuntimeRunID, command.CallbackContractVersion, command.Outcome, command.SafeSummary})
	if err != nil {
		return AutomationCallbackResult{}, err
	}
	run, duplicate, err := svc.cfg.Repository.CompleteCallback(ctx, automationsrepo.CompleteCallbackInput{
		RunPublicID:             command.RunPublicID,
		ProjectID:               command.ProjectID,
		RuntimeSessionID:        command.RuntimeSessionID,
		RuntimeTurnID:           command.RuntimeTurnID,
		RuntimeRunID:            command.RuntimeRunID,
		CallbackContractVersion: command.CallbackContractVersion,
		Status:                  status,
		Outcome:                 command.Outcome,
		SafeSummary:             command.SafeSummary,
		PayloadSHA256:           payloadHash,
		Now:                     svc.cfg.Now().UTC(),
	})
	return AutomationCallbackResult{Run: run, Duplicate: duplicate}, err
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
		SafeSummary:           sanitizeAutomationSummary(safeSummary),
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

func sanitizeAutomationSummary(summary string) string {
	summary = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == '\x00' || r == '\r' {
			return -1
		}
		return r
	}, summary))
	runes := []rune(summary)
	if len(runes) > maxAutomationSafeSummary {
		summary = string(runes[:maxAutomationSafeSummary])
	}
	return summary
}

func automationSummaryIsSafe(summary string) bool {
	lower := strings.ToLower(summary)
	if strings.Contains(lower, "ты выполняешь минимальный playbook") || strings.Contains(lower, "callback_contract:") {
		return false
	}
	for _, pattern := range automationSensitiveSummaryPatterns {
		if pattern.MatchString(summary) {
			return false
		}
	}
	return true
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
