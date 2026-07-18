package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
)

type AgentSessionMemorySearch struct {
	SessionKey string                `json:"session_key"`
	Records    []entity.MemoryRecord `json:"records"`
}

type AgentSessionMemoryRememberCommand struct {
	Scope      string
	Title      string
	Content    string
	Importance string
}

type AgentSessionWorkContextCommand struct {
	Summary      string
	Domains      []string
	ResourceKeys []string
	Links        []string
}

type AgentSessionActiveWork struct {
	SessionKey string             `json:"session_key"`
	Claims     []entity.WorkClaim `json:"claims"`
}

type AgentSessionOwnerAttentionCommand struct {
	Severity       string
	Summary        string
	Options        []string
	Recommendation string
	EvidenceLinks  []string
	PauseScope     string
	IdempotencyKey string
}

var likelySecretAssignment = regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?key|authorization|bearer|token|password|passwd|secret|private[_-]?key|kubeconfig)\s*[:=]\s*\S+`)

func (svc *AgentSessionService) requireCoordinationPermission(ctx context.Context, session entity.AgentSession, capability string, action string, targetRoleID int64) error {
	store, ok := svc.cfg.Store.(adminrepo.CoordinationRepository)
	if !ok {
		return nil
	}
	allowed, err := store.IsRoleCapabilityAllowed(ctx, session.ActiveTurnID, session.ProjectID, session.RoleID, capability)
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("coordination policy denies capability %q for role %d", capability, session.RoleID)
	}
	if action == "" || targetRoleID == 0 {
		return nil
	}
	allowed, err = store.IsRoleRelationshipAllowed(ctx, session.ActiveTurnID, session.ProjectID, session.RoleID, action, targetRoleID)
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("coordination policy denies action %q from role %d to role %d", action, session.RoleID, targetRoleID)
	}
	return nil
}

func (svc *AgentSessionService) SearchMemory(ctx context.Context, sessionKey string, token string, query string, limit int) (AgentSessionMemorySearch, error) {
	session, err := svc.authorize(ctx, sessionKey, token)
	if err != nil {
		return AgentSessionMemorySearch{}, err
	}
	store, ok := svc.cfg.Store.(adminrepo.CoordinationRepository)
	if !ok {
		return AgentSessionMemorySearch{}, fmt.Errorf("project memory is not configured")
	}
	query = strings.TrimSpace(query)
	if len([]rune(query)) > 500 {
		return AgentSessionMemorySearch{}, fmt.Errorf("memory query must be no longer than 500 characters")
	}
	var records []entity.MemoryRecord
	err = svc.withCurrentSessionRuntimeGuard(ctx, session, "agent_session.memory_search.side_effect", func(current entity.AgentSession) error {
		if permissionErr := svc.requireCoordinationPermission(ctx, current, entity.CoordinationCapabilityReadProjectMemory, "", 0); permissionErr != nil {
			return permissionErr
		}
		var searchErr error
		records, searchErr = store.SearchMemory(ctx, adminrepo.SearchMemoryInput{ProjectID: current.ProjectID, RoleID: current.RoleID, Query: query, Limit: limit})
		return searchErr
	})
	if err != nil {
		return AgentSessionMemorySearch{}, err
	}
	return AgentSessionMemorySearch{SessionKey: session.SessionKey, Records: records}, nil
}

func (svc *AgentSessionService) RememberMemory(ctx context.Context, sessionKey string, token string, command AgentSessionMemoryRememberCommand) (entity.MemoryRecord, error) {
	session, err := svc.authorize(ctx, sessionKey, token)
	if err != nil {
		return entity.MemoryRecord{}, err
	}
	_, ok := svc.cfg.Store.(adminrepo.CoordinationRepository)
	if !ok {
		return entity.MemoryRecord{}, fmt.Errorf("project memory is not configured")
	}
	command.Scope = strings.ToLower(strings.TrimSpace(command.Scope))
	command.Title = strings.TrimSpace(command.Title)
	command.Content = strings.TrimSpace(command.Content)
	command.Importance = strings.ToLower(strings.TrimSpace(command.Importance))
	if command.Scope != "project" && command.Scope != "role" {
		return entity.MemoryRecord{}, fmt.Errorf("memory scope must be project or role")
	}
	if command.Title == "" || command.Content == "" {
		return entity.MemoryRecord{}, fmt.Errorf("memory title and content are required")
	}
	if len([]rune(command.Title)) > 300 || len([]rune(command.Content)) > 12000 {
		return entity.MemoryRecord{}, fmt.Errorf("memory title or content is too long")
	}
	if likelySecretAssignment.MatchString(command.Title) || likelySecretAssignment.MatchString(command.Content) {
		return entity.MemoryRecord{}, fmt.Errorf("memory contains a likely secret assignment")
	}
	if command.Importance == "" {
		command.Importance = "normal"
	}
	if !stringInSet(command.Importance, "low", "normal", "high", "critical") {
		return entity.MemoryRecord{}, fmt.Errorf("memory importance must be low, normal, high, or critical")
	}
	capability := entity.CoordinationCapabilityWriteRoleMemory
	if command.Scope == "project" {
		capability = entity.CoordinationCapabilityWriteProjectMemory
	}
	var record entity.MemoryRecord
	err = svc.withCurrentSessionPersistenceGuard(ctx, session, "agent_session.memory_remember.side_effect", func(current entity.AgentSession, guardedStore adminrepo.Repository) error {
		if permissionErr := svc.requireCoordinationPermission(ctx, current, capability, "", 0); permissionErr != nil {
			return permissionErr
		}
		coordinationStore, ok := guardedStore.(adminrepo.CoordinationRepository)
		if !ok {
			return fmt.Errorf("project memory is not configured")
		}
		var rememberErr error
		record, rememberErr = coordinationStore.RememberMemory(ctx, adminrepo.RememberMemoryInput{
			ProjectID: current.ProjectID, Scope: command.Scope, RoleID: current.RoleID, CreatedByRoleID: current.RoleID,
			SourceTurnID: current.ActiveTurnID, SourcePostID: current.MattermostRootPostID,
			Importance: command.Importance, Title: command.Title, Content: command.Content,
		})
		return rememberErr
	})
	return record, err
}

func (svc *AgentSessionService) ListActiveWork(ctx context.Context, sessionKey string, token string, limit int) (AgentSessionActiveWork, error) {
	session, err := svc.authorize(ctx, sessionKey, token)
	if err != nil {
		return AgentSessionActiveWork{}, err
	}
	store, ok := svc.cfg.Store.(adminrepo.CoordinationRepository)
	if !ok {
		return AgentSessionActiveWork{}, fmt.Errorf("active work registry is not configured")
	}
	var claims []entity.WorkClaim
	err = svc.withCurrentSessionRuntimeGuard(ctx, session, "agent_session.active_work_list.side_effect", func(current entity.AgentSession) error {
		if permissionErr := svc.requireCoordinationPermission(ctx, current, entity.CoordinationCapabilityReadProjectWork, "", 0); permissionErr != nil {
			return permissionErr
		}
		var listErr error
		claims, listErr = store.ListActiveWork(ctx, 0, current.ProjectID, limit)
		return listErr
	})
	if err != nil {
		return AgentSessionActiveWork{}, err
	}
	return AgentSessionActiveWork{SessionKey: session.SessionKey, Claims: claims}, nil
}

func (svc *AgentSessionService) UpdateWorkContext(ctx context.Context, sessionKey string, token string, command AgentSessionWorkContextCommand) (entity.WorkClaim, error) {
	session, err := svc.authorize(ctx, sessionKey, token)
	if err != nil {
		return entity.WorkClaim{}, err
	}
	if session.ActiveTurnID == 0 {
		return entity.WorkClaim{}, fmt.Errorf("session has no active turn")
	}
	_, ok := svc.cfg.Store.(adminrepo.CoordinationRepository)
	if !ok {
		return entity.WorkClaim{}, fmt.Errorf("active work registry is not configured")
	}
	command.Summary = strings.TrimSpace(command.Summary)
	if command.Summary == "" || len([]rune(command.Summary)) > 2000 {
		return entity.WorkClaim{}, fmt.Errorf("work summary is required and must be no longer than 2000 characters")
	}
	if len(command.Domains) > 50 || len(command.ResourceKeys) > 100 || len(command.Links) > 50 {
		return entity.WorkClaim{}, fmt.Errorf("work context contains too many domains, resources, or links")
	}
	if containsLikelySecret(append(append(append([]string{command.Summary}, command.Domains...), command.ResourceKeys...), command.Links...)...) {
		return entity.WorkClaim{}, fmt.Errorf("work context contains a likely secret assignment")
	}
	var claim entity.WorkClaim
	err = svc.withCurrentSessionPersistenceGuard(ctx, session, "agent_session.work_context_update.side_effect", func(current entity.AgentSession, guardedStore adminrepo.Repository) error {
		if permissionErr := svc.requireCoordinationPermission(ctx, current, entity.CoordinationCapabilityUpdateOwnWork, "", 0); permissionErr != nil {
			return permissionErr
		}
		coordinationStore, ok := guardedStore.(adminrepo.CoordinationRepository)
		if !ok {
			return fmt.Errorf("active work registry is not configured")
		}
		var updateErr error
		claim, updateErr = coordinationStore.UpdateWorkClaim(ctx, adminrepo.UpdateWorkClaimInput{
			TurnID: current.ActiveTurnID, Summary: command.Summary, Domains: normalizedStrings(command.Domains),
			ResourceKeys: normalizedStrings(command.ResourceKeys), Links: normalizedStrings(command.Links),
		})
		return updateErr
	})
	return claim, err
}

func (svc *AgentSessionService) queuedTurnForProcess(ctx context.Context, parentTurnID int64, queuedTurns []entity.AgentSessionTurn) (entity.AgentSessionTurn, bool, error) {
	return svc.queuedTurnForProcessWithStore(ctx, svc.cfg.Store, parentTurnID, queuedTurns)
}

func (svc *AgentSessionService) queuedTurnForProcessWithStore(ctx context.Context, repository adminrepo.Repository, parentTurnID int64, queuedTurns []entity.AgentSessionTurn) (entity.AgentSessionTurn, bool, error) {
	if len(queuedTurns) == 0 {
		return entity.AgentSessionTurn{}, false, nil
	}
	store, ok := repository.(adminrepo.CoordinationRepository)
	if !ok {
		return queuedTurns[0], true, nil
	}
	parentProcess, err := store.GetTurnProcess(ctx, parentTurnID)
	if errors.Is(err, adminrepo.ErrNotFound) {
		return entity.AgentSessionTurn{}, false, nil
	}
	if err != nil {
		return entity.AgentSessionTurn{}, false, err
	}
	for _, queuedTurn := range queuedTurns {
		queuedProcess, err := store.GetTurnProcess(ctx, queuedTurn.ID)
		if errors.Is(err, adminrepo.ErrNotFound) {
			continue
		}
		if err != nil {
			return entity.AgentSessionTurn{}, false, err
		}
		if queuedProcess.ProcessRunID == parentProcess.ProcessRunID {
			return queuedTurn, true, nil
		}
	}
	return entity.AgentSessionTurn{}, false, nil
}

func (svc *AgentSessionService) RequestOwnerAttention(ctx context.Context, sessionKey string, token string, command AgentSessionOwnerAttentionCommand) (entity.OwnerAttentionRequest, error) {
	session, err := svc.authorize(ctx, sessionKey, token)
	if err != nil {
		return entity.OwnerAttentionRequest{}, err
	}
	store, ok := svc.cfg.Store.(adminrepo.CoordinationRepository)
	if !ok {
		return entity.OwnerAttentionRequest{}, fmt.Errorf("owner attention is not configured")
	}
	var process entity.ProcessContext
	err = svc.withCurrentSessionRuntimeGuard(ctx, session, "agent_session.owner_attention_read.side_effect", func(current entity.AgentSession) error {
		if permissionErr := svc.requireCoordinationPermission(ctx, current, entity.CoordinationCapabilityRequestAttention, "", 0); permissionErr != nil {
			return permissionErr
		}
		var processErr error
		process, processErr = store.GetTurnProcess(ctx, current.ActiveTurnID)
		return processErr
	})
	if err != nil {
		return entity.OwnerAttentionRequest{}, err
	}
	command.Severity = defaultString(strings.ToLower(strings.TrimSpace(command.Severity)), "normal")
	command.Summary = strings.TrimSpace(command.Summary)
	command.PauseScope = defaultString(strings.ToLower(strings.TrimSpace(command.PauseScope)), "turn")
	command.IdempotencyKey = strings.TrimSpace(command.IdempotencyKey)
	if command.Summary == "" || command.IdempotencyKey == "" {
		return entity.OwnerAttentionRequest{}, fmt.Errorf("summary and idempotency key are required")
	}
	if !stringInSet(command.Severity, "normal", "urgent", "critical") {
		return entity.OwnerAttentionRequest{}, fmt.Errorf("attention severity must be normal, urgent, or critical")
	}
	if !stringInSet(command.PauseScope, "turn", "wave", "process") {
		return entity.OwnerAttentionRequest{}, fmt.Errorf("attention pause scope must be turn, wave, or process")
	}
	if len([]rune(command.Summary)) > 4000 || len([]rune(command.IdempotencyKey)) > 200 || len(command.Options) > 8 || len(command.EvidenceLinks) > 20 {
		return entity.OwnerAttentionRequest{}, fmt.Errorf("attention request exceeds the allowed size")
	}
	if containsLikelySecret(append(append([]string{command.Summary, command.Recommendation}, command.Options...), command.EvidenceLinks...)...) {
		return entity.OwnerAttentionRequest{}, fmt.Errorf("attention request contains a likely secret assignment")
	}
	var request entity.OwnerAttentionRequest
	err = svc.withCurrentSessionPersistenceGuard(ctx, session, "agent_session.owner_attention_persist.side_effect", func(current entity.AgentSession, guardedStore adminrepo.Repository) error {
		coordinationStore, ok := guardedStore.(adminrepo.CoordinationRepository)
		if !ok {
			return fmt.Errorf("owner attention is not configured")
		}
		var persistErr error
		request, _, persistErr = coordinationStore.CreateOwnerAttention(ctx, adminrepo.CreateOwnerAttentionInput{
			ProcessRunID: process.ProcessRunID, TurnID: current.ActiveTurnID, Severity: command.Severity,
			Summary: command.Summary, Options: normalizedStrings(command.Options), Recommendation: strings.TrimSpace(command.Recommendation),
			EvidenceLinks: normalizedStrings(command.EvidenceLinks), PauseScope: command.PauseScope, IdempotencyKey: command.IdempotencyKey,
		})
		return persistErr
	})
	if err != nil || strings.TrimSpace(request.MattermostPostID) != "" {
		return request, err
	}
	message := svc.ownerAttentionMessage(process.RootInitiatorUserName, command)
	ref, err := svc.postSessionThreadMessageOnlyWithProps(ctx, session, session.MattermostChannelID,
		session.MattermostRootPostID, agentNoTriggerMessage(message), map[string]any{
			"matter_codex_event": "owner_attention",
			"process_run_id":     process.ProcessPublicID,
			"attention_id":       request.ID,
		})
	if err != nil {
		return entity.OwnerAttentionRequest{}, err
	}
	var updated entity.OwnerAttentionRequest
	err = svc.withCurrentSessionPersistenceGuard(ctx, session, "agent_session.owner_attention_post_persist.side_effect", func(_ entity.AgentSession, guardedStore adminrepo.Repository) error {
		coordinationStore, ok := guardedStore.(adminrepo.CoordinationRepository)
		if !ok {
			return fmt.Errorf("owner attention is not configured")
		}
		var updateErr error
		updated, updateErr = coordinationStore.SetOwnerAttentionPost(ctx, request.ID, ref.PostID)
		return updateErr
	})
	return updated, err
}

func (svc *AgentSessionService) notifyRootInitiatorFailure(ctx context.Context, session entity.AgentSession, turn entity.AgentSessionTurn, command CompleteAgentSessionTurnCommand) error {
	store, ok := svc.cfg.Store.(adminrepo.CoordinationRepository)
	if !ok || svc.cfg.ThreadPublisher == nil {
		return nil
	}
	process, err := store.GetTurnProcess(ctx, turn.ID)
	if err != nil {
		return err
	}
	role, err := svc.cfg.Store.GetAgentRole(ctx, session.RoleID)
	if err != nil {
		return err
	}
	errorMessage := safeFailureSummary(defaultString(command.ErrorMessage, svc.t("coordination.failure.unknown", nil)))
	summary := svc.t("coordination.failure.summary", map[string]any{
		"Agent": defaultString(mentionableMattermostUsername(role.BotIdentity), role.Name),
		"RunID": turn.RunID,
		"Error": errorMessage,
	})
	request, _, err := store.CreateOwnerAttention(ctx, adminrepo.CreateOwnerAttentionInput{
		ProcessRunID:   process.ProcessRunID,
		TurnID:         turn.ID,
		Severity:       "urgent",
		Summary:        summary,
		Recommendation: svc.t("coordination.failure.recommendation", nil),
		EvidenceLinks: []string{
			svc.mattermostThreadURLForProcess(ctx, process),
			svc.mattermostThreadURLForSession(ctx, session),
		},
		PauseScope:     "wave",
		IdempotencyKey: fmt.Sprintf("turn-%d-final-failure", turn.ID),
	})
	if err != nil || request.MattermostPostID != "" {
		return err
	}
	lineage, _ := store.GetTurnLineage(ctx, turn.ID)
	var body strings.Builder
	if userName := mentionableMattermostUsername(process.RootInitiatorUserName); userName != "" {
		body.WriteString("@")
		body.WriteString(userName)
		body.WriteString(" ")
	}
	body.WriteString(svc.t("coordination.failure.header", nil))
	body.WriteString("\n\n")
	body.WriteString(svc.t("coordination.failure.process", nil))
	body.WriteString(" `")
	body.WriteString(process.ProcessPublicID)
	body.WriteString("`\n")
	body.WriteString(svc.t("coordination.failure.agent", nil))
	body.WriteString(" @")
	body.WriteString(defaultString(mentionableMattermostUsername(role.BotIdentity), role.Name))
	body.WriteString("\n")
	body.WriteString(svc.t("coordination.failure.run", nil))
	body.WriteString(" `")
	body.WriteString(turn.RunID)
	body.WriteString("`\n")
	body.WriteString(svc.t("coordination.failure.error", nil))
	body.WriteString(" ")
	body.WriteString(errorMessage)
	if len(lineage) > 0 {
		body.WriteString("\n")
		body.WriteString(svc.t("coordination.failure.chain", nil))
		body.WriteString(" ")
		body.WriteString(svc.processLineageMarkdown(ctx, process.ProjectID, lineage))
	}
	if rootURL := svc.mattermostThreadURLForProcess(ctx, process); rootURL != "" {
		body.WriteString("\n")
		body.WriteString(svc.t("coordination.failure.source", nil))
		body.WriteString(" ")
		body.WriteString(rootURL)
	}
	ref, err := svc.postSessionThreadMessageOnlyWithProps(ctx, session, turn.MattermostChannelID,
		turn.MattermostRootPostID, agentNoTriggerMessage(body.String()), map[string]any{
			"matter_codex_event": "process_failure_attention",
			"process_run_id":     process.ProcessPublicID,
			"turn_id":            turn.ID,
		})
	if err != nil {
		return err
	}
	_, err = store.SetOwnerAttentionPost(ctx, request.ID, ref.PostID)
	return err
}

func (svc *AgentSessionService) processLineageMarkdown(ctx context.Context, projectID int64, lineage []entity.ProcessLineageStep) string {
	project, err := svc.cfg.Store.GetProject(ctx, projectID)
	if err != nil {
		return ""
	}
	parts := make([]string, 0, len(lineage))
	for _, step := range lineage {
		label := "@" + defaultString(mentionableMattermostUsername(step.BotIdentity), step.RoleName)
		if url := svc.mattermostThreadURL(project.Slug, step.LaunchPostID); url != "" {
			label = "[" + label + "](" + url + ")"
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, " -> ")
}

func (svc *AgentSessionService) mattermostThreadURLForProcess(ctx context.Context, process entity.ProcessContext) string {
	project, err := svc.cfg.Store.GetProject(ctx, process.ProjectID)
	if err != nil {
		return ""
	}
	return svc.mattermostThreadURL(project.Slug, defaultString(process.RootTriggerPostID, process.RootThreadPostID))
}

func (svc *AgentSessionService) mattermostThreadURLForSession(ctx context.Context, session entity.AgentSession) string {
	project, err := svc.cfg.Store.GetProject(ctx, session.ProjectID)
	if err != nil {
		return ""
	}
	return svc.mattermostThreadURL(project.Slug, session.MattermostRootPostID)
}

func safeFailureSummary(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "runtime failure"
	}
	lines := strings.Split(value, "\n")
	for index, line := range lines {
		lower := strings.ToLower(line)
		if likelySecretAssignment.MatchString(line) || strings.Contains(lower, "private key") || strings.Contains(lower, "postgres://") {
			lines[index] = "[скрыто: потенциальный секрет]"
		}
	}
	value = strings.Join(lines, " ")
	const maxRunes = 1200
	runes := []rune(value)
	if len(runes) > maxRunes {
		value = string(runes[:maxRunes]) + "..."
	}
	return value
}

func stringInSet(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func containsLikelySecret(values ...string) bool {
	for _, value := range values {
		if likelySecretAssignment.MatchString(value) {
			return true
		}
	}
	return false
}

func (svc *AgentSessionService) ownerAttentionMessage(userName string, command AgentSessionOwnerAttentionCommand) string {
	var body strings.Builder
	mention := mentionableMattermostUsername(userName)
	if mention != "" {
		body.WriteString("@")
		body.WriteString(mention)
		body.WriteString(" ")
	}
	body.WriteString(svc.t("coordination.attention.header", nil))
	body.WriteString("\n\n")
	body.WriteString(svc.t("coordination.attention.reason", nil))
	body.WriteString(" ")
	body.WriteString(command.Summary)
	if len(command.Options) > 0 {
		body.WriteString("\n\n")
		body.WriteString(svc.t("coordination.attention.options", nil))
		body.WriteString("\n")
		for _, option := range command.Options {
			body.WriteString("- ")
			body.WriteString(option)
			body.WriteString("\n")
		}
	}
	if strings.TrimSpace(command.Recommendation) != "" {
		body.WriteString("\n")
		body.WriteString(svc.t("coordination.attention.recommendation", nil))
		body.WriteString(" ")
		body.WriteString(command.Recommendation)
	}
	return strings.TrimSpace(body.String())
}

func normalizedStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
