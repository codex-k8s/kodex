package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/value"
)

const (
	menuViewAutomations = "automations"

	menuDialogAutomationCreate = "automation_create"

	menuActionAutomationHistory = "automation_history"
	menuActionAutomationRunNow  = "automation_run_now"

	menuResourceAutomationSchedule = "automation_schedule"
	menuResourceAutomationRun      = "automation_run"

	dialogCallbackAutomationCreate = "agents_automation_create"

	dialogFieldAutomationProject  = "automation_project"
	dialogFieldAutomationRole     = "automation_role"
	dialogFieldAutomationChat     = "automation_chat"
	dialogFieldAutomationName     = "automation_name"
	dialogFieldAutomationPreset   = "automation_preset"
	dialogFieldAutomationTime     = "automation_time"
	dialogFieldAutomationTimeZone = "automation_time_zone"
	dialogFieldAutomationPlaybook = "automation_playbook"
)

func (svc *SlashCommandService) automationScheduleListCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewAutomations)
	card.Title = svc.t("automation.list.title", nil)
	card.Fields = nil
	projectID, ok := svc.automationProjectID(ctx, command)
	if !ok || svc.cfg.Automations == nil || !svc.cfg.Automations.Available() {
		card.Text = svc.t("automation.unavailable", nil)
		card.Actions = svc.automationNavigationActions()
		return card
	}
	actor := AuthenticatedActor{UserID: command.UserID, UserName: command.UserName}
	schedules, err := svc.cfg.Automations.ListSchedules(ctx, actor, projectID, 100)
	if err != nil {
		card.Text = svc.t("automation.forbidden_or_failed", nil)
		card.Actions = svc.automationNavigationActions()
		return card
	}
	card.Text = svc.t("automation.list.text", map[string]any{"Count": len(schedules)})
	card.Actions = nil
	if len(schedules) == 0 {
		card.Text = svc.t("automation.list.empty", nil)
	}
	start, end, page := entityPageBounds(len(schedules), command.Page)
	for index, schedule := range schedules[start:end] {
		card.Fields = append(card.Fields, MattermostCardField{
			Title: svc.t("automation.schedule.list_item.title", map[string]any{"Number": start + index + 1, "Name": schedule.Name}),
			Value: svc.t("automation.schedule.list_item.value", map[string]any{
				"Project": schedule.ProjectName,
				"Role":    schedule.TargetAgentRoleName,
				"Chat":    schedule.TargetChatName,
				"Time":    schedule.LocalTime,
				"Zone":    schedule.TimeZone,
				"Next":    formatAutomationTime(schedule.NextRunAt, schedule.TimeZone),
			}),
		})
		card.Actions = append(card.Actions, svc.menuResourceAction(
			menuViewAutomations,
			fmt.Sprintf("automationschedule%d", index),
			menuActionShow,
			menuResourceAutomationSchedule,
			automationResourceID(schedule.ProjectID, schedule.PublicID),
			"automation.action.open",
			"automation.action.open.tooltip",
			"default",
			map[string]any{"Name": schedule.Name},
		))
	}
	card.Actions = append(card.Actions, svc.pageActions(menuViewAutomations, menuResourceAutomationSchedule, strconv.FormatInt(projectID, 10), page, len(schedules))...)
	card.Actions = append(card.Actions, svc.automationCreateAction())
	card.Actions = append(card.Actions, svc.automationNavigationActions()...)
	return card
}

func (svc *SlashCommandService) automationScheduleCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewAutomations)
	projectID, publicID, ok := parseAutomationResourceID(command.ID)
	if !ok || svc.cfg.Automations == nil {
		return svc.automationErrorCard(card)
	}
	actor := AuthenticatedActor{UserID: command.UserID, UserName: command.UserName}
	schedule, err := svc.cfg.Automations.GetSchedule(ctx, actor, publicID, projectID)
	if err != nil {
		return svc.automationErrorCard(card)
	}
	card.Title = svc.t("automation.schedule.title", map[string]any{"Name": schedule.Name})
	card.Text = svc.t("automation.schedule.text", nil)
	card.Fields = []MattermostCardField{
		{Title: svc.t("automation.field.project", nil), Value: schedule.ProjectName, Short: true},
		{Title: svc.t("automation.field.state", nil), Value: svc.t("automation.state.enabled", nil), Short: true},
		{Title: svc.t("automation.field.role", nil), Value: schedule.TargetAgentRoleName, Short: true},
		{Title: svc.t("automation.field.chat", nil), Value: schedule.TargetChatName, Short: true},
		{Title: svc.t("automation.field.schedule", nil), Value: svc.t("automation.schedule.daily", map[string]any{"Time": schedule.LocalTime, "Zone": schedule.TimeZone}), Short: false},
		{Title: svc.t("automation.field.next_run", nil), Value: formatAutomationTime(schedule.NextRunAt, schedule.TimeZone), Short: false},
		{Title: svc.t("automation.field.playbook", nil), Value: svc.t("automation.playbook.project_check", nil), Short: false},
	}
	runAction := svc.menuResourceAction(menuViewAutomations, "automationrunnow", menuActionAutomationRunNow, menuResourceAutomationSchedule, automationResourceID(schedule.ProjectID, schedule.PublicID), "automation.action.run_now", "automation.action.run_now.tooltip", "primary", nil)
	runAction.Context["idempotency_key"] = newAutomationID("command")
	card.Actions = []MattermostCardAction{
		runAction,
		svc.menuResourceAction(menuViewAutomations, "automationhistory", menuActionAutomationHistory, menuResourceAutomationSchedule, automationResourceID(schedule.ProjectID, schedule.PublicID), "automation.action.history", "automation.action.history.tooltip", "default", nil),
		svc.menuResourceAction(menuViewAutomations, "automationlist", menuActionList, menuResourceAutomationSchedule, strconv.FormatInt(schedule.ProjectID, 10), "automation.action.list", "automation.action.list.tooltip", "default", nil),
	}
	card.Actions = append(card.Actions, svc.automationNavigationActions()...)
	return card
}

func (svc *SlashCommandService) automationRunNowCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewAutomations)
	projectID, scheduleID, ok := parseAutomationResourceID(command.ID)
	if !ok || svc.cfg.Automations == nil {
		return svc.automationErrorCard(card)
	}
	result, err := svc.cfg.Automations.RunNow(ctx, RunAutomationNowCommand{
		Actor:          AuthenticatedActor{UserID: command.UserID, UserName: command.UserName},
		ProjectID:      projectID,
		ScheduleID:     scheduleID,
		IdempotencyKey: command.IdempotencyKey,
	})
	if result.Run.PublicID == "" {
		return svc.automationErrorCard(card)
	}
	card = svc.automationRunCard(ctx, result.Run)
	if err != nil {
		card.Text = svc.t("automation.run.failed_to_start", nil)
	}
	if result.Duplicate {
		card.Text = svc.t("automation.run.duplicate", nil)
	}
	return card
}

func (svc *SlashCommandService) automationRunEntityCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewAutomations)
	projectID, publicID, ok := parseAutomationResourceID(command.ID)
	if !ok || svc.cfg.Automations == nil {
		return svc.automationErrorCard(card)
	}
	run, err := svc.cfg.Automations.GetRun(ctx, AuthenticatedActor{UserID: command.UserID, UserName: command.UserName}, publicID, projectID)
	if err != nil {
		return svc.automationErrorCard(card)
	}
	return svc.automationRunCard(ctx, run)
}

func (svc *SlashCommandService) automationHistoryCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewAutomations)
	projectID, scheduleID, ok := parseAutomationResourceID(command.ID)
	if !ok || svc.cfg.Automations == nil {
		return svc.automationErrorCard(card)
	}
	runs, err := svc.cfg.Automations.ListRuns(ctx, AuthenticatedActor{UserID: command.UserID, UserName: command.UserName}, scheduleID, projectID, 100)
	if err != nil {
		return svc.automationErrorCard(card)
	}
	card.Title = svc.t("automation.history.title", nil)
	card.Text = svc.t("automation.history.text", map[string]any{"Count": len(runs)})
	card.Fields = nil
	card.Actions = nil
	if len(runs) == 0 {
		card.Text = svc.t("automation.history.empty", nil)
	}
	start, end, page := entityPageBounds(len(runs), command.Page)
	for index, run := range runs[start:end] {
		card.Fields = append(card.Fields, MattermostCardField{
			Title: svc.t("automation.run.list_item.title", map[string]any{"Number": start + index + 1, "Schedule": run.ScheduleName}),
			Value: svc.t("automation.run.list_item.value", map[string]any{
				"Status":  svc.automationStatusText(run.Status),
				"Outcome": svc.automationOutcomeText(run.Outcome),
				"Created": formatAutomationTime(run.CreatedAt, "UTC"),
				"Summary": defaultString(run.SafeSummary, svc.t("automation.summary.pending", nil)),
			}),
		})
		card.Actions = append(card.Actions, svc.menuResourceAction(menuViewAutomations, fmt.Sprintf("automationrun%d", index), menuActionShow, menuResourceAutomationRun, automationResourceID(run.ProjectID, run.PublicID), "automation.action.run_open", "automation.action.run_open.tooltip", "default", nil))
	}
	card.Actions = append(card.Actions, svc.automationHistoryPageActions(projectID, scheduleID, page, len(runs))...)
	card.Actions = append(card.Actions, svc.menuResourceAction(menuViewAutomations, "automationschedule", menuActionShow, menuResourceAutomationSchedule, automationResourceID(projectID, scheduleID), "automation.action.schedule_back", "automation.action.schedule_back.tooltip", "default", nil))
	card.Actions = append(card.Actions, svc.automationNavigationActions()...)
	return card
}

func (svc *SlashCommandService) automationRunCard(ctx context.Context, run entity.ScheduledRun) *MattermostCard {
	card := svc.menuCard(ctx, menuViewAutomations)
	card.Title = svc.t("automation.run.title", map[string]any{"Schedule": run.ScheduleName})
	card.Color = automationStatusColor(run.Status)
	card.Text = svc.t("automation.run.text", nil)
	card.Fields = []MattermostCardField{
		{Title: svc.t("automation.field.state", nil), Value: svc.automationStatusText(run.Status), Short: true},
		{Title: svc.t("automation.field.outcome", nil), Value: svc.automationOutcomeText(run.Outcome), Short: true},
		{Title: svc.t("automation.field.project", nil), Value: run.ProjectName, Short: true},
		{Title: svc.t("automation.field.role", nil), Value: run.TargetAgentRoleName, Short: true},
		{Title: svc.t("automation.field.chat", nil), Value: run.TargetChatName, Short: true},
		{Title: svc.t("automation.field.created", nil), Value: formatAutomationTime(run.CreatedAt, "UTC"), Short: true},
		{Title: svc.t("automation.field.summary", nil), Value: defaultString(run.SafeSummary, svc.t("automation.summary.pending", nil)), Short: false},
	}
	card.Actions = []MattermostCardAction{
		svc.menuResourceAction(menuViewAutomations, "automationhistory", menuActionAutomationHistory, menuResourceAutomationSchedule, automationResourceID(run.ProjectID, run.SchedulePublicID), "automation.action.history", "automation.action.history.tooltip", "default", nil),
		svc.menuResourceAction(menuViewAutomations, "automationschedule", menuActionShow, menuResourceAutomationSchedule, automationResourceID(run.ProjectID, run.SchedulePublicID), "automation.action.schedule_back", "automation.action.schedule_back.tooltip", "default", nil),
	}
	card.Actions = append(card.Actions, svc.automationNavigationActions()...)
	return card
}

func (svc *SlashCommandService) automationCreateDialog(ctx context.Context, command MenuActionCommand) (*MattermostDialog, string) {
	if svc.cfg.Store == nil || svc.cfg.Automations == nil || !svc.cfg.Automations.Available() {
		return nil, svc.t("automation.unavailable", nil)
	}
	projects, err := svc.cfg.Store.ListProjects(ctx, 100)
	if err != nil || len(projects) == 0 {
		return nil, svc.t("automation.target_options.unavailable", nil)
	}
	roles, roleErr := svc.cfg.Store.ListAgentRoles(ctx, 0)
	chats, chatErr := svc.cfg.Store.ListChats(ctx, 0)
	if roleErr != nil || chatErr != nil || len(roles) == 0 || len(chats) == 0 {
		return nil, svc.t("automation.target_options.unavailable", nil)
	}
	projectNames := make(map[int64]string, len(projects))
	projectOptions := make([]MattermostDialogOption, 0, len(projects))
	for _, project := range projects {
		projectNames[project.ID] = project.Name
		projectOptions = append(projectOptions, MattermostDialogOption{Text: project.Name, Value: strconv.FormatInt(project.ID, 10)})
	}
	roleOptions := make([]MattermostDialogOption, 0, len(roles))
	for _, role := range roles {
		if role.Enabled {
			roleOptions = append(roleOptions, MattermostDialogOption{Text: fmt.Sprintf("%s — %s", projectNames[role.ProjectID], role.Name), Value: strconv.FormatInt(role.ID, 10)})
		}
	}
	chatOptions := make([]MattermostDialogOption, 0, len(chats))
	for _, chat := range chats {
		chatOptions = append(chatOptions, MattermostDialogOption{Text: fmt.Sprintf("%s — %s", projectNames[chat.ProjectID], chat.Name), Value: strconv.FormatInt(chat.ID, 10)})
	}
	if command.IdempotencyKey == "" {
		command.IdempotencyKey = newAutomationID("command")
	}
	return &MattermostDialog{
		SubmitURL:        svc.cfg.DialogSubmitURL,
		CallbackID:       dialogCallbackAutomationCreate,
		Title:            svc.t("automation.dialog.title", nil),
		IntroductionText: svc.t("automation.dialog.intro", nil),
		SubmitLabel:      svc.t("automation.dialog.submit", nil),
		State:            encodeDialogState(command),
		Elements: []MattermostDialogElement{
			{DisplayName: svc.t("automation.dialog.project", nil), Name: dialogFieldAutomationProject, Type: "select", Options: projectOptions},
			{DisplayName: svc.t("automation.dialog.role", nil), Name: dialogFieldAutomationRole, Type: "select", Options: roleOptions, HelpText: svc.t("automation.dialog.role.help", nil)},
			{DisplayName: svc.t("automation.dialog.chat", nil), Name: dialogFieldAutomationChat, Type: "select", Options: chatOptions, HelpText: svc.t("automation.dialog.chat.help", nil)},
			{DisplayName: svc.t("automation.dialog.name", nil), Name: dialogFieldAutomationName, Type: "text", MinLength: 1, MaxLength: maxAutomationScheduleName, Placeholder: svc.t("automation.dialog.name.placeholder", nil)},
			{DisplayName: svc.t("automation.dialog.preset", nil), Name: dialogFieldAutomationPreset, Type: "select", Default: string(value.AutomationSchedulePresetDaily), Options: []MattermostDialogOption{{Text: svc.t("automation.dialog.preset.daily", nil), Value: string(value.AutomationSchedulePresetDaily)}}},
			{DisplayName: svc.t("automation.dialog.time", nil), Name: dialogFieldAutomationTime, Type: "text", SubType: "text", Default: "09:00", MinLength: 5, MaxLength: 5, Placeholder: "09:00"},
			{DisplayName: svc.t("automation.dialog.time_zone", nil), Name: dialogFieldAutomationTimeZone, Type: "select", Default: "UTC", Options: automationTimeZoneOptions()},
			{DisplayName: svc.t("automation.dialog.playbook", nil), Name: dialogFieldAutomationPlaybook, Type: "select", Default: value.AutomationPlaybookProjectCheckV1, Options: []MattermostDialogOption{{Text: svc.t("automation.playbook.project_check", nil), Value: value.AutomationPlaybookProjectCheckV1}}},
		},
	}, ""
}

func (svc *SlashCommandService) handleAutomationCreateDialog(ctx context.Context, command DialogSubmissionCommand, state mattermostDialogState) DialogSubmissionResult {
	input, fieldErrors := svc.automationDialogInput(command.Submission)
	if len(fieldErrors) > 0 {
		return DialogSubmissionResult{StatusCode: 200, Errors: fieldErrors}
	}
	schedule, _, err := svc.cfg.Automations.CreateSchedule(ctx, CreateAutomationScheduleCommand{
		Actor:             AuthenticatedActor{UserID: command.UserID, UserName: defaultString(command.UserName, state.UserName)},
		ProjectID:         input.ProjectID,
		TargetAgentRoleID: input.RoleID,
		TargetChatID:      input.ChatID,
		Name:              input.Name,
		Preset:            input.Preset,
		LocalTime:         input.LocalTime,
		TimeZone:          input.TimeZone,
		PlaybookKey:       input.PlaybookKey,
		IdempotencyKey:    state.IdempotencyKey,
	})
	if err != nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("automation.create.failed", nil)}
	}
	card := svc.automationScheduleCard(ctx, MenuActionCommand{
		View:      menuViewAutomations,
		ID:        automationResourceID(schedule.ProjectID, schedule.PublicID),
		UserID:    command.UserID,
		UserName:  defaultString(command.UserName, state.UserName),
		ChannelID: defaultString(state.ChannelID, command.ChannelID),
		PostID:    state.PostID,
	})
	return DialogSubmissionResult{StatusCode: 200, Card: card}
}

type automationDialogValues struct {
	ProjectID   int64
	RoleID      int64
	ChatID      int64
	Name        string
	Preset      string
	LocalTime   string
	TimeZone    string
	PlaybookKey string
}

func (svc *SlashCommandService) automationDialogInput(submission map[string]any) (automationDialogValues, map[string]string) {
	input := automationDialogValues{
		Name:        strings.TrimSpace(submissionString(submission, dialogFieldAutomationName)),
		Preset:      strings.TrimSpace(submissionString(submission, dialogFieldAutomationPreset)),
		LocalTime:   strings.TrimSpace(submissionString(submission, dialogFieldAutomationTime)),
		TimeZone:    strings.TrimSpace(submissionString(submission, dialogFieldAutomationTimeZone)),
		PlaybookKey: strings.TrimSpace(submissionString(submission, dialogFieldAutomationPlaybook)),
	}
	input.ProjectID, _ = strconv.ParseInt(submissionString(submission, dialogFieldAutomationProject), 10, 64)
	input.RoleID, _ = strconv.ParseInt(submissionString(submission, dialogFieldAutomationRole), 10, 64)
	input.ChatID, _ = strconv.ParseInt(submissionString(submission, dialogFieldAutomationChat), 10, 64)
	errorsByField := map[string]string{}
	if input.ProjectID <= 0 {
		errorsByField[dialogFieldAutomationProject] = svc.t("automation.validation.project", nil)
	}
	if input.RoleID <= 0 {
		errorsByField[dialogFieldAutomationRole] = svc.t("automation.validation.role", nil)
	}
	if input.ChatID <= 0 {
		errorsByField[dialogFieldAutomationChat] = svc.t("automation.validation.chat", nil)
	}
	if input.Name == "" || len([]rune(input.Name)) > maxAutomationScheduleName {
		errorsByField[dialogFieldAutomationName] = svc.t("automation.validation.name", nil)
	}
	if input.Preset != string(value.AutomationSchedulePresetDaily) {
		errorsByField[dialogFieldAutomationPreset] = svc.t("automation.validation.preset", nil)
	}
	if _, err := time.Parse("15:04", input.LocalTime); err != nil {
		errorsByField[dialogFieldAutomationTime] = svc.t("automation.validation.time", nil)
	}
	if _, err := loadAutomationTimeZone(input.TimeZone); err != nil {
		errorsByField[dialogFieldAutomationTimeZone] = svc.t("automation.validation.time_zone", nil)
	}
	if input.PlaybookKey != value.AutomationPlaybookProjectCheckV1 {
		errorsByField[dialogFieldAutomationPlaybook] = svc.t("automation.validation.playbook", nil)
	}
	return input, errorsByField
}

func (svc *SlashCommandService) automationProjectID(ctx context.Context, command MenuActionCommand) (int64, bool) {
	if projectID, ok := parseInt64ID(command.ID); ok {
		return projectID, true
	}
	if projectID, _, ok := parseAutomationResourceID(command.ID); ok {
		return projectID, true
	}
	if svc.cfg.Store == nil || strings.TrimSpace(command.ChannelID) == "" {
		return 0, false
	}
	chat, err := svc.cfg.Store.GetChatByMattermostChannelID(ctx, command.ChannelID)
	return chat.ProjectID, err == nil && chat.ProjectID > 0
}

func (svc *SlashCommandService) automationCreateAction() MattermostCardAction {
	action := svc.menuDialogAction(menuViewAutomations, "automationcreate", menuDialogAutomationCreate, "automation.action.create", "automation.action.create.tooltip", "primary")
	action.Context["idempotency_key"] = newAutomationID("command")
	return action
}

func (svc *SlashCommandService) automationNavigationActions() []MattermostCardAction {
	return []MattermostCardAction{svc.menuAction(menuViewMain, "menu.action.main", "menu.action.main.tooltip", "default")}
}

func (svc *SlashCommandService) automationHistoryPageActions(projectID int64, scheduleID string, page int, total int) []MattermostCardAction {
	pages := (total + entityListPageSize - 1) / entityListPageSize
	actions := make([]MattermostCardAction, 0, 2)
	add := func(actionID string, targetPage int, nameID string, tooltipID string) {
		action := svc.menuResourceAction(menuViewAutomations, actionID, menuActionAutomationHistory, menuResourceAutomationSchedule, automationResourceID(projectID, scheduleID), nameID, tooltipID, "default", nil)
		action.Context["page"] = targetPage
		actions = append(actions, action)
	}
	if page > 0 {
		add("automationhistoryprev", page-1, "menu.action.prev_page", "menu.action.prev_page.tooltip")
	}
	if page+1 < pages {
		add("automationhistorynext", page+1, "menu.action.next_page", "menu.action.next_page.tooltip")
	}
	return actions
}

func (svc *SlashCommandService) automationErrorCard(card *MattermostCard) *MattermostCard {
	card.Title = svc.t("automation.error.title", nil)
	card.Text = svc.t("automation.forbidden_or_failed", nil)
	card.Fields = nil
	card.Actions = svc.automationNavigationActions()
	return card
}

func (svc *SlashCommandService) automationStatusText(status string) string {
	switch status {
	case string(value.AutomationRunStatusQueued):
		return svc.t("automation.status.queued", nil)
	case string(value.AutomationRunStatusRunning):
		return svc.t("automation.status.running", nil)
	case string(value.AutomationRunStatusSucceeded):
		return svc.t("automation.status.succeeded", nil)
	case string(value.AutomationRunStatusFailed):
		return svc.t("automation.status.failed", nil)
	default:
		return svc.t("automation.status.unknown", nil)
	}
}

func (svc *SlashCommandService) automationOutcomeText(outcome string) string {
	switch outcome {
	case "":
		return svc.t("automation.outcome.pending", nil)
	case string(value.AutomationRunOutcomeNoAction):
		return svc.t("automation.outcome.no_action", nil)
	case string(value.AutomationRunOutcomeActionTaken):
		return svc.t("automation.outcome.action_taken", nil)
	case string(value.AutomationRunOutcomeRequiresHuman):
		return svc.t("automation.outcome.requires_human", nil)
	case string(value.AutomationRunOutcomeFailed):
		return svc.t("automation.outcome.failed", nil)
	default:
		return svc.t("automation.outcome.unknown", nil)
	}
}

func automationStatusColor(status string) string {
	switch status {
	case string(value.AutomationRunStatusRunning):
		return "#1c58d9"
	case string(value.AutomationRunStatusSucceeded):
		return "#227a55"
	case string(value.AutomationRunStatusFailed):
		return "#c4314b"
	default:
		return "#5b667a"
	}
}

func automationResourceID(projectID int64, publicID string) string {
	return strconv.FormatInt(projectID, 10) + "|" + strings.TrimSpace(publicID)
}

func parseAutomationResourceID(resourceID string) (int64, string, bool) {
	projectPart, publicID, ok := strings.Cut(strings.TrimSpace(resourceID), "|")
	if !ok || strings.TrimSpace(publicID) == "" {
		return 0, "", false
	}
	projectID, err := strconv.ParseInt(projectPart, 10, 64)
	if err != nil || projectID <= 0 {
		return 0, "", false
	}
	return projectID, strings.TrimSpace(publicID), true
}

func formatAutomationTime(value time.Time, timeZone string) string {
	if value.IsZero() {
		return "—"
	}
	location, err := time.LoadLocation(timeZone)
	if err != nil {
		location = time.UTC
	}
	return value.In(location).Format("2006-01-02 15:04 MST")
}

func automationTimeZoneOptions() []MattermostDialogOption {
	return []MattermostDialogOption{
		{Text: "UTC", Value: "UTC"},
		{Text: "Europe/Moscow", Value: "Europe/Moscow"},
		{Text: "Europe/Berlin", Value: "Europe/Berlin"},
		{Text: "Asia/Almaty", Value: "Asia/Almaty"},
		{Text: "Asia/Tbilisi", Value: "Asia/Tbilisi"},
		{Text: "America/New_York", Value: "America/New_York"},
	}
}
