package mattermost

import (
	"context"
	"fmt"

	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
	mattermostmodel "github.com/mattermost/mattermost/server/public/model"
)

type ControlSurface struct {
	client *mattermostmodel.Client4
}

func NewControlSurface(siteURL string, token string) *ControlSurface {
	client := mattermostmodel.NewAPIv4Client(siteURL)
	client.SetToken(token)
	return &ControlSurface{client: client}
}

func (surface *ControlSurface) EnsureRepositoryChannel(ctx context.Context, teamName string, channelName string, displayName string) (bool, error) {
	team, _, err := surface.client.GetTeamByName(ctx, teamName, "")
	if err != nil {
		return false, fmt.Errorf("get Mattermost team: %w", err)
	}
	if _, response, err := surface.client.GetChannelByName(ctx, channelName, team.Id, ""); err == nil {
		return false, nil
	} else if response == nil || response.StatusCode != 404 {
		return false, fmt.Errorf("get Mattermost channel: %w", err)
	}
	if _, _, err := surface.client.CreateChannel(ctx, &mattermostmodel.Channel{
		TeamId:      team.Id,
		Name:        channelName,
		DisplayName: displayName,
		Type:        mattermostmodel.ChannelTypeOpen,
	}); err != nil {
		return false, fmt.Errorf("create Mattermost channel: %w", err)
	}
	return true, nil
}

func (surface *ControlSurface) UpsertFlowCard(ctx context.Context, card statusservice.FlowCard) (statusservice.FlowCardPost, error) {
	post := flowCardPost(card)
	if card.PostID == "" {
		created, _, err := surface.client.CreatePost(ctx, post)
		if err != nil {
			return statusservice.FlowCardPost{}, fmt.Errorf("create Mattermost flow card: %w", err)
		}
		return statusservice.FlowCardPost{ChannelID: created.ChannelId, PostID: created.Id}, nil
	}
	updated, _, err := surface.client.UpdatePost(ctx, card.PostID, post)
	if err != nil {
		return statusservice.FlowCardPost{}, fmt.Errorf("update Mattermost flow card: %w", err)
	}
	return statusservice.FlowCardPost{ChannelID: updated.ChannelId, PostID: updated.Id}, nil
}

func (surface *ControlSurface) OpenDialog(ctx context.Context, triggerID string, dialog statusservice.MattermostDialog) error {
	elements := make([]mattermostmodel.DialogElement, 0, len(dialog.Elements))
	for _, element := range dialog.Elements {
		options := make([]*mattermostmodel.PostActionOptions, 0, len(element.Options))
		for _, option := range element.Options {
			options = append(options, &mattermostmodel.PostActionOptions{
				Text:  option.Text,
				Value: option.Value,
			})
		}
		elements = append(elements, mattermostmodel.DialogElement{
			DisplayName: element.DisplayName,
			Name:        element.Name,
			Type:        element.Type,
			SubType:     element.SubType,
			Default:     element.Default,
			Placeholder: element.Placeholder,
			HelpText:    element.HelpText,
			Optional:    element.Optional,
			MinLength:   element.MinLength,
			MaxLength:   element.MaxLength,
			Options:     options,
		})
	}
	_, err := surface.client.OpenInteractiveDialog(ctx, mattermostmodel.OpenDialogRequest{
		TriggerId: triggerID,
		URL:       dialog.SubmitURL,
		Dialog: mattermostmodel.Dialog{
			CallbackId:       dialog.CallbackID,
			Title:            dialog.Title,
			IntroductionText: dialog.IntroductionText,
			Elements:         elements,
			SubmitLabel:      dialog.SubmitLabel,
			State:            dialog.State,
		},
	})
	if err != nil {
		return fmt.Errorf("open Mattermost dialog: %w", err)
	}
	return nil
}

func flowCardPost(card statusservice.FlowCard) *mattermostmodel.Post {
	post := &mattermostmodel.Post{
		Id:        card.PostID,
		ChannelId: card.ChannelID,
		Message:   card.Message,
	}
	post.SetProps(mattermostmodel.StringInterface{
		"attachments": []*mattermostmodel.MessageAttachment{
			flowCardAttachment(card),
		},
	})
	return post
}

func flowCardAttachment(card statusservice.FlowCard) *mattermostmodel.MessageAttachment {
	fields := make([]*mattermostmodel.MessageAttachmentField, 0, len(card.Fields))
	for _, field := range card.Fields {
		fields = append(fields, &mattermostmodel.MessageAttachmentField{
			Title: field.Title,
			Value: field.Value,
			Short: mattermostmodel.SlackCompatibleBool(field.Short),
		})
	}
	actions := make([]*mattermostmodel.PostAction, 0, len(card.Actions))
	for _, action := range card.Actions {
		actions = append(actions, &mattermostmodel.PostAction{
			Id:       action.ID,
			Type:     mattermostmodel.PostActionTypeButton,
			Name:     action.Name,
			Tooltip:  action.Tooltip,
			Style:    action.Style,
			Disabled: action.Disabled,
			Integration: &mattermostmodel.PostActionIntegration{
				URL:     card.ActionURL,
				Context: action.Context,
			},
		})
	}
	return &mattermostmodel.MessageAttachment{
		Fallback: card.Title,
		Color:    card.Color,
		Title:    card.Title,
		Text:     card.Text,
		Fields:   fields,
		Actions:  actions,
	}
}
