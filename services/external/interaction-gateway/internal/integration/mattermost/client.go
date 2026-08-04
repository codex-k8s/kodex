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
		return nil, errors.New("Mattermost client configuration is invalid")
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
	user, _, err := client.primary.api.GetUser(ctx, client.primary.identity.UserID, "")
	if err != nil || user == nil || user.Id != client.primary.identity.UserID || !user.IsBot {
		return errors.New("Mattermost bot identity is not ready")
	}
	for _, channelID := range client.index.channelIDs() {
		channel, _, channelErr := client.primary.api.GetChannel(ctx, channelID)
		if channelErr != nil || channel == nil || channel.Id != channelID {
			return errors.New("Mattermost mapped channel is not ready")
		}
	}
	return nil
}

func (client *Client) ResolveInbound(ctx context.Context, raw domainmattermost.RawEvent) (entity.Boundary, domainmattermost.RawEvent, error) {
	if invalidProviderID(raw.UserID) || invalidProviderID(raw.ChannelID) || raw.Revision == 0 {
		return entity.Boundary{}, raw, errors.New("Mattermost inbound identity is invalid")
	}
	if raw.PostID != "" {
		post, _, err := client.primary.api.GetPost(ctx, raw.PostID, "")
		if err != nil || post == nil || post.Id != raw.PostID || post.ChannelId != raw.ChannelID ||
			(raw.Kind == "POST" && post.UserId != raw.UserID) {
			return entity.Boundary{}, raw, errors.New("Mattermost post readback mismatch")
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
		(raw.TeamID != "" && raw.TeamID != channel.TeamId) {
		return entity.Boundary{}, raw, errors.New("Mattermost channel readback mismatch")
	}
	raw.TeamID = channel.TeamId
	team, _, err := client.primary.api.GetTeam(ctx, channel.TeamId, "")
	if err != nil || team == nil || team.Id != channel.TeamId || team.DeleteAt != 0 {
		return entity.Boundary{}, raw, errors.New("Mattermost team readback mismatch")
	}
	user, _, err := client.primary.api.GetUser(ctx, raw.UserID, "")
	if err != nil || user == nil || user.Id != raw.UserID || user.DeleteAt != 0 {
		return entity.Boundary{}, raw, errors.New("Mattermost user readback mismatch")
	}
	raw.Verified = true
	boundary, err := client.index.resolve(channel.TeamId, channel.Id, user.Id, user.IsBot)
	return boundary, raw, err
}

func (client *Client) ResolveDelivery(projectID, actorID string) (entity.Boundary, error) {
	return client.index.resolveDelivery(projectID, actorID)
}

func (client *Client) DownloadFile(ctx context.Context, channelID, fileID string) ([]byte, string, string, error) {
	if invalidProviderID(channelID) || invalidProviderID(fileID) {
		return nil, "", "", errors.New("Mattermost file reference is invalid")
	}
	info, _, err := client.primary.api.GetFileInfo(ctx, fileID)
	if err != nil || info == nil || info.Id != fileID || info.ChannelId != channelID || info.DeleteAt != 0 ||
		info.Size <= 0 || info.Size > client.config.MaximumFileBytes || len(info.Name) == 0 || len(info.Name) > 255 {
		return nil, "", "", errors.New("Mattermost file metadata mismatch")
	}
	raw, _, err := client.primary.api.GetFile(ctx, fileID)
	if err != nil || len(raw) == 0 || int64(len(raw)) != info.Size || int64(len(raw)) > client.config.MaximumFileBytes {
		return nil, "", "", errors.New("Mattermost file body mismatch")
	}
	mediaType := info.MimeType
	if mediaType == "" || !strings.Contains(mediaType, "/") {
		mediaType = http.DetectContentType(raw[:min(len(raw), 512)])
	}
	return raw, filepath.Base(info.Name), mediaType, nil
}

func (client *Client) Publish(ctx context.Context, delivery entity.Delivery, attachments map[string][]byte) (domainmattermost.Published, error) {
	bot, ok := client.bots[delivery.BotStableKey]
	if !ok || invalidProviderID(delivery.ChannelID) || delivery.ID == "" {
		return domainmattermost.Published{}, errors.New("Mattermost delivery boundary is invalid")
	}
	var payload cardPayload
	if json.Unmarshal(delivery.Payload, &payload) != nil || payload.Message == "" {
		return domainmattermost.Published{}, errors.New("Mattermost card payload is invalid")
	}
	found, foundExact, err := client.findPending(ctx, bot, delivery, payload)
	if err != nil {
		return domainmattermost.Published{}, err
	}
	if foundExact {
		return publishedReceipt(found, delivery.PayloadSHA256)
	}
	fileIDs := make(model.StringArray, 0, len(delivery.Attachments))
	for _, binding := range delivery.Attachments {
		raw, exists := attachments[binding.ArtifactID]
		if !exists || binding.SizeBytes == 0 || binding.SizeBytes > uint64(client.config.MaximumFileBytes) ||
			uint64(len(raw)) != binding.SizeBytes || digest(raw) != binding.SHA256 ||
			binding.MediaType == "" || binding.Name == "" {
			return domainmattermost.Published{}, errors.New("Mattermost delivery attachment mismatch")
		}
		uploaded, _, err := bot.api.UploadFile(ctx, raw, delivery.ChannelID, binding.Name)
		if err != nil || uploaded == nil || len(uploaded.FileInfos) != 1 || uploaded.FileInfos[0].Id == "" {
			return domainmattermost.Published{}, errors.New("Mattermost file upload failed")
		}
		fileIDs = append(fileIDs, uploaded.FileInfos[0].Id)
	}
	post := &model.Post{
		ChannelId: delivery.ChannelID, RootId: delivery.RootPostID, Message: payload.Message,
		Props: payload.Props, FileIds: fileIDs, PendingPostId: delivery.ID,
	}
	created, _, err := bot.api.CreatePost(ctx, post)
	if err != nil {
		found, foundExact, findErr := client.findPending(ctx, bot, delivery, payload)
		if findErr != nil {
			return domainmattermost.Published{}, findErr
		}
		if foundExact {
			return publishedReceipt(found, delivery.PayloadSHA256)
		}
		return domainmattermost.Published{}, errors.New("Mattermost post delivery failed")
	}
	if created == nil || created.Id == "" || created.ChannelId != delivery.ChannelID || created.PendingPostId != delivery.ID {
		return domainmattermost.Published{}, errors.New("Mattermost post receipt mismatch")
	}
	return publishedReceipt(created, delivery.PayloadSHA256)
}

func (client *Client) OpenDecisionDialog(ctx context.Context, botStableKey, triggerID, callbackURL, state, locale string) error {
	bot, ok := client.bots[botStableKey]
	if !ok || triggerID == "" || state == "" {
		return errors.New("Mattermost dialog boundary is invalid")
	}
	title, introduction, label, field := "Owner decision", "Record a bounded reason for the decision.", "Submit", "Reason"
	if locale == "ru" {
		title, introduction, label, field = "Решение владельца", "Укажите ограниченное обоснование решения.", "Отправить", "Причина"
	}
	_, err := bot.api.OpenInteractiveDialog(ctx, model.OpenDialogRequest{
		TriggerId: triggerID, URL: callbackURL,
		Dialog: model.Dialog{
			CallbackId: "owner-decision", Title: title, IntroductionText: introduction,
			SubmitLabel: label, NotifyOnCancel: true, State: state,
			Elements: []model.DialogElement{
				{DisplayName: title, Name: "decision", Type: "select", Options: []*model.PostActionOptions{
					{Text: "APPROVE", Value: "APPROVE"}, {Text: "REJECT", Value: "REJECT"},
					{Text: "CHANGES_REQUESTED", Value: "CHANGES_REQUESTED"}, {Text: "CANCEL", Value: "CANCEL"},
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
	payload cardPayload) (*model.Post, bool, error) {
	since := delivery.CreatedAt.Add(-client.config.CatchUpWindow).UnixMilli()
	posts, _, err := bot.api.GetPostsSince(ctx, delivery.ChannelID, since, false)
	if err != nil || posts == nil {
		return nil, false, errors.New("Mattermost pending delivery lookup failed")
	}
	for _, post := range posts.Posts {
		if post != nil && post.PendingPostId == delivery.ID && post.ChannelId == delivery.ChannelID {
			if !client.exactPending(ctx, bot, post, delivery, payload) {
				return nil, false, errors.New("Mattermost pending delivery readback mismatch")
			}
			return post, true, nil
		}
	}
	return nil, false, nil
}

func (client *Client) exactPending(ctx context.Context, bot *botClient, post *model.Post,
	delivery entity.Delivery, payload cardPayload) bool {
	if post.UserId != bot.identity.UserID || post.RootId != delivery.RootPostID || post.Message != payload.Message ||
		len(post.FileIds) != len(delivery.Attachments) || !exactProps(post.Props, payload.Props) {
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
	for _, channelID := range client.ChannelIDs() {
		since := cursors[channelID]
		if since == 0 {
			since = time.Now().Add(-client.config.CatchUpWindow).UnixMilli()
		}
		posts, _, err := client.primary.api.GetPostsSince(ctx, channelID, since, false)
		if err != nil {
			return errors.New("Mattermost catch-up failed")
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
			return errors.New("Mattermost reaction catch-up failed")
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
				return errors.New("Mattermost WebSocket response channel closed")
			}
		case _, ok := <-websocketClient.PingTimeoutChannel:
			if ok {
				return errors.New("Mattermost WebSocket ping timed out")
			}
		case event, ok := <-websocketClient.EventChannel:
			if !ok {
				return errors.New("Mattermost WebSocket event channel closed")
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

func (client *Client) ChannelIDs() []string { return client.index.channelIDs() }

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
		return postRaw(&post), true
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
		PostID, ChannelID, RootPostID, PendingPostID, PayloadSHA256 string
		UpdateAt                                                    int64
	}{post.Id, post.ChannelId, post.RootId, post.PendingPostId, payloadDigest, post.UpdateAt})
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
		return "", errors.New("Mattermost token path is not absolute")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 16 || info.Size() > 16<<10 || info.Mode().Perm()&0o037 != 0 {
		return "", errors.New("Mattermost token file is unsafe")
	}
	raw, err := os.ReadFile(path)
	value := strings.TrimSpace(string(raw))
	if err != nil || len(value) < 16 || strings.ContainsAny(value, "\r\n\x00") {
		return "", errors.New("read Mattermost token")
	}
	return value, nil
}
