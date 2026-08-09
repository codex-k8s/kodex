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
	SiteURL                 string
	TLSServerName           string
	CAFile                  string
	ClientCertificateFile   string
	ClientPrivateKeyFile    string
	MappingManifestFile     string
	MappingExpectedRevision string
	MappingSHA256File       string
	MappingVaultKVVersion   uint64
	RequestTimeout          time.Duration
	MaximumFileBytes        int64
	CatchUpWindow           time.Duration
}

type botClient struct {
	identity BotIdentity
	api      *model.Client4
	token    string
}

type Client struct {
	config        Config
	manifest      Manifest
	index         *index
	bots          map[string]*botClient
	primary       *botClient
	dialer        *websocket.Dialer
	websocketURL  string
	httpClient    *http.Client
	runtimeRoutes domainmattermost.RuntimeRouteReader
	runtimeBots   domainmattermost.RuntimeBotIdentityReader
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
		config.CatchUpWindow < time.Minute || config.CatchUpWindow > 24*time.Hour ||
		config.MappingExpectedRevision == "" || !filepath.IsAbs(config.MappingSHA256File) || config.MappingVaultKVVersion == 0 {
		return nil, errors.New("mattermost client configuration is invalid")
	}
	manifest, manifestIndex, err := loadManifest(config.MappingManifestFile, config.MappingExpectedRevision,
		config.MappingSHA256File, config.MappingVaultKVVersion)
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
		websocketURL: "wss://" + parsed.Host, httpClient: &http.Client{Transport: transport, Timeout: config.RequestTimeout},
	}
	for _, identity := range manifest.Bots {
		token, tokenErr := readToken(identity.TokenFile)
		if tokenErr != nil {
			return nil, tokenErr
		}
		api := model.NewAPIv4Client(config.SiteURL)
		api.AuthToken = token
		api.AuthType = model.HeaderBearer
		api.HTTPClient = result.httpClient
		configured := &botClient{identity: identity, api: api, token: token}
		result.bots[identity.StableKey] = configured
		if result.primary == nil {
			result.primary = configured
		}
	}
	return result, nil
}

// UseRuntimeRoutes однократно подключает durable joined projection до startup
// barrier. Runtime route из manifest после этого не используется.
func (client *Client) UseRuntimeRoutes(reader domainmattermost.RuntimeRouteReader) error {
	if reader == nil || client.runtimeRoutes != nil {
		return errors.New("mattermost runtime route reader is invalid")
	}
	client.runtimeRoutes = reader
	return nil
}

// UseRuntimeBotIdentities однократно подключает authoritative Agent bot
// catalog до startup barrier. Static manifest после этого не выдаёт runtime
// credential для Agent delivery.
func (client *Client) UseRuntimeBotIdentities(reader domainmattermost.RuntimeBotIdentityReader) error {
	if reader == nil || client.runtimeBots != nil {
		return errors.New("mattermost runtime bot identity reader is invalid")
	}
	client.runtimeBots = reader
	return nil
}

func (client *Client) BootstrapBoundaries() []entity.Boundary {
	return client.index.channelBoundaries()
}

func (client *Client) Check(ctx context.Context) error {
	routes, err := client.runtimeRouteList(ctx)
	if err != nil || len(routes) == 0 || client.runtimeBots == nil {
		return errors.New("mattermost joined runtime route is not ready")
	}
	for _, route := range routes {
		stableKeys := map[string]struct{}{route.Boundary.BotStableKey: {}}
		template, ok := client.index.templates[route.TemplateKey]
		if !ok {
			return errors.New("mattermost joined runtime route template is unavailable")
		}
		for _, assignment := range template.Assignments {
			stableKeys[assignment.BotStableKey] = struct{}{}
		}
		for stableKey := range stableKeys {
			bot, identity, botErr := client.runtimeBot(ctx, route.Principal, stableKey, "", 0)
			if botErr != nil {
				return errors.New("mattermost bot identity is not ready")
			}
			channel, channelErr := client.getMappedChannel(ctx, bot, route.Boundary.TeamID, route.Boundary.ChannelID)
			if channelErr != nil || channel == nil || channel.Id != route.Boundary.ChannelID ||
				channel.TeamId != route.Boundary.TeamID || channel.DeleteAt != 0 {
				return errors.New("mattermost mapped channel is not ready")
			}
			member, _, memberErr := bot.api.GetChannelMember(ctx, route.Boundary.ChannelID, identity.ProviderUserID, "")
			if memberErr != nil || member == nil || member.ChannelId != route.Boundary.ChannelID ||
				member.UserId != identity.ProviderUserID {
				return errors.New("mattermost bot channel binding is not ready")
			}
		}
	}
	return nil
}

func (client *Client) ReconcileLifecycle(ctx context.Context, knownThreads map[string]string,
	consume func(context.Context, domainmattermost.RawEvent) error,
) error {
	boundaries, err := client.ChannelBoundaries(ctx)
	if err != nil {
		return err
	}
	for _, boundary := range boundaries {
		channel, err := client.getMappedChannel(ctx, client.primary, boundary.TeamID, boundary.ChannelID)
		if err != nil || channel == nil || channel.Id != boundary.ChannelID || channel.TeamId != boundary.TeamID {
			return errors.New("mattermost channel lifecycle reconciliation failed")
		}
		revision := max(channel.CreateAt, channel.UpdateAt, channel.DeleteAt)
		if revision <= 0 {
			return errors.New("mattermost channel lifecycle revision is unavailable")
		}
		kind := "CHANNEL_RESTORE"
		if channel.DeleteAt != 0 {
			kind = "CHANNEL_DELETE"
		}
		raw := domainmattermost.RawEvent{
			Kind: kind, Revision: uint64(revision), TeamID: channel.TeamId,
			ChannelID: channel.Id, DeleteAt: channel.DeleteAt,
			ProviderEventID: fmt.Sprintf("reconcile:%s:%s:%d", strings.ToLower(kind), channel.Id, revision),
		}
		if err := consume(ctx, raw); err != nil {
			return err
		}
	}
	rootIDs := make([]string, 0, len(knownThreads))
	for rootPostID := range knownThreads {
		rootIDs = append(rootIDs, rootPostID)
	}
	sort.Strings(rootIDs)
	for _, rootPostID := range rootIDs {
		post, _, err := client.primary.api.GetPostIncludeDeleted(ctx, rootPostID, "")
		if err != nil || post == nil || post.Id != rootPostID || post.ChannelId != knownThreads[rootPostID] || post.RootId != "" {
			return errors.New("mattermost thread lifecycle reconciliation failed")
		}
		revision := max(post.CreateAt, post.UpdateAt, post.DeleteAt)
		kind := "THREAD_RESTORE"
		if post.DeleteAt != 0 {
			kind = "THREAD_DELETE"
		}
		raw := domainmattermost.RawEvent{
			Kind: kind, Revision: uint64(revision), ChannelID: post.ChannelId,
			PostID: post.Id, RootPostID: post.Id, UserID: post.UserId, DeleteAt: post.DeleteAt,
			ProviderEventID: fmt.Sprintf("reconcile:%s:%s:%d", strings.ToLower(kind), post.Id, revision),
		}
		if err := consume(ctx, raw); err != nil {
			return err
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
	route, err := client.runtimeRoute(ctx, channel.TeamId, channel.Id)
	if err != nil {
		return entity.Boundary{}, raw, err
	}
	boundary, template, err := client.resolveRuntimeBoundary(route, user.Id, user.IsBot)
	if err != nil || boundary.IgnoredBot {
		return boundary, raw, err
	}
	selectors := make([]string, 0, 2)
	for _, mention := range explicitMentions(raw.Text) {
		if _, assigned := client.index.assigned(template.TeamID, template.ChannelID, mention); assigned {
			selectors = append(selectors, mention)
		}
	}
	if len(selectors) > 1 {
		return entity.Boundary{}, raw, errors.New("mattermost agent assignment is ambiguous")
	}
	if len(selectors) == 1 {
		mentioned, _, readErr := client.primary.api.GetUserByUsername(ctx, selectors[0], "")
		if readErr != nil || mentioned == nil || mentioned.Id == "" || mentioned.DeleteAt != 0 {
			return entity.Boundary{}, raw, errors.New("mattermost mentioned identity is unknown")
		}
		assignment, assignmentErr := client.index.resolveAssignment(template.TeamID, template.ChannelID, selectors[0], mentioned.Id)
		if assignmentErr != nil {
			return entity.Boundary{}, raw, assignmentErr
		}
		boundary.RoleID, boundary.BotStableKey = assignment.RoleID, assignment.BotStableKey
		boundary, err = client.attachRuntimeBotIdentity(ctx, route.Principal, boundary, mentioned.Id)
	} else {
		boundary, err = client.attachRuntimeBotIdentity(ctx, route.Principal, boundary, "")
	}
	return boundary, raw, err
}

func (client *Client) resolveLifecycle(ctx context.Context, raw domainmattermost.RawEvent) (entity.Boundary, domainmattermost.RawEvent, error) {
	if invalidProviderID(raw.ChannelID) || raw.Revision == 0 {
		return entity.Boundary{}, raw, errors.New("mattermost lifecycle event is invalid")
	}
	teamID := raw.TeamID
	if teamID == "" {
		return entity.Boundary{}, raw, errors.New("mattermost lifecycle Team identity is missing")
	}
	channel, err := client.getMappedChannel(ctx, client.primary, teamID, raw.ChannelID)
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
		post, _, postErr := client.primary.api.GetPostIncludeDeleted(ctx, raw.PostID, "")
		if postErr != nil || post == nil || post.Id != raw.PostID || post.ChannelId != raw.ChannelID || post.RootId != "" ||
			(deleting && post.DeleteAt == 0) || (!deleting && post.DeleteAt != 0) {
			return entity.Boundary{}, raw, errors.New("mattermost thread lifecycle readback mismatch")
		}
		raw.RootPostID, raw.DeleteAt, raw.UserID = post.Id, post.DeleteAt, post.UserId
		raw.Revision = uint64(max(post.UpdateAt, post.DeleteAt))
		raw.ProviderEventID = fmt.Sprintf("%s:%s:%d", strings.ToLower(raw.Kind), post.Id, raw.Revision)
	}
	route, routeErr := client.runtimeRoute(ctx, channel.TeamId, channel.Id)
	if routeErr != nil {
		return entity.Boundary{}, raw, errors.New("mattermost lifecycle event is outside the server-owned mapping")
	}
	raw.TeamID, raw.Verified = channel.TeamId, true
	if raw.Kind != "THREAD_RESTORE_CANDIDATE" {
		raw.UserID = client.primary.identity.UserID
	}
	boundary := route.Boundary
	boundary.ActorID = boundary.MappingOwnerActorID
	return boundary, raw, nil
}

func (client *Client) getMappedChannel(ctx context.Context, bot *botClient, teamID, channelID string) (*model.Channel, error) {
	if bot == nil || invalidProviderID(teamID) || invalidProviderID(channelID) {
		return nil, errors.New("mattermost mapped channel reference is invalid")
	}
	channels, _, err := bot.api.GetChannelsForTeamForUser(ctx, teamID, bot.identity.UserID, true, "")
	if err != nil {
		return nil, errors.New("read Mattermost mapped channels including deleted")
	}
	for _, channel := range channels {
		if channel != nil && channel.Id == channelID && channel.TeamId == teamID {
			return channel, nil
		}
	}
	return nil, errors.New("mattermost mapped channel is unavailable")
}

func (client *Client) ResolveDelivery(ctx context.Context, projectID, actorID string) (entity.Boundary, error) {
	return client.resolveRuntimeDelivery(ctx, projectID, "", actorID)
}

func (client *Client) ResolveRoomDelivery(ctx context.Context, projectID, chatID, actorID string) (entity.Boundary, error) {
	return client.resolveRuntimeDelivery(ctx, projectID, chatID, actorID)
}

func (client *Client) ResolveMappedChannel(ctx context.Context, teamID, channelID string) (entity.Boundary, error) {
	route, err := client.runtimeRoute(ctx, teamID, channelID)
	if err != nil {
		return entity.Boundary{}, err
	}
	return client.attachRuntimeBotIdentity(ctx, route.Principal, route.Boundary, "")
}

// ValidateRuntimeBotIdentity подтверждает, что сохранённый inbound всё ещё
// связан с current admitted generation непосредственно перед продолжением.
func (client *Client) ValidateRuntimeBotIdentity(ctx context.Context, teamID, channelID, stableKey,
	providerUserID string, generation uint64,
) error {
	if invalidProviderID(teamID) || invalidProviderID(channelID) || stableKey == "" ||
		invalidProviderID(providerUserID) || generation == 0 {
		return errors.New("mattermost runtime bot identity boundary is invalid")
	}
	route, err := client.runtimeRoute(ctx, teamID, channelID)
	if err != nil || client.runtimeBots == nil || !client.runtimeStableKeyAllowed(route, stableKey) {
		return errors.New("mattermost runtime bot identity route is not current")
	}
	identity, err := client.runtimeBots.ResolveCurrentRuntimeIdentity(ctx, route.Principal, stableKey, providerUserID)
	if err != nil || identity.ProviderUserID != providerUserID || identity.ProviderGeneration != generation ||
		identity.ProviderTeamID != teamID || identity.Status != "AVAILABLE" {
		return errors.New("mattermost runtime bot identity generation is stale")
	}
	return nil
}

func (client *Client) runtimeStableKeyAllowed(route entity.MattermostRuntimeRoute, stableKey string) bool {
	if route.Boundary.BotStableKey == stableKey {
		return true
	}
	template, ok := client.index.templates[route.TemplateKey]
	if !ok {
		return false
	}
	for _, assignment := range template.Assignments {
		if assignment.BotStableKey == stableKey {
			return true
		}
	}
	return false
}

func (client *Client) runtimeRoute(ctx context.Context, teamID, channelID string) (entity.MattermostRuntimeRoute, error) {
	if client.runtimeRoutes == nil {
		return entity.MattermostRuntimeRoute{}, errors.New("mattermost runtime route reader is unavailable")
	}
	route, err := client.runtimeRoutes.ResolveRuntimeRoute(ctx, teamID, channelID)
	if err != nil || route.Boundary.TeamID != teamID || route.Boundary.ChannelID != channelID ||
		route.MappingVersion == 0 || route.MappingGeneration == 0 || route.MappingDigestSHA256 == "" ||
		route.ProviderSnapshotSHA256 == "" || route.RouteDigestSHA256 == "" {
		return entity.MattermostRuntimeRoute{}, errors.New("mattermost joined runtime route is unavailable")
	}
	if _, ok := client.index.templates[route.TemplateKey]; !ok {
		return entity.MattermostRuntimeRoute{}, errors.New("mattermost runtime route template is unavailable")
	}
	return route, nil
}

func (client *Client) runtimeRouteList(ctx context.Context) ([]entity.MattermostRuntimeRoute, error) {
	if client.runtimeRoutes == nil {
		return nil, errors.New("mattermost runtime route reader is unavailable")
	}
	routes, err := client.runtimeRoutes.ListRuntimeRoutes(ctx)
	if err != nil {
		return nil, err
	}
	for _, route := range routes {
		if _, err := client.runtimeRoute(ctx, route.Boundary.TeamID, route.Boundary.ChannelID); err != nil {
			return nil, err
		}
	}
	return routes, nil
}

func (client *Client) resolveRuntimeBoundary(route entity.MattermostRuntimeRoute, userID string,
	isBot bool,
) (entity.Boundary, ChannelBinding, error) {
	template, ok := client.index.templates[route.TemplateKey]
	if !ok || template.OrganizationID != route.Boundary.OrganizationID ||
		template.ProjectID != route.Boundary.ProjectID || template.ChatID != route.Boundary.ChatID ||
		template.RoleID != route.Boundary.RoleID || template.LifecycleActorID != route.Boundary.MappingOwnerActorID {
		return entity.Boundary{}, ChannelBinding{}, errors.New("mattermost joined runtime route template mismatch")
	}
	boundary := route.Boundary
	if isBot {
		boundary.IgnoredBot = true
		return boundary, template, nil
	}
	if _, ignored := client.index.botUsers[userID]; ignored {
		boundary.IgnoredBot = true
		return boundary, template, nil
	}
	actor, ok := client.index.actors[userID+"\x00"+boundary.OrganizationID+"\x00"+boundary.ProjectID]
	if !ok {
		return boundary, template, errors.New("mattermost actor is outside the joined runtime route")
	}
	boundary.ActorID, boundary.MattermostUserID = actor.ActorID, userID
	return boundary, template, nil
}

func (client *Client) resolveRuntimeDelivery(ctx context.Context, projectID, chatID,
	actorID string,
) (entity.Boundary, error) {
	if client.runtimeRoutes == nil {
		return entity.Boundary{}, errors.New("mattermost runtime route reader is unavailable")
	}
	route, err := client.runtimeRoutes.ResolveRuntimeDelivery(ctx, projectID, chatID, actorID)
	if err != nil {
		return entity.Boundary{}, err
	}
	actor, err := client.index.resolveOwner(entity.TeamPrincipal{
		ActorID: actorID, OrganizationID: route.Boundary.OrganizationID, ProjectID: projectID,
	})
	if err != nil {
		return entity.Boundary{}, err
	}
	boundary := route.Boundary
	boundary.ActorID, boundary.MattermostUserID = actorID, actor.MattermostUserID
	return client.attachRuntimeBotIdentity(ctx, route.Principal, boundary, "")
}

func (client *Client) attachRuntimeBotIdentity(ctx context.Context, principal entity.TeamPrincipal,
	boundary entity.Boundary, providerUserID string,
) (entity.Boundary, error) {
	if client.runtimeBots == nil || boundary.BotStableKey == "" {
		return entity.Boundary{}, errors.New("mattermost runtime bot identity reader is unavailable")
	}
	identity, err := client.runtimeBots.ResolveCurrentRuntimeIdentity(ctx, principal,
		boundary.BotStableKey, providerUserID)
	if err != nil || identity.ProviderUserID == "" || identity.ProviderGeneration == 0 ||
		identity.ProviderTeamID != boundary.TeamID {
		return entity.Boundary{}, errors.New("mattermost runtime bot identity is not current")
	}
	boundary.BotProviderUserID = identity.ProviderUserID
	boundary.BotProviderGeneration = identity.ProviderGeneration
	return boundary, nil
}

func (client *Client) runtimeBot(ctx context.Context, principal entity.TeamPrincipal, stableKey,
	providerUserID string, generation uint64,
) (*botClient, entity.AgentMattermostBotIdentity, error) {
	if client.runtimeBots == nil {
		return nil, entity.AgentMattermostBotIdentity{}, errors.New("mattermost runtime bot identity reader is unavailable")
	}
	identity, token, err := client.runtimeBots.ReadCurrentRuntimeBotToken(ctx, principal,
		stableKey, providerUserID, generation)
	if err != nil || identity.ProviderUserID == "" || identity.ProviderGeneration == 0 || token == "" {
		return nil, entity.AgentMattermostBotIdentity{}, errors.New("mattermost runtime bot identity is unavailable")
	}
	api := model.NewAPIv4Client(client.config.SiteURL)
	api.AuthToken, api.AuthType, api.HTTPClient = token, model.HeaderBearer, client.httpClient
	bot := &botClient{identity: BotIdentity{StableKey: stableKey, UserID: identity.ProviderUserID}, api: api, token: token}
	return bot, identity, nil
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

func (client *Client) Publish(ctx context.Context, delivery entity.Delivery, fileIDs []string) (domainmattermost.Published, error) {
	if invalidProviderID(delivery.ChannelID) || delivery.ID == "" || len(fileIDs) != len(delivery.Attachments) ||
		delivery.BotProviderUserID == "" || delivery.BotProviderGeneration == 0 {
		return domainmattermost.Published{}, errors.New("mattermost delivery boundary is invalid")
	}
	route, err := client.runtimeRoute(ctx, delivery.TeamID, delivery.ChannelID)
	if err != nil || route.Boundary.OrganizationID != delivery.OrganizationID ||
		route.Boundary.ProjectID != delivery.ProjectID {
		return domainmattermost.Published{}, errors.New("mattermost delivery route is not current")
	}
	bot, identity, err := client.runtimeBot(ctx, route.Principal, delivery.BotStableKey,
		delivery.BotProviderUserID, delivery.BotProviderGeneration)
	if err != nil || identity.ProviderTeamID != delivery.TeamID {
		return domainmattermost.Published{}, errors.New("mattermost delivery bot identity is not current")
	}
	var payload cardPayload
	if json.Unmarshal(delivery.Payload, &payload) != nil || payload.Message == "" {
		return domainmattermost.Published{}, errors.New("mattermost card payload is invalid")
	}
	if delivery.UpdatePostID != "" {
		post := &model.Post{
			Id: delivery.UpdatePostID, ChannelId: delivery.ChannelID,
			RootId: delivery.RootPostID, Message: payload.Message, Props: payload.Props,
			FileIds: model.StringArray(fileIDs),
		}
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
		return domainmattermost.Published{}, domainmattermost.ErrAmbiguousEffect
	}
	if created == nil || created.Id == "" {
		return domainmattermost.Published{}, domainmattermost.ErrAmbiguousEffect
	}
	readback, _, err := bot.api.GetPost(ctx, created.Id, "")
	if err != nil || readback == nil || readback.Id != created.Id || !client.exactPending(ctx, bot, readback, delivery, payload, fileIDs) {
		return domainmattermost.Published{}, domainmattermost.ErrAmbiguousEffect
	}
	return publishedReceipt(readback, delivery.PayloadSHA256)
}

func (client *Client) OpenDecisionDialog(ctx context.Context, delivery entity.Delivery,
	triggerID, callbackURL, state, locale string,
) error {
	if triggerID == "" || state == "" || delivery.BotProviderGeneration == 0 || delivery.BotProviderUserID == "" {
		return errors.New("mattermost dialog boundary is invalid")
	}
	route, err := client.runtimeRoute(ctx, delivery.TeamID, delivery.ChannelID)
	if err != nil || route.Boundary.OrganizationID != delivery.OrganizationID || route.Boundary.ProjectID != delivery.ProjectID {
		return errors.New("mattermost dialog route is not current")
	}
	bot, identity, err := client.runtimeBot(ctx, route.Principal, delivery.BotStableKey,
		delivery.BotProviderUserID, delivery.BotProviderGeneration)
	if err != nil || identity.ProviderTeamID != delivery.TeamID {
		return errors.New("mattermost dialog bot identity is not current")
	}
	title, introduction, label, field := "Owner decision", "Record a bounded reason for the decision.", "Submit", "Reason"
	approve, reject, changes, cancel := "Approve", "Reject", "Request changes", "Cancel"
	if locale == "ru" {
		title, introduction, label, field = "Решение владельца", "Укажите ограниченное обоснование решения.", "Отправить", "Причина"
		approve, reject, changes, cancel = "Одобрить", "Отклонить", "Запросить изменения", "Отменить"
	}
	_, err = bot.api.OpenInteractiveDialog(ctx, model.OpenDialogRequest{
		TriggerId: triggerID, URL: callbackURL,
		Dialog: model.Dialog{
			CallbackId: "owner-decision", Title: title, IntroductionText: introduction,
			SubmitLabel: label, NotifyOnCancel: true, State: state,
			Elements: []model.DialogElement{
				{DisplayName: title, Name: "decision", Type: "select", Options: []*model.PostActionOptions{
					{Text: approve, Value: "APPROVE"},
					{Text: reject, Value: "REJECT"},
					{Text: changes, Value: "CHANGES_REQUESTED"},
					{Text: cancel, Value: "CANCEL"},
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
	payload cardPayload, fileIDs []string,
) (*model.Post, bool, error) {
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
	delivery entity.Delivery, payload cardPayload, expectedFileIDs []string,
) bool {
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
	consume func(context.Context, domainmattermost.RawEvent) error,
) error {
	boundaries, err := client.ChannelBoundaries(ctx)
	if err != nil {
		return err
	}
	for _, boundary := range boundaries {
		channelID := boundary.ChannelID
		channel, _, channelErr := client.primary.api.GetChannel(ctx, channelID)
		if channelErr != nil || channel == nil || channel.Id != channelID || channel.TeamId != boundary.TeamID {
			return errors.New("mattermost catch-up channel readback failed")
		}
		if channel.DeleteAt != 0 {
			continue
		}
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
			if post == nil {
				continue
			}
			if post.DeleteAt != 0 && post.RootId == "" {
				raw := postRaw(post)
				raw.Kind, raw.RootPostID, raw.DeleteAt = "THREAD_DELETE", post.Id, post.DeleteAt
				if err := consume(ctx, raw); err != nil {
					return err
				}
				continue
			}
			if post.DeleteAt != 0 {
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

func (client *Client) ChannelBoundaries(ctx context.Context) ([]entity.Boundary, error) {
	routes, err := client.runtimeRouteList(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]entity.Boundary, 0, len(routes))
	for _, route := range routes {
		result = append(result, route.Boundary)
	}
	return result, nil
}

func (client *Client) ReadinessBoundary(ctx context.Context) (entity.Boundary, error) {
	routes, err := client.runtimeRouteList(ctx)
	if err == nil && len(routes) > 0 {
		boundary := routes[0].Boundary
		boundary.ActorID = boundary.MappingOwnerActorID
		return client.attachRuntimeBotIdentity(ctx, routes[0].Principal, boundary, "")
	}
	return entity.Boundary{}, errors.New("mattermost readiness boundary is unavailable")
}

func (client *Client) AuthenticateArtifactDownload(ctx context.Context, bearer string, grant entity.DownloadGrant) error {
	if !strings.HasPrefix(bearer, "Bearer ") || strings.ContainsAny(bearer, "\r\n\x00") {
		return errors.New("mattermost artifact credential is invalid")
	}
	token := strings.TrimSpace(strings.TrimPrefix(bearer, "Bearer "))
	if len(token) < 16 || len(token) > 16<<10 {
		return errors.New("mattermost artifact credential is invalid")
	}
	api := model.NewAPIv4Client(client.config.SiteURL)
	api.AuthToken, api.AuthType, api.HTTPClient = token, model.HeaderBearer, client.httpClient
	user, _, err := api.GetMe(ctx, "")
	if err != nil || user == nil || user.Id != grant.MattermostUserID || user.DeleteAt != 0 || user.IsBot {
		return errors.New("mattermost artifact actor is invalid")
	}
	route, err := client.runtimeRoute(ctx, grant.TeamID, grant.ChannelID)
	if err != nil {
		return errors.New("mattermost artifact actor boundary mismatch")
	}
	boundary, _, err := client.resolveRuntimeBoundary(route, user.Id, false)
	if err != nil || boundary.OrganizationID != grant.OrganizationID || boundary.ProjectID != grant.ProjectID ||
		boundary.ActorID != grant.ActorID || boundary.TeamID != grant.TeamID || boundary.ChannelID != grant.ChannelID {
		return errors.New("mattermost artifact actor boundary mismatch")
	}
	if client.runtimeBots == nil {
		return errors.New("mattermost artifact bot identity reader is unavailable")
	}
	identity, err := client.runtimeBots.ResolveCurrentRuntimeIdentity(ctx, route.Principal,
		grant.BotStableKey, grant.BotProviderUserID)
	if err != nil || identity.ProviderUserID != grant.BotProviderUserID ||
		identity.ProviderGeneration != grant.BotProviderGeneration || identity.ProviderTeamID != grant.TeamID {
		return errors.New("mattermost artifact bot identity is stale")
	}
	member, _, err := client.primary.api.GetChannelMember(ctx, grant.ChannelID, user.Id, "")
	if err != nil || member == nil || member.ChannelId != grant.ChannelID || member.UserId != user.Id {
		return errors.New("mattermost artifact channel membership is invalid")
	}
	return nil
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
		return domainmattermost.RawEvent{
			ProviderEventID: fmt.Sprintf("%s:%s:%d", strings.ToLower(kind), channelID, event.GetSequence()),
			Kind:            kind, Revision: uint64(event.GetSequence()), ChannelID: channelID,
		}, true
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
	}{
		post.Id, post.ChannelId, post.RootId, post.PendingPostId, post.UserId, payloadDigest,
		append([]string(nil), post.FileIds...), post.UpdateAt,
	})
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
