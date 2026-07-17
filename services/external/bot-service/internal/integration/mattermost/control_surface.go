package mattermost

import (
	"context"
	"fmt"
	"sort"
	"strings"

	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
	mattermostmodel "github.com/mattermost/mattermost/server/public/model"
)

type ControlSurface struct {
	client      *mattermostmodel.Client4
	adminClient *mattermostmodel.Client4
}

var _ statusservice.MattermostThreadPublisher = (*ControlSurface)(nil)
var _ statusservice.MattermostConversationReader = (*ControlSurface)(nil)
var _ statusservice.MattermostRoleBotManager = (*ControlSurface)(nil)

func NewControlSurface(siteURL string, token string, adminToken string) *ControlSurface {
	client := mattermostmodel.NewAPIv4Client(siteURL)
	client.SetToken(token)
	adminClient := client
	if strings.TrimSpace(adminToken) != "" {
		adminClient = mattermostmodel.NewAPIv4Client(siteURL)
		adminClient.SetToken(adminToken)
	}
	return &ControlSurface{client: client, adminClient: adminClient}
}

func (surface *ControlSurface) BotUserID(ctx context.Context) (string, error) {
	user, _, err := surface.client.GetMe(ctx, "")
	if err != nil {
		return "", fmt.Errorf("get Mattermost bot user: %w", err)
	}
	return user.Id, nil
}

func (surface *ControlSurface) ResolveMattermostUserName(ctx context.Context, userID string) (string, error) {
	user, _, err := surface.client.GetUser(ctx, strings.TrimSpace(userID), "")
	if err != nil {
		return "", fmt.Errorf("get Mattermost user: %w", err)
	}
	if user == nil || strings.TrimSpace(user.Username) == "" {
		return "", fmt.Errorf("get Mattermost user: response has no username")
	}
	return strings.TrimSpace(user.Username), nil
}

func (surface *ControlSurface) ResolveMattermostUserID(ctx context.Context, username string) (string, error) {
	user, _, err := surface.client.GetUserByUsername(ctx, strings.TrimPrefix(strings.TrimSpace(username), "@"), "")
	if err != nil {
		return "", fmt.Errorf("get Mattermost user by username: %w", err)
	}
	if user == nil || strings.TrimSpace(user.Id) == "" {
		return "", fmt.Errorf("get Mattermost user by username: response has no user id")
	}
	return strings.TrimSpace(user.Id), nil
}

func (surface *ControlSurface) EnsureRoleBot(ctx context.Context, input statusservice.MattermostRoleBotInput) (statusservice.MattermostRoleBotBinding, error) {
	username := strings.TrimSpace(input.Username)
	if username == "" {
		return statusservice.MattermostRoleBotBinding{}, fmt.Errorf("Mattermost bot username is required")
	}
	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" {
		displayName = username
	}
	bot, err := surface.findBotByUsername(ctx, username)
	if err != nil {
		return statusservice.MattermostRoleBotBinding{}, err
	}
	if bot == nil {
		user, response, userErr := surface.adminClient.GetUserByUsername(ctx, username, "")
		if userErr == nil {
			if user == nil || strings.TrimSpace(user.Id) == "" {
				return statusservice.MattermostRoleBotBinding{}, fmt.Errorf("get Mattermost role user: response has no user")
			}
			bot, _, err = surface.adminClient.ConvertUserToBot(ctx, user.Id)
			if err != nil {
				return statusservice.MattermostRoleBotBinding{}, fmt.Errorf("convert Mattermost role user to bot: %w", err)
			}
		} else {
			if response == nil || response.StatusCode != 404 {
				return statusservice.MattermostRoleBotBinding{}, fmt.Errorf("get Mattermost role user: %w", userErr)
			}
			bot, _, err = surface.adminClient.CreateBot(ctx, &mattermostmodel.Bot{
				Username:    username,
				DisplayName: displayName,
				Description: strings.TrimSpace(input.Description),
			})
			if err != nil {
				return statusservice.MattermostRoleBotBinding{}, fmt.Errorf("create Mattermost role bot: %w", err)
			}
		}
	}
	if bot == nil || strings.TrimSpace(bot.UserId) == "" {
		return statusservice.MattermostRoleBotBinding{}, fmt.Errorf("ensure Mattermost role bot: response has no bot user")
	}
	return surface.roleBindingForUser(ctx, bot.UserId, bot.Username, bot.DisplayName)
}

func (surface *ControlSurface) EnsureExistingRoleBot(ctx context.Context, userID string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return fmt.Errorf("Mattermost role user id is required")
	}
	if _, response, err := surface.adminClient.GetBot(ctx, userID, ""); err == nil {
		return nil
	} else if response == nil || response.StatusCode != 404 {
		return fmt.Errorf("get Mattermost role bot: %w", err)
	}
	if _, _, err := surface.adminClient.ConvertUserToBot(ctx, userID); err != nil {
		return fmt.Errorf("convert Mattermost role user to bot: %w", err)
	}
	return nil
}

func (surface *ControlSurface) roleBindingForUser(ctx context.Context, userID string, username string, displayName string) (statusservice.MattermostRoleBotBinding, error) {
	token, _, err := surface.adminClient.CreateUserAccessToken(ctx, userID, "matter-codex role identity")
	if err != nil {
		return statusservice.MattermostRoleBotBinding{}, fmt.Errorf("create Mattermost role identity token: %w", err)
	}
	return statusservice.MattermostRoleBotBinding{
		UserID:      userID,
		Username:    username,
		DisplayName: displayName,
		Token:       token.Token,
	}, nil
}

func (surface *ControlSurface) EnsureProjectChannelMember(ctx context.Context, teamName string, channelID string, userID string) error {
	if strings.TrimSpace(userID) == "" {
		return nil
	}
	team, _, err := surface.client.GetTeamByName(ctx, teamName, "")
	if err != nil {
		return fmt.Errorf("get Mattermost team: %w", err)
	}
	if err := surface.ensureTeamMember(ctx, team.Id, userID); err != nil {
		return err
	}
	return surface.ensureChannelMember(ctx, channelID, userID)
}

func (surface *ControlSurface) findBotByUsername(ctx context.Context, username string) (*mattermostmodel.Bot, error) {
	for page := 0; page < 20; page++ {
		bots, _, err := surface.adminClient.GetBotsIncludeDeleted(ctx, page, 200, "")
		if err != nil {
			return nil, fmt.Errorf("list Mattermost bots: %w", err)
		}
		if len(bots) == 0 {
			return nil, nil
		}
		for _, bot := range bots {
			if bot != nil && strings.EqualFold(bot.Username, username) {
				return bot, nil
			}
		}
	}
	return nil, nil
}

func (surface *ControlSurface) PostThreadMessage(ctx context.Context, input statusservice.MattermostThreadPostInput) (statusservice.MattermostPostRef, error) {
	return createThreadPost(ctx, surface.client, input)
}

func (surface *ControlSurface) PostThreadMessageWithToken(ctx context.Context, token string, input statusservice.MattermostThreadPostInput) (statusservice.MattermostPostRef, error) {
	client := mattermostmodel.NewAPIv4Client(surface.client.URL)
	client.SetToken(token)
	return createThreadPost(ctx, client, input)
}

func (surface *ControlSurface) UpdateThreadMessage(ctx context.Context, input statusservice.MattermostThreadUpdateInput) (statusservice.MattermostPostRef, error) {
	return updateThreadPost(ctx, surface.client, input)
}

func (surface *ControlSurface) UpdateThreadMessageWithToken(ctx context.Context, token string, input statusservice.MattermostThreadUpdateInput) (statusservice.MattermostPostRef, error) {
	client := mattermostmodel.NewAPIv4Client(surface.client.URL)
	client.SetToken(token)
	return updateThreadPost(ctx, client, input)
}

func (surface *ControlSurface) PostThreadCard(ctx context.Context, card statusservice.MattermostCard) (statusservice.MattermostPostRef, error) {
	post := mattermostCardPost(card)
	post.RootId = card.RootPostID
	created, _, err := surface.client.CreatePost(ctx, post)
	if err != nil {
		return statusservice.MattermostPostRef{}, fmt.Errorf("create Mattermost thread card: %w", err)
	}
	return statusservice.MattermostPostRef{ChannelID: created.ChannelId, PostID: created.Id}, nil
}

func (surface *ControlSurface) UpdateThreadCard(ctx context.Context, card statusservice.MattermostCard) (statusservice.MattermostPostRef, error) {
	post := mattermostCardPost(card)
	updated, _, err := surface.client.UpdatePost(ctx, card.PostID, post)
	if err != nil {
		return statusservice.MattermostPostRef{}, fmt.Errorf("update Mattermost thread card: %w", err)
	}
	return statusservice.MattermostPostRef{ChannelID: updated.ChannelId, PostID: updated.Id}, nil
}

func (surface *ControlSurface) AddPostReactionWithToken(ctx context.Context, token string, input statusservice.MattermostPostReactionInput) error {
	client := mattermostmodel.NewAPIv4Client(surface.client.URL)
	client.SetToken(token)
	if _, _, err := client.SaveReaction(ctx, &mattermostmodel.Reaction{
		UserId:    input.UserID,
		PostId:    input.PostID,
		EmojiName: input.EmojiName,
	}); err != nil {
		return fmt.Errorf("add Mattermost post reaction: %w", err)
	}
	return nil
}

func (surface *ControlSurface) GetThreadPosts(ctx context.Context, rootPostID string, limit int) ([]statusservice.MattermostPostMessage, error) {
	rootPostID = strings.TrimSpace(rootPostID)
	if rootPostID == "" {
		return nil, fmt.Errorf("Mattermost root post id is required")
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	postList, _, err := surface.client.GetPostThread(ctx, rootPostID, "", false)
	if err != nil {
		return nil, fmt.Errorf("get Mattermost thread posts: %w", err)
	}
	posts := mattermostPostMessages(postList)
	if len(posts) > limit {
		posts = posts[len(posts)-limit:]
	}
	return posts, nil
}

func (surface *ControlSurface) SearchChannelPosts(ctx context.Context, channelID string, query string, limit int) ([]statusservice.MattermostPostMessage, error) {
	channelID = strings.TrimSpace(channelID)
	query = strings.ToLower(strings.TrimSpace(query))
	if channelID == "" {
		return nil, fmt.Errorf("Mattermost channel id is required")
	}
	if query == "" {
		return nil, fmt.Errorf("search query is required")
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	postList, _, err := surface.client.GetPostsForChannel(ctx, channelID, 0, 100, "", false, false)
	if err != nil {
		return nil, fmt.Errorf("get Mattermost channel posts: %w", err)
	}
	posts := mattermostPostMessages(postList)
	matched := make([]statusservice.MattermostPostMessage, 0, limit)
	for index := len(posts) - 1; index >= 0; index-- {
		post := posts[index]
		if strings.Contains(strings.ToLower(post.Message), query) {
			matched = append(matched, post)
			if len(matched) >= limit {
				break
			}
		}
	}
	sort.Slice(matched, func(i int, j int) bool {
		if matched[i].CreateAt == matched[j].CreateAt {
			return matched[i].ID < matched[j].ID
		}
		return matched[i].CreateAt < matched[j].CreateAt
	})
	return matched, nil
}

func createThreadPost(ctx context.Context, client *mattermostmodel.Client4, input statusservice.MattermostThreadPostInput) (statusservice.MattermostPostRef, error) {
	post, _, err := client.CreatePost(ctx, &mattermostmodel.Post{
		ChannelId: input.ChannelID,
		RootId:    input.RootPostID,
		Message:   input.Message,
		Props:     input.Props,
	})
	if err != nil {
		return statusservice.MattermostPostRef{}, fmt.Errorf("create Mattermost thread post: %w", err)
	}
	return statusservice.MattermostPostRef{ChannelID: post.ChannelId, PostID: post.Id}, nil
}

func updateThreadPost(ctx context.Context, client *mattermostmodel.Client4, input statusservice.MattermostThreadUpdateInput) (statusservice.MattermostPostRef, error) {
	post, _, err := client.UpdatePost(ctx, input.PostID, &mattermostmodel.Post{
		Id:        input.PostID,
		ChannelId: input.ChannelID,
		RootId:    input.RootPostID,
		Message:   input.Message,
		Props:     input.Props,
	})
	if err != nil {
		return statusservice.MattermostPostRef{}, fmt.Errorf("update Mattermost thread post: %w", err)
	}
	return statusservice.MattermostPostRef{ChannelID: post.ChannelId, PostID: post.Id}, nil
}

func mattermostPostMessages(postList *mattermostmodel.PostList) []statusservice.MattermostPostMessage {
	if postList == nil || len(postList.Posts) == 0 {
		return nil
	}
	posts := make([]*mattermostmodel.Post, 0, len(postList.Posts))
	for _, post := range postList.Posts {
		if post != nil && strings.TrimSpace(post.Message) != "" {
			posts = append(posts, post)
		}
	}
	sort.Slice(posts, func(i int, j int) bool {
		if posts[i].CreateAt == posts[j].CreateAt {
			return posts[i].Id < posts[j].Id
		}
		return posts[i].CreateAt < posts[j].CreateAt
	})
	result := make([]statusservice.MattermostPostMessage, 0, len(posts))
	for _, post := range posts {
		result = append(result, statusservice.MattermostPostMessage{
			ID:        post.Id,
			RootID:    post.RootId,
			UserID:    post.UserId,
			Message:   truncateMattermostPostMessage(post.Message, 2000),
			CreateAt:  post.CreateAt,
			UpdateAt:  post.UpdateAt,
			ChannelID: post.ChannelId,
		})
	}
	return result
}

func truncateMattermostPostMessage(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if limit <= 0 || len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "\n...[truncated]"
}

func (surface *ControlSurface) EnsureRepositoryChannel(ctx context.Context, teamName string, channelName string, displayName string) (bool, error) {
	_, created, err := surface.EnsureProjectChannel(ctx, teamName, channelName, displayName, false, nil)
	return created, err
}

func (surface *ControlSurface) EnsureProjectTeam(ctx context.Context, teamName string, displayName string, memberUserID string) (statusservice.MattermostTeamBinding, bool, error) {
	memberUserIDs := surface.memberUserIDsWithControlBot(ctx, []string{memberUserID})
	team, response, err := surface.client.GetTeamByName(ctx, teamName, "")
	if err == nil {
		for _, userID := range memberUserIDs {
			_ = surface.ensureTeamMember(ctx, team.Id, userID)
		}
		return mattermostTeamBinding(team), false, nil
	}
	if response == nil || response.StatusCode != 404 {
		return statusservice.MattermostTeamBinding{}, false, fmt.Errorf("get Mattermost team: %w", err)
	}
	created, _, err := surface.client.CreateTeam(ctx, &mattermostmodel.Team{
		Name:        teamName,
		DisplayName: displayName,
		Type:        mattermostmodel.TeamOpen,
	})
	if err != nil {
		return statusservice.MattermostTeamBinding{}, false, fmt.Errorf("create Mattermost team: %w", err)
	}
	for _, userID := range memberUserIDs {
		_ = surface.ensureTeamMember(ctx, created.Id, userID)
	}
	return mattermostTeamBinding(created), true, nil
}

func (surface *ControlSurface) EnsureProjectChannel(ctx context.Context, teamName string, channelName string, displayName string, private bool, memberUserIDs []string) (statusservice.MattermostChannelBinding, bool, error) {
	team, _, err := surface.client.GetTeamByName(ctx, teamName, "")
	if err != nil {
		return statusservice.MattermostChannelBinding{}, false, fmt.Errorf("get Mattermost team: %w", err)
	}
	memberUserIDs = surface.memberUserIDsWithControlBot(ctx, memberUserIDs)
	if channel, response, err := surface.client.GetChannelByName(ctx, channelName, team.Id, ""); err == nil {
		displayName = strings.TrimSpace(displayName)
		if displayName != "" && channel.DisplayName != displayName {
			channel.DisplayName = displayName
			updated, _, updateErr := surface.client.UpdateChannel(ctx, channel)
			if updateErr != nil {
				return statusservice.MattermostChannelBinding{}, false, fmt.Errorf("update Mattermost channel display name: %w", updateErr)
			}
			channel = updated
		}
		for _, userID := range memberUserIDs {
			_ = surface.ensureTeamMember(ctx, team.Id, userID)
			_ = surface.ensureChannelMember(ctx, channel.Id, userID)
		}
		return mattermostChannelBinding(channel), false, nil
	} else if response == nil || response.StatusCode != 404 {
		return statusservice.MattermostChannelBinding{}, false, fmt.Errorf("get Mattermost channel: %w", err)
	}
	channelType := mattermostmodel.ChannelTypeOpen
	if private {
		channelType = mattermostmodel.ChannelTypePrivate
	}
	created, _, err := surface.client.CreateChannel(ctx, &mattermostmodel.Channel{
		TeamId:      team.Id,
		Name:        channelName,
		DisplayName: displayName,
		Type:        channelType,
	})
	if err != nil {
		return statusservice.MattermostChannelBinding{}, false, fmt.Errorf("create Mattermost channel: %w", err)
	}
	for _, userID := range memberUserIDs {
		_ = surface.ensureTeamMember(ctx, team.Id, userID)
		_ = surface.ensureChannelMember(ctx, created.Id, userID)
	}
	return mattermostChannelBinding(created), true, nil
}

func (surface *ControlSurface) memberUserIDsWithControlBot(ctx context.Context, memberUserIDs []string) []string {
	result := make([]string, 0, len(memberUserIDs)+1)
	seen := make(map[string]struct{}, len(memberUserIDs)+1)
	for _, userID := range memberUserIDs {
		userID = strings.TrimSpace(userID)
		if userID == "" {
			continue
		}
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}
		result = append(result, userID)
	}
	if me, _, err := surface.client.GetMe(ctx, ""); err == nil && strings.TrimSpace(me.Id) != "" {
		if _, ok := seen[me.Id]; !ok {
			result = append(result, me.Id)
		}
	}
	return result
}

func (surface *ControlSurface) ensureTeamMember(ctx context.Context, teamID string, userID string) error {
	if userID == "" {
		return nil
	}
	if _, response, err := surface.client.AddTeamMember(ctx, teamID, userID); err != nil {
		if response != nil && (response.StatusCode == 400 || response.StatusCode == 409) {
			return nil
		}
		return fmt.Errorf("add Mattermost team member: %w", err)
	}
	return nil
}

func (surface *ControlSurface) ensureChannelMember(ctx context.Context, channelID string, userID string) error {
	if userID == "" {
		return nil
	}
	if _, response, err := surface.client.AddChannelMember(ctx, channelID, userID); err != nil {
		if response != nil && (response.StatusCode == 400 || response.StatusCode == 409) {
			return nil
		}
		return fmt.Errorf("add Mattermost channel member: %w", err)
	}
	return nil
}

func mattermostTeamBinding(team *mattermostmodel.Team) statusservice.MattermostTeamBinding {
	if team == nil {
		return statusservice.MattermostTeamBinding{}
	}
	return statusservice.MattermostTeamBinding{
		ID:          team.Id,
		Name:        team.Name,
		DisplayName: team.DisplayName,
	}
}

func mattermostChannelBinding(channel *mattermostmodel.Channel) statusservice.MattermostChannelBinding {
	if channel == nil {
		return statusservice.MattermostChannelBinding{}
	}
	return statusservice.MattermostChannelBinding{
		ID:          channel.Id,
		TeamID:      channel.TeamId,
		Name:        channel.Name,
		DisplayName: channel.DisplayName,
		Type:        string(channel.Type),
	}
}

func (surface *ControlSurface) legacyEnsureRepositoryChannel(ctx context.Context, teamName string, channelName string, displayName string) (bool, error) {
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

func mattermostCardPost(card statusservice.MattermostCard) *mattermostmodel.Post {
	post := &mattermostmodel.Post{
		Id:        card.PostID,
		ChannelId: card.ChannelID,
		RootId:    card.RootPostID,
		Message:   card.Message,
	}
	props := mattermostmodel.StringInterface{}
	for key, value := range card.Props {
		props[key] = value
	}
	props["attachments"] = []*mattermostmodel.MessageAttachment{mattermostCardAttachment(card)}
	post.SetProps(props)
	return post
}

func mattermostCardAttachment(card statusservice.MattermostCard) *mattermostmodel.MessageAttachment {
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
