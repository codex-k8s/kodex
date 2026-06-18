package service

import "context"

type MattermostRoleBotInput struct {
	Username    string
	DisplayName string
	Description string
}

type MattermostRoleBotBinding struct {
	UserID      string
	Username    string
	DisplayName string
	Token       string
}

type MattermostRoleBotManager interface {
	EnsureRoleBot(ctx context.Context, input MattermostRoleBotInput) (MattermostRoleBotBinding, error)
	EnsureProjectChannelMember(ctx context.Context, teamName string, channelID string, userID string) error
}
