package mattermost

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	domainmattermost "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/mattermost"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
	"github.com/gorilla/websocket"
	model "github.com/mattermost/mattermost/server/public/model"
)

type Config struct {
	SiteURL               string
	TLSServerName         string
	CAFile                string
	ClientCertificateFile string
	ClientPrivateKeyFile  string
	MappingManifestFile   string
	RequestTimeout        time.Duration
	MaximumFileBytes      int64
	CatchUpWindow         time.Duration
}

type botClient struct {
	identity BotIdentity
	api      *model.Client4
	token    string
}

type Client struct {
	config       Config
	manifest     Manifest
	index        *index
	bots         map[string]*botClient
	primary      *botClient
	dialer       *websocket.Dialer
	websocketURL string
}

type cardPayload struct {
	Message string                `json:"message"`
	Props   model.StringInterface `json:"props,omitempty"`
}

func New(config Config) (*Client, error) {
	parsed, err := url.Parse(config.SiteURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != config.TLSServerName ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" ||
		config.RequestTimeout < time.Second || config.RequestTimeout > 30*time.Second ||
		config.MaximumFileBytes < 1<<20 || config.MaximumFileBytes > 256<<20 ||
		config.CatchUpWindow < time.Minute || config.CatchUpWindow > 24*time.Hour {
		return nil, errors.New("mattermost client configuration is invalid")
	}
	manifest, manifestIndex, err := loadManifest(config.MappingManifestFile)
	if err != nil {
		return nil, err
	}
	certificate, err := tls.LoadX509KeyPair(config.ClientCertificateFile, config.ClientPrivateKeyFile)
	if err != nil {
		return nil, errors.New("load Mattermost client identity")
	}
	caRaw, err := os.ReadFile(config.CAFile)
	if err != nil || len(caRaw) == 0 || len(caRaw) > 1<<20 {
		return nil, errors.New("read Mattermost CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("parse Mattermost CA")
	}
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS13, ServerName: config.TLSServerName,
		RootCAs: roots, Certificates: []tls.Certificate{certificate},
	}
	transport := &http.Transport{
		TLSClientConfig: tlsConfig, ForceAttemptHTTP2: true, MaxIdleConns: 32,
		MaxIdleConnsPerHost: 16, IdleConnTimeout: 30 * time.Second,
		ResponseHeaderTimeout: config.RequestTimeout,
	}
	result := &Client{
		config: config, manifest: manifest, index: manifestIndex,
		bots:         make(map[string]*botClient, len(manifest.Bots)),
		dialer:       &websocket.Dialer{TLSClientConfig: tlsConfig, HandshakeTimeout: config.RequestTimeout},
		websocketURL: "wss://" + parsed.Host,
	}
	for _, identity := range manifest.Bots {
		token, tokenErr := readToken(identity.TokenFile)
		if tokenErr != nil {
			return nil, tokenErr
		}
		api := model.NewAPIv4Client(config.SiteURL)
		api.AuthToken = token
		api.AuthType = model.HeaderBearer
		api.HTTPClient = &http.Client{Transport: transport, Timeout: config.RequestTimeout}
		configured := &botClient{identity: identity, api: api, token: token}
		result.bots[identity.StableKey] = configured
		if result.primary == nil {
			result.primary = configured
		}
	}
	return result, nil
}

func (client *Client) Check(ctx context.Context) error {
	for _, bot := range client.bots {
		user, _, err := bot.api.GetMe(ctx, "")
		if err != nil || user == nil || user.Id != bot.identity.UserID || !user.IsBot || user.DeleteAt != 0 {
			return errors.New("mattermost bot identity is not ready")
		}
		for _, binding := range client.manifest.Channels {
			allowed := binding.BotStableKey == bot.identity.StableKey
			for _, assignment := range binding.Assignments {
				allowed = allowed || assignment.BotStableKey == bot.identity.StableKey
			}
			if !allowed {
				continue
			}
			channel, _, channelErr := bot.api.GetChannel(ctx, binding.ChannelID)
			if channelErr != nil || channel == nil || channel.Id != binding.ChannelID ||
				channel.TeamId != binding.TeamID || channel.DeleteAt != 0 {
				return errors.New("mattermost mapped channel is not ready")
			}
			member, _, memberErr := bot.api.GetChannelMember(ctx, binding.ChannelID, bot.identity.UserID, "")
			if memberErr != nil || member == nil || member.ChannelId != binding.ChannelID || member.UserId != bot.identity.UserID {
				return errors.New("mattermost bot channel binding is not ready")
			}
		}
	}
	for _, binding := range client.manifest.Channels {
		member, _, err := client.primary.api.GetChannelMember(ctx, binding.ChannelID, client.primary.identity.UserID, "")
		if err != nil || member == nil || member.ChannelId != binding.ChannelID ||
			member.UserId != client.primary.identity.UserID {
			return errors.New("mattermost primary readback bot channel binding is not ready")
		}
	}
	return nil
}

func (client *Client) ResolveInbound(ctx context.Context, raw domainmattermost.RawEvent) (entity.Boundary, domainmattermost.RawEvent, error) {
	if raw.Kind == "CHANNEL_DELETE" || raw.Kind == "CHANNEL_RESTORE" ||
		raw.Kind == "THREAD_DELETE" || raw.Kind == "THREAD_RESTORE" || raw.Kind == "THREAD_RESTORE_CANDIDATE" {
		return client.resolveLifecycle(ctx, raw)
	}
	if invalidProviderID(raw.UserID) || invalidProviderID(raw.ChannelID) || raw.Revision == 0 {
		return entity.Boundary{}, raw, errors.New("mattermost inbound identity is invalid")
	}
	if raw.PostID != "" {
		post, _, err := client.primary.api.GetPost(ctx, raw.PostID, "")
		if err != nil || post == nil || post.Id != raw.PostID || post.ChannelId != raw.ChannelID ||
			(raw.Kind == "POST" && post.UserId != raw.UserID) {
			return entity.Boundary{}, raw, errors.New("mattermost post readback mismatch")
		}
		if raw.Kind == "POST" {
			raw.Text, raw.FileIDs = post.Message, append([]string(nil), post.FileIds...)
		}
		raw.RootPostID = post.RootId
		if revision := max(post.CreateAt, post.UpdateAt); raw.Kind == "POST" && revision > 0 {
			raw.Revision = uint64(revision)
			raw.Cursor = post.CreateAt
			raw.ProviderEventID = fmt.Sprintf("post:%s:%d", post.Id, revision)
		}
	}
	channel, _, err := client.primary.api.GetChannel(ctx, raw.ChannelID)
	if err != nil || channel == nil || channel.Id != raw.ChannelID || channel.TeamId == "" ||
		channel.DeleteAt != 0 || (raw.TeamID != "" && raw.TeamID != channel.TeamId) {
		return entity.Boundary{}, raw, errors.New("mattermost channel readback mismatch")
	}
	raw.TeamID = channel.TeamId
	team, _, err := client.primary.api.GetTeam(ctx, channel.TeamId, "")
	if err != nil || team == nil || team.Id != channel.TeamId || team.DeleteAt != 0 {
		return entity.Boundary{}, raw, errors.New("mattermost team readback mismatch")
	}
	user, _, err := client.primary.api.GetUser(ctx, raw.UserID, "")
	if err != nil || user == nil || user.Id != raw.UserID || user.DeleteAt != 0 {
		return entity.Boundary{}, raw, errors.New("mattermost user readback mismatch")
	}
	raw.Verified = true
	boundary, err := client.index.resolve(channel.TeamId, channel.Id, user.Id, user.IsBot)
	if err != nil || boundary.IgnoredBot {
		return boundary, raw, err
	}
	mentions := explicitMentions(raw.Text)
	if len(mentions) > 1 {
		return entity.Boundary{}, raw, errors.New("mattermost agent assignment is ambiguous")
	}
	if len(mentions) == 1 {
		mentioned, _, readErr := client.primary.api.GetUserByUsername(ctx, mentions[0], "")
		if readErr != nil || mentioned == nil || mentioned.Id == "" || mentioned.DeleteAt != 0 {
			return entity.Boundary{}, raw, errors.New("mattermost mentioned identity is unknown")
		}
		assignment, assignmentErr := client.index.resolveAssignment(channel.TeamId, channel.Id, mentions[0], mentioned.Id)
		if assignmentErr != nil {
			return entity.Boundary{}, raw, assignmentErr
		}
		boundary.RoleID, boundary.BotStableKey = assignment.RoleID, assignment.BotStableKey
	}
	return boundary, raw, err
}

func (client *Client) resolveLifecycle(ctx context.Context, raw domainmattermost.RawEvent) (entity.Boundary, domainmattermost.RawEvent, error) {
	if invalidProviderID(raw.ChannelID) || raw.Revision == 0 {
		return entity.Boundary{}, raw, errors.New("mattermost lifecycle event is invalid")
	}
	channel, _, err := client.primary.api.GetChannel(ctx, raw.ChannelID)
	if err != nil || channel == nil || channel.Id != raw.ChannelID || channel.TeamId == "" {
		return entity.Boundary{}, raw, errors.New("mattermost lifecycle channel readback mismatch")
	}
	deleting := raw.Kind == "CHANNEL_DELETE" || raw.Kind == "THREAD_DELETE"
	if raw.Kind == "CHANNEL_DELETE" || raw.Kind == "CHANNEL_RESTORE" {
		if (deleting && channel.DeleteAt == 0) || (!deleting && channel.DeleteAt != 0) {
			return entity.Boundary{}, raw, errors.New("mattermost channel lifecycle state mismatch")
		}
		raw.DeleteAt = channel.DeleteAt
		raw.Revision = uint64(max(channel.UpdateAt, channel.DeleteAt))
		raw.ProviderEventID = fmt.Sprintf("%s:%s:%d", strings.ToLower(raw.Kind), channel.Id, raw.Revision)
	}
	if raw.Kind == "THREAD_DELETE" || raw.Kind == "THREAD_RESTORE" || raw.Kind == "THREAD_RESTORE_CANDIDATE" {
		if invalidProviderID(raw.PostID) {
			return entity.Boundary{}, raw, errors.New("mattermost thread lifecycle reference is invalid")
		}
		post, _, postErr := client.primary.api.GetPost(ctx, raw.PostID, "")
		if postErr != nil || post == nil || post.Id != raw.PostID || post.ChannelId != raw.ChannelID || post.RootId != "" ||
			(deleting && post.DeleteAt == 0) || (!deleting && post.DeleteAt != 0) {
			return entity.Boundary{}, raw, errors.New("mattermost thread lifecycle readback mismatch")
		}
		raw.RootPostID, raw.DeleteAt, raw.UserID = post.Id, post.DeleteAt, post.UserId
		raw.Revision = uint64(max(post.UpdateAt, post.DeleteAt))
		raw.ProviderEventID = fmt.Sprintf("%s:%s:%d", strings.ToLower(raw.Kind), post.Id, raw.Revision)
	}
	binding, ok := client.index.channels[channel.TeamId+"\x00"+channel.Id]
	if !ok {
		return entity.Boundary{}, raw, errors.New("mattermost lifecycle event is outside the server-owned mapping")
	}
	raw.TeamID, raw.Verified = channel.TeamId, true
	if raw.Kind != "THREAD_RESTORE_CANDIDATE" {
		raw.UserID = client.primary.identity.UserID
	}
	return entity.Boundary{OrganizationID: binding.OrganizationID, ProjectID: binding.ProjectID,
		ChatID: binding.ChatID, ActorID: binding.LifecycleActorID, RoleID: binding.RoleID,
		Locale: binding.Locale, BotStableKey: binding.BotStableKey, TeamID: channel.TeamId,
		ChannelID: channel.Id, SessionID: binding.SessionID}, raw, nil
}

func (client *Client) ResolveDelivery(projectID, actorID string) (entity.Boundary, error) {
	return client.index.resolveDelivery(projectID, actorID)
}

func (client *Client) DownloadFile(ctx context.Context, channelID, fileID string) ([]byte, string, string, error) {
	if invalidProviderID(channelID) || invalidProviderID(fileID) {
		return nil, "", "", errors.New("mattermost file reference is invalid")
	}
	info, _, err := client.primary.api.GetFileInfo(ctx, fileID)
	if err != nil || info == nil || info.Id != fileID || info.ChannelId != channelID || info.DeleteAt != 0 ||
		info.Size <= 0 || info.Size > client.config.MaximumFileBytes || len(info.Name) == 0 || len(info.Name) > 255 {
		return nil, "", "", errors.New("mattermost file metadata mismatch")
	}
	raw, _, err := client.primary.api.GetFile(ctx, fileID)
	if err != nil || len(raw) == 0 || int64(len(raw)) != info.Size || int64(len(raw)) > client.config.MaximumFileBytes {
		return nil, "", "", errors.New("mattermost file body mismatch")
	}
	mediaType := info.MimeType
	if mediaType == "" || !strings.Contains(mediaType, "/") {
		mediaType = http.DetectContentType(raw[:min(len(raw), 512)])
	}
	return raw, filepath.Base(info.Name), mediaType, nil
}

func (client *Client) UploadFile(ctx context.Context, delivery entity.Delivery, binding entity.ArtifactBinding, raw []byte) (string, error) {
	bot, ok := client.bots[delivery.BotStableKey]
	if !ok || invalidProviderID(delivery.ChannelID) || binding.SizeBytes == 0 ||
		binding.SizeBytes > uint64(client.config.MaximumFileBytes) || uint64(len(raw)) != binding.SizeBytes ||
		digest(raw) != binding.SHA256 || binding.MediaType == "" || binding.Name == "" {
		return "", errors.New("mattermost delivery attachment mismatch")
	}
	uploaded, _, err := bot.api.UploadFile(ctx, raw, delivery.ChannelID, binding.Name)
	if err != nil || uploaded == nil || len(uploaded.FileInfos) != 1 || uploaded.FileInfos[0].Id == "" {
		return "", errors.New("mattermost file upload failed")
	}
	fileID := uploaded.FileInfos[0].Id
	info, _, err := bot.api.GetFileInfo(ctx, fileID)
	if err != nil || info == nil || info.Id != fileID || info.ChannelId != delivery.ChannelID || info.DeleteAt != 0 ||
		filepath.Base(info.Name) != binding.Name || uint64(info.Size) != binding.SizeBytes || info.MimeType != binding.MediaType {
		return "", errors.New("mattermost file upload readback mismatch")
	}
	readback, _, err := bot.api.GetFile(ctx, fileID)
	if err != nil || uint64(len(readback)) != binding.SizeBytes || digest(readback) != binding.SHA256 {
		return "", errors.New("mattermost file upload body mismatch")
	}
	return fileID, nil
}

func (client *Client) Publish(ctx context.Context, delivery entity.Delivery, fileIDs []string) (domainmattermost.Published, error) {
	bot, ok := client.bots[delivery.BotStableKey]
	if !ok || invalidProviderID(delivery.ChannelID) || delivery.ID == "" || len(fileIDs) != len(delivery.Attachments) {
		return domainmattermost.Published{}, errors.New("mattermost delivery boundary is invalid")
	}
	var payload cardPayload
	if json.Unmarshal(delivery.Payload, &payload) != nil || payload.Message == "" {
		return domainmattermost.Published{}, errors.New("mattermost card payload is invalid")
	}
	if delivery.UpdatePostID != "" {
		post := &model.Post{Id: delivery.UpdatePostID, ChannelId: delivery.ChannelID,
			RootId: delivery.RootPostID, Message: payload.Message, Props: payload.Props,
			FileIds: model.StringArray(fileIDs)}
		updated, _, updateErr := bot.api.UpdatePost(ctx, delivery.UpdatePostID, post)
		if updateErr != nil || updated == nil || updated.Id != delivery.UpdatePostID {
			readback, _, readErr := bot.api.GetPost(ctx, delivery.UpdatePostID, "")
			if readErr != nil || readback == nil || !client.exactPending(ctx, bot, readback, delivery, payload, fileIDs) {
				return domainmattermost.Published{}, errors.New("mattermost post update failed")
			}
			return publishedReceipt(readback, delivery.PayloadSHA256)
		}
		readback, _, readErr := bot.api.GetPost(ctx, delivery.UpdatePostID, "")
		if readErr != nil || readback == nil || !client.exactPending(ctx, bot, readback, delivery, payload, fileIDs) {
			return domainmattermost.Published{}, errors.New("mattermost post update readback mismatch")
		}
		return publishedReceipt(readback, delivery.PayloadSHA256)
	}
	found, foundExact, err := client.findPending(ctx, bot, delivery, payload, fileIDs)
	if err != nil {
		return domainmattermost.Published{}, err
	}
	if foundExact {
		return publishedReceipt(found, delivery.PayloadSHA256)
	}
	post := &model.Post{
		ChannelId: delivery.ChannelID, RootId: delivery.RootPostID, Message: payload.Message,
		Props: payload.Props, FileIds: model.StringArray(fileIDs), PendingPostId: delivery.ID,
	}
	created, _, err := bot.api.CreatePost(ctx, post)
	if err != nil {
		found, foundExact, findErr := client.findPending(ctx, bot, delivery, payload, fileIDs)
		if findErr != nil {
			return domainmattermost.Published{}, findErr
		}
		if foundExact {
			return publishedReceipt(found, delivery.PayloadSHA256)
		}
		return domainmattermost.Published{}, errors.New("mattermost post delivery failed")
	}
	if created == nil || created.Id == "" {
		return domainmattermost.Published{}, errors.New("mattermost post receipt mismatch")
	}
	readback, _, err := bot.api.GetPost(ctx, created.Id, "")
	if err != nil || readback == nil || readback.Id != created.Id || !client.exactPending(ctx, bot, readback, delivery, payload, fileIDs) {
		return domainmattermost.Published{}, errors.New("mattermost post readback mismatch")
	}
	return publishedReceipt(readback, delivery.PayloadSHA256)
}

func (client *Client) OpenDecisionDialog(ctx context.Context, botStableKey, triggerID, callbackURL, state, locale string) error {
	bot, ok := client.bots[botStableKey]
	if !ok || triggerID == "" || state == "" {
		return errors.New("mattermost dialog boundary is invalid")
	}
	title, introduction, label, field := "Owner decision", "Record a bounded reason for the decision.", "Submit", "Reason"
	approve, reject, changes, cancel := "Approve", "Reject", "Request changes", "Cancel"
	if locale == "ru" {
		title, introduction, label, field = "Решение владельца", "Укажите ограниченное обоснование решения.", "Отправить", "Причина"
		approve, reject, changes, cancel = "Одобрить", "Отклонить", "Запросить изменения", "Отменить"
	}
	_, err := bot.api.OpenInteractiveDialog(ctx, model.OpenDialogRequest{
		TriggerId: triggerID, URL: callbackURL,
		Dialog: model.Dialog{
			CallbackId: "owner-decision", Title: title, IntroductionText: introduction,
			SubmitLabel: label, NotifyOnCancel: true, State: state,
			Elements: []model.DialogElement{
				{DisplayName: title, Name: "decision", Type: "select", Options: []*model.PostActionOptions{
					{Text: approve, Value: "APPROVE"}, {Text: reject, Value: "REJECT"},
					{Text: changes, Value: "CHANGES_REQUESTED"}, {Text: cancel, Value: "CANCEL"},
				}},
				{DisplayName: field, Name: "reason", Type: "textarea", MinLength: 1, MaxLength: 2048},
			},
		},
	})
	if err != nil {
		return errors.New("open Mattermost decision dialog")
	}
	return nil
}

func (client *Client) findPending(ctx context.Context, bot *botClient, delivery entity.Delivery,
	payload cardPayload, fileIDs []string) (*model.Post, bool, error) {
	since := delivery.CreatedAt.Add(-client.config.CatchUpWindow).UnixMilli()
	posts, _, err := bot.api.GetPostsSince(ctx, delivery.ChannelID, since, false)
	if err != nil || posts == nil {
		return nil, false, errors.New("mattermost pending delivery lookup failed")
	}
	for _, post := range posts.Posts {
		if post != nil && post.PendingPostId == delivery.ID && post.ChannelId == delivery.ChannelID {
			if !client.exactPending(ctx, bot, post, delivery, payload, fileIDs) {
				return nil, false, errors.New("mattermost pending delivery readback mismatch")
			}
			return post, true, nil
		}
	}
	return nil, false, nil
}

func (client *Client) exactPending(ctx context.Context, bot *botClient, post *model.Post,
	delivery entity.Delivery, payload cardPayload, expectedFileIDs []string) bool {
	if post.ChannelId != delivery.ChannelID || post.UserId != bot.identity.UserID ||
		post.RootId != delivery.RootPostID || post.Message != payload.Message ||
		!slices.Equal([]string(post.FileIds), expectedFileIDs) || len(post.FileIds) != len(delivery.Attachments) ||
		!exactProps(post.Props, payload.Props) {
		return false
	}
	if (delivery.UpdatePostID == "" && post.PendingPostId != delivery.ID) ||
		(delivery.UpdatePostID != "" && post.Id != delivery.UpdatePostID) {
		return false
	}
	for index, fileID := range post.FileIds {
		binding := delivery.Attachments[index]
		info, _, err := bot.api.GetFileInfo(ctx, fileID)
		if err != nil || info == nil || info.Id != fileID || info.ChannelId != delivery.ChannelID ||
			info.DeleteAt != 0 || filepath.Base(info.Name) != binding.Name || uint64(info.Size) != binding.SizeBytes ||
			info.MimeType != binding.MediaType {
			return false
		}
		raw, _, err := bot.api.GetFile(ctx, fileID)
		if err != nil || uint64(len(raw)) != binding.SizeBytes || digest(raw) != binding.SHA256 {
			return false
		}
	}
	return true
}

func exactProps(actual, expected model.StringInterface) bool {
	projected := make(model.StringInterface, len(expected))
	for key, value := range actual {
		if _, ok := expected[key]; ok {
			projected[key] = value
			continue
		}
		if key != "from_bot" || value != "true" {
			return false
		}
	}
	if len(projected) != len(expected) {
		return false
	}
	actualRaw, actualErr := json.Marshal(projected)
	expectedRaw, expectedErr := json.Marshal(expected)
	return actualErr == nil && expectedErr == nil && string(actualRaw) == string(expectedRaw)
}

func (client *Client) CatchUp(ctx context.Context, cursors map[string]int64, reactionPosts map[string]string,
	consume func(context.Context, domainmattermost.RawEvent) error) error {
	for _, boundary := range client.ChannelBoundaries() {
		channelID := boundary.ChannelID
		since := cursors[channelID]
		if since == 0 {
			since = time.Now().Add(-client.config.CatchUpWindow).UnixMilli()
		}
		posts, _, err := client.primary.api.GetPostsSince(ctx, channelID, since, false)
		if err != nil {
			return errors.New("mattermost catch-up failed")
		}
		posts.SortByCreateAt()
		for _, post := range posts.ToSlice() {
			if post == nil || post.DeleteAt != 0 {
				continue
			}
			raw := postRaw(post)
			if err := consume(ctx, raw); err != nil {
				return err
			}
		}
	}
	postIDs := make([]string, 0, len(reactionPosts))
	for postID := range reactionPosts {
		postIDs = append(postIDs, postID)
	}
	sort.Strings(postIDs)
	for _, postID := range postIDs {
		channelID := reactionPosts[postID]
		reactions, _, err := client.primary.api.GetReactions(ctx, postID)
		if err != nil {
			return errors.New("mattermost reaction catch-up failed")
		}
		sort.SliceStable(reactions, func(left, right int) bool {
			if reactions[left] == nil {
				return false
			}
			if reactions[right] == nil {
				return true
			}
			return reactions[left].CreateAt < reactions[right].CreateAt
		})
		for _, reaction := range reactions {
			raw, ok := reactionRaw(reaction, channelID, "reaction_added")
			if !ok {
				continue
			}
			if err := consume(ctx, raw); err != nil {
				return err
			}
		}
	}
	return nil
}

func (client *Client) Listen(ctx context.Context, consume func(context.Context, domainmattermost.RawEvent) error) error {
	websocketClient, err := model.NewWebSocketClient4WithDialer(client.dialer, client.websocketURL, client.primary.token)
	if err != nil {
		return errors.New("connect Mattermost WebSocket")
	}
	defer websocketClient.Close()
	websocketClient.Listen()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case _, ok := <-websocketClient.ResponseChannel:
			if !ok {
				return errors.New("mattermost WebSocket response channel closed")
			}
		case _, ok := <-websocketClient.PingTimeoutChannel:
			if ok {
				return errors.New("mattermost WebSocket ping timed out")
			}
		case event, ok := <-websocketClient.EventChannel:
			if !ok {
				return errors.New("mattermost WebSocket event channel closed")
			}
			raw, relevant := decodeEvent(event)
			if relevant {
				if err := consume(ctx, raw); err != nil {
					return err
				}
			}
		}
	}
}

func (client *Client) ChannelBoundaries() []entity.Boundary { return client.index.channelBoundaries() }

func (client *Client) ReadinessBoundary() (entity.Boundary, error) {
	for _, binding := range client.manifest.Channels {
		return entity.Boundary{OrganizationID: binding.OrganizationID, ProjectID: binding.ProjectID,
			ChatID: binding.ChatID, ActorID: binding.LifecycleActorID, RoleID: binding.RoleID,
			Locale: binding.Locale, BotStableKey: binding.BotStableKey, TeamID: binding.TeamID,
			ChannelID: binding.ChannelID, SessionID: binding.SessionID}, nil
	}
	return entity.Boundary{}, errors.New("mattermost readiness boundary is unavailable")
}

func decodeEvent(event *model.WebSocketEvent) (domainmattermost.RawEvent, bool) {
	if event == nil {
		return domainmattermost.RawEvent{}, false
	}
	switch event.EventType() {
	case model.WebsocketEventPosted, model.WebsocketEventPostEdited:
		var post model.Post
		if !decodeData(event.GetData()["post"], &post) || post.Id == "" || max(post.CreateAt, post.UpdateAt) <= 0 {
			return domainmattermost.RawEvent{}, false
		}
		raw := postRaw(&post)
		if event.EventType() == model.WebsocketEventPostEdited && post.RootId == "" {
			raw.Kind = "THREAD_RESTORE_CANDIDATE"
		}
		return raw, true
	case model.WebsocketEventPostDeleted:
		var post model.Post
		if !decodeData(event.GetData()["post"], &post) || post.Id == "" || post.RootId != "" || post.DeleteAt <= 0 {
			return domainmattermost.RawEvent{}, false
		}
		raw := postRaw(&post)
		raw.Kind, raw.DeleteAt = "THREAD_DELETE", post.DeleteAt
		return raw, true
	case model.WebsocketEventChannelDeleted, model.WebsocketEventChannelRestored:
		channelID := ""
		if value, ok := event.GetData()["channel_id"].(string); ok {
			channelID = value
		}
		if channelID == "" && event.GetBroadcast() != nil {
			channelID = event.GetBroadcast().ChannelId
		}
		if invalidProviderID(channelID) || event.GetSequence() <= 0 {
			return domainmattermost.RawEvent{}, false
		}
		kind := "CHANNEL_DELETE"
		if event.EventType() == model.WebsocketEventChannelRestored {
			kind = "CHANNEL_RESTORE"
		}
		return domainmattermost.RawEvent{ProviderEventID: fmt.Sprintf("%s:%s:%d", strings.ToLower(kind), channelID, event.GetSequence()),
			Kind: kind, Revision: uint64(event.GetSequence()), ChannelID: channelID}, true
	case model.WebsocketEventReactionAdded:
		var reaction model.Reaction
		if !decodeData(event.GetData()["reaction"], &reaction) || reaction.PostId == "" {
			return domainmattermost.RawEvent{}, false
		}
		channelID := reaction.ChannelId
		if channelID == "" && event.GetBroadcast() != nil {
			channelID = event.GetBroadcast().ChannelId
		}
		return reactionRaw(&reaction, channelID, string(event.EventType()))
	default:
		return domainmattermost.RawEvent{}, false
	}
}

func reactionRaw(reaction *model.Reaction, channelID, source string) (domainmattermost.RawEvent, bool) {
	if reaction == nil || invalidProviderID(reaction.PostId) || invalidProviderID(reaction.UserId) ||
		invalidProviderID(channelID) || reaction.EmojiName == "" {
		return domainmattermost.RawEvent{}, false
	}
	revision := max(reaction.CreateAt, reaction.UpdateAt, reaction.DeleteAt)
	if revision <= 0 {
		return domainmattermost.RawEvent{}, false
	}
	return domainmattermost.RawEvent{
		ProviderEventID: fmt.Sprintf("%s:%s:%s:%s:%d", source, reaction.PostId, reaction.UserId, reaction.EmojiName, revision),
		Kind:            "REACTION", Revision: uint64(revision), ChannelID: channelID,
		PostID: reaction.PostId, UserID: reaction.UserId, Text: reaction.EmojiName,
	}, true
}

func postRaw(post *model.Post) domainmattermost.RawEvent {
	revision := max(post.CreateAt, post.UpdateAt)
	return domainmattermost.RawEvent{
		ProviderEventID: fmt.Sprintf("post:%s:%d", post.Id, revision), Kind: "POST",
		Revision: uint64(revision), Cursor: post.CreateAt, ChannelID: post.ChannelId, PostID: post.Id,
		RootPostID: post.RootId, UserID: post.UserId, Text: post.Message, FileIDs: append([]string(nil), post.FileIds...),
		DeleteAt: post.DeleteAt,
	}
}

func decodeData(value any, target any) bool {
	var raw []byte
	switch typed := value.(type) {
	case string:
		raw = []byte(typed)
	default:
		raw, _ = json.Marshal(value)
	}
	return len(raw) > 0 && json.Unmarshal(raw, target) == nil
}

func publishedReceipt(post *model.Post, payloadDigest string) (domainmattermost.Published, error) {
	raw, err := json.Marshal(struct {
		PostID, ChannelID, RootPostID, PendingPostID, UserID, PayloadSHA256 string
		FileIDs                                                             []string
		UpdateAt                                                            int64
	}{post.Id, post.ChannelId, post.RootId, post.PendingPostId, post.UserId, payloadDigest,
		append([]string(nil), post.FileIds...), post.UpdateAt})
	if err != nil {
		return domainmattermost.Published{}, errors.New("encode Mattermost receipt")
	}
	return domainmattermost.Published{
		PostID: post.Id, ChannelID: post.ChannelId, RootPostID: rootID(post), ReceiptSHA256: digest(raw),
	}, nil
}

func rootID(post *model.Post) string {
	if post.RootId != "" {
		return post.RootId
	}
	return post.Id
}

func digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func readToken(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("mattermost token path is not absolute")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 16 || info.Size() > 16<<10 || info.Mode().Perm()&0o037 != 0 {
		return "", errors.New("mattermost token file is unsafe")
	}
	raw, err := os.ReadFile(path)
	value := strings.TrimSpace(string(raw))
	if err != nil || len(value) < 16 || strings.ContainsAny(value, "\r\n\x00") {
		return "", errors.New("read Mattermost token")
	}
	return value, nil
}

func explicitMentions(text string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, 2)
	for index := 0; index < len(text); index++ {
		if text[index] != '@' || (index > 0 && (text[index-1] == '/' || text[index-1] == '@' || isUsernameByte(text[index-1]))) {
			continue
		}
		end := index + 1
		for end < len(text) {
			value := text[end]
			if !isUsernameByte(value) {
				break
			}
			end++
		}
		username := strings.ToLower(text[index+1 : end])
		if invalidUsername(username) {
			continue
		}
		if _, exists := seen[username]; !exists {
			seen[username] = struct{}{}
			result = append(result, username)
		}
		index = end - 1
	}
	sort.Strings(result)
	return result
}

func isUsernameByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9' || value == '.' || value == '_' || value == '-'
}
