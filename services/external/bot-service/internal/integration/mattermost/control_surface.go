package mattermost

import (
	"context"
	"fmt"

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
