package service

import (
	"context"
	"fmt"
	"strings"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	texti18n "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/i18n"
)

type projectRunCardInput struct {
	Localizer         *texti18n.Localizer
	Store             adminrepo.Repository
	Publisher         MattermostThreadPublisher
	MattermostSiteURL string
	Project           entity.Project
	Session           entity.AgentSession
	Turn              entity.AgentSessionTurn
	RoleName          string
	OpenAIAccountName string
	CodexLimits       string
	Status            string
}

func upsertProjectRunCard(ctx context.Context, input projectRunCardInput) (MattermostPostRef, error) {
	if input.Store == nil || input.Publisher == nil || strings.TrimSpace(input.Project.MattermostRunsChannelID) == "" {
		return MattermostPostRef{}, nil
	}
	input.Status = defaultString(strings.TrimSpace(input.Status), input.Turn.Status)
	card := MattermostCard{
		ChannelID: input.Project.MattermostRunsChannelID,
		PostID:    input.Turn.MattermostRunsPostID,
		Message:   "matter-codex project run timeline #notrigger",
		Props: map[string]any{
			"matter_codex_event":   "project_run_status",
			"project_id":           input.Project.ID,
			"session_id":           input.Session.ID,
			"turn_id":              input.Turn.ID,
			"run_id":               input.Turn.RunID,
			"parent_turn_ids":      input.Turn.ParentTurnIDs,
			"trigger_post_ids":     input.Turn.TriggerPostIDs,
			"initiator_user_names": input.Turn.InitiatorUserNames,
			"status":               input.Status,
		},
		Color: projectRunStatusColor(input.Status),
		Title: projectRunT(input.Localizer, "project.run.card.title", map[string]any{
			"Agent": defaultString(strings.TrimSpace(input.RoleName), "agent"),
		}),
		Text: projectRunT(input.Localizer, "project.run.card.text", map[string]any{
			"RunID":   input.Turn.RunID,
			"Status":  input.Status,
			"Account": emptyAsUnknown(input.OpenAIAccountName),
		}),
		Fields: []MattermostCardField{
			{Title: projectRunT(input.Localizer, "project.run.card.field.initiator", nil), Value: projectRunInitiators(input.Turn), Short: true},
			{Title: projectRunT(input.Localizer, "project.run.card.field.trigger", nil), Value: projectRunTriggerLinks(input.MattermostSiteURL, input.Project.Slug, input.Turn), Short: true},
			{Title: projectRunT(input.Localizer, "project.run.card.field.thread", nil), Value: projectRunLink(input.MattermostSiteURL, input.Project.Slug, input.Turn.MattermostRootPostID), Short: true},
		},
	}
	for _, parentTurnID := range input.Turn.ParentTurnIDs {
		parent, err := input.Store.GetAgentSessionTurn(ctx, parentTurnID)
		if err == nil && strings.TrimSpace(parent.MattermostRunsPostID) != "" {
			card.Fields = append(card.Fields, MattermostCardField{
				Title: projectRunT(input.Localizer, "project.run.card.field.parent", nil),
				Value: projectRunLink(input.MattermostSiteURL, input.Project.Slug, parent.MattermostRunsPostID),
				Short: true,
			})
		}
	}
	if limits := strings.TrimSpace(input.CodexLimits); limits != "" {
		card.Fields = append(card.Fields, MattermostCardField{
			Title: projectRunT(input.Localizer, "project.run.card.field.limits", nil),
			Value: limits,
		})
	}

	if strings.TrimSpace(input.Turn.MattermostRunsPostID) != "" {
		return input.Publisher.UpdateThreadCard(ctx, card)
	}
	ref, err := input.Publisher.PostThreadCard(ctx, card)
	if err != nil {
		return MattermostPostRef{}, err
	}
	if strings.TrimSpace(ref.PostID) == "" {
		return MattermostPostRef{}, fmt.Errorf("Mattermost runs card post id is empty")
	}
	if _, err := input.Store.UpdateAgentSessionTurnRunsPost(ctx, adminrepo.UpdateAgentSessionTurnRunsPostInput{
		TurnID:     input.Turn.ID,
		RunsPostID: ref.PostID,
	}); err != nil {
		return MattermostPostRef{}, err
	}
	return ref, nil
}

func projectRunStatusColor(status string) string {
	switch status {
	case agentSessionTurnQueued:
		return "#f5ab00"
	case agentSessionTurnRunning:
		return "#1c58d9"
	case agentSessionTurnSucceeded:
		return "#2f8f46"
	case agentSessionTurnBlocked:
		return "#e67e22"
	case agentSessionTurnFailed:
		return "#d24b40"
	default:
		return "#9aa4b2"
	}
}

func projectRunLink(siteURL string, projectSlug string, postID string) string {
	postID = strings.TrimSpace(postID)
	if postID == "" || strings.TrimSpace(siteURL) == "" || strings.TrimSpace(projectSlug) == "" {
		return "-"
	}
	url := strings.TrimRight(strings.TrimSpace(siteURL), "/") + "/" + strings.Trim(strings.TrimSpace(projectSlug), "/") + "/pl/" + postID
	return "[" + postID + "](" + url + ")"
}

func projectRunInitiators(turn entity.AgentSessionTurn) string {
	usernames := turn.InitiatorUserNames
	if len(usernames) == 0 && strings.TrimSpace(turn.UserName) != "" {
		usernames = []string{turn.UserName}
	}
	items := make([]string, 0, len(usernames))
	for _, username := range usernames {
		username = mentionableMattermostUsername(username)
		if username != "" {
			items = append(items, "@"+username)
		}
	}
	if len(items) == 0 {
		return "-"
	}
	return strings.Join(items, ", ")
}

func projectRunTriggerLinks(siteURL string, projectSlug string, turn entity.AgentSessionTurn) string {
	postIDs := turn.TriggerPostIDs
	if len(postIDs) == 0 && strings.TrimSpace(turn.MattermostPostID) != "" {
		postIDs = []string{turn.MattermostPostID}
	}
	links := make([]string, 0, len(postIDs))
	for _, postID := range postIDs {
		if link := projectRunLink(siteURL, projectSlug, postID); link != "-" {
			links = append(links, link)
		}
	}
	if len(links) == 0 {
		return "-"
	}
	return strings.Join(links, "\n")
}

func projectRunT(localizer *texti18n.Localizer, messageID string, data map[string]any) string {
	if localizer == nil {
		return messageID
	}
	return localizer.T(messageID, data)
}
