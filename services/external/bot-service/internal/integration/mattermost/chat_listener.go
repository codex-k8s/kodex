package mattermost

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
	mattermostmodel "github.com/mattermost/mattermost/server/public/model"
)

type ChatPostHandler interface {
	HandleChatPost(ctx context.Context, command statusservice.ChatPostCommand) statusservice.ChatRunResult
}

type MattermostUserNameResolver interface {
	ResolveMattermostUserName(ctx context.Context, userID string) (string, error)
}

type ChatListenerConfig struct {
	SiteURL          string
	Token            string
	BotUserID        string
	Handler          ChatPostHandler
	UserNameResolver MattermostUserNameResolver
	Logger           *slog.Logger
}

type ChatListener struct {
	siteURL          string
	token            string
	botUserID        string
	handler          ChatPostHandler
	userNameResolver MattermostUserNameResolver
	userNames        sync.Map
	logger           *slog.Logger
}

func NewChatListener(cfg ChatListenerConfig) (*ChatListener, error) {
	siteURL := strings.TrimRight(strings.TrimSpace(cfg.SiteURL), "/")
	if siteURL == "" {
		return nil, fmt.Errorf("Mattermost site URL is required")
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, fmt.Errorf("Mattermost bot token is required")
	}
	if cfg.Handler == nil {
		return nil, fmt.Errorf("chat post handler is required")
	}
	return &ChatListener{
		siteURL:          siteURL,
		token:            strings.TrimSpace(cfg.Token),
		botUserID:        strings.TrimSpace(cfg.BotUserID),
		handler:          cfg.Handler,
		userNameResolver: cfg.UserNameResolver,
		logger:           cfg.Logger,
	}, nil
}

func (listener *ChatListener) Run(ctx context.Context) {
	for {
		if err := listener.runOnce(ctx); err != nil && ctx.Err() == nil {
			listener.logWarn("Mattermost chat listener stopped", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func (listener *ChatListener) runOnce(ctx context.Context) error {
	websocketURL, err := mattermostWebSocketURL(listener.siteURL)
	if err != nil {
		return err
	}
	client, err := mattermostmodel.NewWebSocketClient4(websocketURL, listener.token)
	if err != nil {
		return fmt.Errorf("open Mattermost websocket: %w", err)
	}
	defer client.Close()
	client.Listen()
	listener.logInfo("Mattermost chat listener connected")
	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-client.EventChannel:
			if !ok {
				if client.ListenError != nil {
					return client.ListenError
				}
				return fmt.Errorf("Mattermost websocket closed")
			}
			listener.handleEvent(ctx, event)
		}
	}
}

func (listener *ChatListener) handleEvent(ctx context.Context, event *mattermostmodel.WebSocketEvent) {
	if event == nil || event.EventType() != mattermostmodel.WebsocketEventPosted {
		return
	}
	post, ok := websocketEventPost(event.GetData())
	if !ok {
		return
	}
	if strings.TrimSpace(post.UserId) == "" || post.UserId == listener.botUserID {
		return
	}
	broadcastChannelID := ""
	if event.GetBroadcast() != nil {
		broadcastChannelID = event.GetBroadcast().ChannelId
	}
	command := statusservice.ChatPostCommand{
		ChannelID:  defaultString(post.ChannelId, broadcastChannelID),
		PostID:     post.Id,
		RootPostID: post.RootId,
		CreateAt:   post.CreateAt,
		UserID:     post.UserId,
		UserName:   listener.resolveUserName(ctx, post.UserId, event.GetData()),
		Message:    post.Message,
		Props:      post.Props,
	}
	if command.ChannelID == "" || command.PostID == "" {
		return
	}
	result := listener.handler.HandleChatPost(ctx, command)
	if !result.Ignored {
		listener.logInfo("Mattermost chat post handled", "channel_id", command.ChannelID, "post_id", command.PostID, "run_id", result.RunID, "mode", result.Mode)
	}
}

func (listener *ChatListener) resolveUserName(ctx context.Context, userID string, data map[string]any) string {
	if senderName, ok := data["sender_name"].(string); ok && strings.TrimSpace(senderName) != "" {
		return strings.TrimSpace(senderName)
	}
	userID = strings.TrimSpace(userID)
	if userID == "" || listener.userNameResolver == nil {
		return ""
	}
	if cached, ok := listener.userNames.Load(userID); ok {
		return cached.(string)
	}
	userName, err := listener.userNameResolver.ResolveMattermostUserName(ctx, userID)
	if err != nil {
		listener.logWarn("Mattermost post sender was not resolved", "user_id", userID, "error", err)
		return ""
	}
	userName = strings.TrimSpace(userName)
	if userName != "" {
		listener.userNames.Store(userID, userName)
	}
	return userName
}

func websocketEventPost(data map[string]any) (*mattermostmodel.Post, bool) {
	raw, ok := data["post"]
	if !ok || raw == nil {
		return nil, false
	}
	var body []byte
	switch value := raw.(type) {
	case string:
		body = []byte(value)
	default:
		var err error
		body, err = json.Marshal(value)
		if err != nil {
			return nil, false
		}
	}
	var post mattermostmodel.Post
	if err := json.Unmarshal(body, &post); err != nil {
		return nil, false
	}
	if strings.TrimSpace(post.Type) != "" {
		return nil, false
	}
	return &post, true
}

func mattermostWebSocketURL(siteURL string) (string, error) {
	parsed, err := url.Parse(siteURL)
	if err != nil {
		return "", fmt.Errorf("parse Mattermost site URL: %w", err)
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("Mattermost site URL scheme must be http, https, ws, or wss")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func (listener *ChatListener) logInfo(message string, args ...any) {
	if listener.logger != nil {
		listener.logger.Info(message, args...)
	}
}

func (listener *ChatListener) logWarn(message string, args ...any) {
	if listener.logger != nil {
		listener.logger.Warn(message, args...)
	}
}

func defaultString(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
