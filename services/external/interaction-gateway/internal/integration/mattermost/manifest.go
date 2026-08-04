package mattermost

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/codex-k8s/matter-codex/libs/go/i18n"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
	"github.com/google/uuid"
)

type Manifest struct {
	Version  int              `json:"version"`
	Channels []ChannelBinding `json:"channels"`
	Actors   []ActorBinding   `json:"actors"`
	Bots     []BotIdentity    `json:"bots"`
}

type ChannelBinding struct {
	TeamID         string `json:"team_id"`
	ChannelID      string `json:"channel_id"`
	OrganizationID string `json:"organization_id"`
	ProjectID      string `json:"project_id"`
	ChatID         string `json:"chat_id"`
	RoleID         string `json:"role_id"`
	SessionID      string `json:"session_id,omitempty"`
	Locale         string `json:"locale"`
	BotStableKey   string `json:"bot_stable_key"`
	OwnerDelivery  bool   `json:"owner_delivery"`
}

type ActorBinding struct {
	MattermostUserID string `json:"mattermost_user_id"`
	ActorID          string `json:"actor_id"`
	OrganizationID   string `json:"organization_id"`
	ProjectID        string `json:"project_id"`
}

type BotIdentity struct {
	StableKey string `json:"stable_key"`
	UserID    string `json:"user_id"`
	TokenFile string `json:"token_file"`
}

type index struct {
	channels   map[string]ChannelBinding
	actors     map[string]ActorBinding
	bots       map[string]BotIdentity
	botUsers   map[string]struct{}
	deliveries map[string]entity.Boundary
}

func loadManifest(path string) (Manifest, *index, error) {
	if !filepath.IsAbs(path) {
		return Manifest{}, nil, errors.New("Mattermost mapping manifest path is not absolute")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 1<<20 || info.Mode().Perm()&0o022 != 0 {
		return Manifest{}, nil, errors.New("Mattermost mapping manifest is unsafe")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, nil, errors.New("read Mattermost mapping manifest")
	}
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&manifest) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		manifest.Version != 1 || len(manifest.Channels) == 0 || len(manifest.Actors) == 0 || len(manifest.Bots) == 0 {
		return Manifest{}, nil, errors.New("Mattermost mapping manifest is invalid")
	}
	result := &index{
		channels: map[string]ChannelBinding{}, actors: map[string]ActorBinding{}, bots: map[string]BotIdentity{},
		botUsers: map[string]struct{}{}, deliveries: map[string]entity.Boundary{},
	}
	for _, bot := range manifest.Bots {
		if invalidProviderID(bot.UserID) || invalidStableKey(bot.StableKey) || !filepath.IsAbs(bot.TokenFile) {
			return Manifest{}, nil, errors.New("Mattermost bot identity is invalid")
		}
		if _, exists := result.bots[bot.StableKey]; exists {
			return Manifest{}, nil, errors.New("Mattermost bot identity is duplicated")
		}
		result.bots[bot.StableKey] = bot
		result.botUsers[bot.UserID] = struct{}{}
	}
	teamProjects, projectTeams := map[string]string{}, map[string]string{}
	channelChats, chatChannels := map[string]string{}, map[string]string{}
	for _, channel := range manifest.Channels {
		locale, localeOK := i18n.ResolveLocale(channel.Locale)
		_, botOK := result.bots[channel.BotStableKey]
		if invalidProviderID(channel.TeamID) || invalidProviderID(channel.ChannelID) ||
			uuid.Validate(channel.OrganizationID) != nil || uuid.Validate(channel.ProjectID) != nil ||
			uuid.Validate(channel.ChatID) != nil || uuid.Validate(channel.RoleID) != nil ||
			(channel.SessionID != "" && uuid.Validate(channel.SessionID) != nil) || !localeOK || !botOK {
			return Manifest{}, nil, errors.New("Mattermost channel mapping is invalid")
		}
		channel.Locale = locale
		key := channel.TeamID + "\x00" + channel.ChannelID
		if _, exists := result.channels[key]; exists {
			return Manifest{}, nil, errors.New("Mattermost channel mapping is duplicated")
		}
		projectScope := channel.OrganizationID + "\x00" + channel.ProjectID
		if existing, exists := teamProjects[channel.TeamID]; exists && existing != projectScope {
			return Manifest{}, nil, errors.New("Mattermost team mapping is ambiguous")
		}
		if existing, exists := projectTeams[projectScope]; exists && existing != channel.TeamID {
			return Manifest{}, nil, errors.New("Mattermost project mapping is ambiguous")
		}
		chatScope := projectScope + "\x00" + channel.ChatID
		if existing, exists := channelChats[channel.ChannelID]; exists && existing != chatScope {
			return Manifest{}, nil, errors.New("Mattermost channel chat mapping is ambiguous")
		}
		if existing, exists := chatChannels[chatScope]; exists && existing != channel.ChannelID {
			return Manifest{}, nil, errors.New("Mattermost chat mapping is ambiguous")
		}
		teamProjects[channel.TeamID], projectTeams[projectScope] = projectScope, channel.TeamID
		channelChats[channel.ChannelID], chatChannels[chatScope] = chatScope, channel.ChannelID
		result.channels[key] = channel
	}
	for _, actor := range manifest.Actors {
		if invalidProviderID(actor.MattermostUserID) || uuid.Validate(actor.ActorID) != nil ||
			uuid.Validate(actor.OrganizationID) != nil || uuid.Validate(actor.ProjectID) != nil {
			return Manifest{}, nil, errors.New("Mattermost actor mapping is invalid")
		}
		key := actor.MattermostUserID + "\x00" + actor.OrganizationID + "\x00" + actor.ProjectID
		if _, exists := result.actors[key]; exists {
			return Manifest{}, nil, errors.New("Mattermost actor mapping is duplicated")
		}
		result.actors[key] = actor
	}
	for _, channel := range result.channels {
		if !channel.OwnerDelivery {
			continue
		}
		for _, actor := range result.actors {
			if actor.ProjectID != channel.ProjectID || actor.OrganizationID != channel.OrganizationID {
				continue
			}
			key := channel.ProjectID + "\x00" + actor.ActorID
			if _, exists := result.deliveries[key]; exists {
				return Manifest{}, nil, errors.New("Mattermost owner delivery route is ambiguous")
			}
			result.deliveries[key] = entity.Boundary{
				OrganizationID: channel.OrganizationID, ProjectID: channel.ProjectID,
				ChatID: channel.ChatID, ActorID: actor.ActorID, RoleID: channel.RoleID,
				Locale: channel.Locale, BotStableKey: channel.BotStableKey,
				TeamID: channel.TeamID, ChannelID: channel.ChannelID, SessionID: channel.SessionID,
			}
		}
	}
	if len(result.deliveries) == 0 {
		return Manifest{}, nil, errors.New("Mattermost owner delivery route is missing")
	}
	return manifest, result, nil
}

func (current *index) resolve(teamID, channelID, userID string, isBot bool) (entity.Boundary, error) {
	channel, ok := current.channels[teamID+"\x00"+channelID]
	if !ok {
		return entity.Boundary{}, errors.New("Mattermost channel is outside the server-owned mapping")
	}
	if isBot {
		return entity.Boundary{IgnoredBot: true}, nil
	}
	if _, ignored := current.botUsers[userID]; ignored {
		return entity.Boundary{IgnoredBot: true}, nil
	}
	actor, ok := current.actors[userID+"\x00"+channel.OrganizationID+"\x00"+channel.ProjectID]
	if !ok {
		return entity.Boundary{}, errors.New("Mattermost actor is outside the server-owned mapping")
	}
	return entity.Boundary{
		OrganizationID: channel.OrganizationID, ProjectID: channel.ProjectID,
		ChatID: channel.ChatID, ActorID: actor.ActorID, RoleID: channel.RoleID,
		Locale: channel.Locale, BotStableKey: channel.BotStableKey,
		TeamID: teamID, ChannelID: channelID, SessionID: channel.SessionID,
	}, nil
}

func (current *index) channelIDs() []string {
	values := make([]string, 0, len(current.channels))
	for _, binding := range current.channels {
		values = append(values, binding.ChannelID)
	}
	slices.Sort(values)
	return slices.Compact(values)
}

func (current *index) resolveDelivery(projectID, actorID string) (entity.Boundary, error) {
	boundary, ok := current.deliveries[projectID+"\x00"+actorID]
	if ok {
		return boundary, nil
	}
	return entity.Boundary{}, errors.New("Mattermost delivery target is outside the server-owned mapping")
}

func invalidProviderID(value string) bool {
	return len(value) < 1 || len(value) > 64 || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\x00\r\n")
}

func invalidStableKey(value string) bool {
	if len(value) < 1 || len(value) > 64 {
		return true
	}
	for _, symbol := range value {
		if (symbol < 'a' || symbol > 'z') && (symbol < '0' || symbol > '9') && symbol != '-' {
			return true
		}
	}
	return false
}
