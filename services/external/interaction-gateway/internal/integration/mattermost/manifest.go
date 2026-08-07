package mattermost

import (
	"crypto/sha256"
	"encoding/hex"
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
	Source   string           `json:"source"`
	Revision string           `json:"revision"`
	Channels []ChannelBinding `json:"channels"`
	Actors   []ActorBinding   `json:"actors"`
	Bots     []BotIdentity    `json:"bots"`
}

type ChannelBinding struct {
	RuntimeKey       string            `json:"-"`
	TeamID           string            `json:"team_id"`
	ChannelID        string            `json:"channel_id"`
	OrganizationID   string            `json:"organization_id"`
	ProjectID        string            `json:"project_id"`
	ChatID           string            `json:"chat_id"`
	RoleID           string            `json:"role_id"`
	SessionID        string            `json:"session_id,omitempty"`
	Locale           string            `json:"locale"`
	BotStableKey     string            `json:"bot_stable_key"`
	OwnerDelivery    bool              `json:"owner_delivery"`
	LifecycleActorID string            `json:"lifecycle_actor_id"`
	Assignments      []AgentAssignment `json:"assignments,omitempty"`
}

// AgentAssignment разделяет server-owned default route и точное разрешённое
// назначение, выбранное только по проверенному Mattermost user readback.
type AgentAssignment struct {
	MentionUsername  string `json:"mention_username"`
	MattermostUserID string `json:"mattermost_user_id"`
	RoleID           string `json:"role_id"`
	BotStableKey     string `json:"bot_stable_key"`
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
	channels    map[string]ChannelBinding
	actors      map[string]ActorBinding
	bots        map[string]BotIdentity
	botUsers    map[string]struct{}
	assignments map[string]AgentAssignment
	templates   map[string]ChannelBinding
}

var runtimeTemplateNamespace = uuid.MustParse("5d75f46a-a2d2-54e7-9978-108c3934fd39")

func loadManifest(path, expectedRevision, digestFile string, vaultKVVersion uint64) (Manifest, *index, error) {
	if !filepath.IsAbs(path) {
		return Manifest{}, nil, errors.New("mattermost mapping manifest path is not absolute")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 1<<20 || info.Mode().Perm()&0o022 != 0 {
		return Manifest{}, nil, errors.New("mattermost mapping manifest is unsafe")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, nil, errors.New("read Mattermost mapping manifest")
	}
	digestRaw, digestErr := os.ReadFile(digestFile)
	expectedDigest := strings.TrimSpace(string(digestRaw))
	sum := sha256.Sum256(raw)
	if digestErr != nil || vaultKVVersion == 0 || len(expectedDigest) != 64 ||
		hex.EncodeToString(sum[:]) != expectedDigest {
		return Manifest{}, nil, errors.New("mattermost mapping immutable digest mismatch")
	}
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&manifest) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		manifest.Version != 1 || !strings.HasPrefix(manifest.Source, "vault://") ||
		len(manifest.Revision) < 8 || len(manifest.Revision) > 128 || strings.TrimSpace(manifest.Revision) != manifest.Revision ||
		manifest.Revision != expectedRevision ||
		len(manifest.Channels) == 0 || len(manifest.Actors) == 0 || len(manifest.Bots) == 0 {
		return Manifest{}, nil, errors.New("mattermost mapping manifest is invalid")
	}
	result := &index{
		channels: map[string]ChannelBinding{}, actors: map[string]ActorBinding{}, bots: map[string]BotIdentity{},
		botUsers: map[string]struct{}{}, assignments: map[string]AgentAssignment{},
		templates: map[string]ChannelBinding{},
	}
	for _, bot := range manifest.Bots {
		if invalidProviderID(bot.UserID) || invalidStableKey(bot.StableKey) || !filepath.IsAbs(bot.TokenFile) {
			return Manifest{}, nil, errors.New("mattermost bot identity is invalid")
		}
		if _, exists := result.bots[bot.StableKey]; exists {
			return Manifest{}, nil, errors.New("mattermost bot identity is duplicated")
		}
		result.bots[bot.StableKey] = bot
		result.botUsers[bot.UserID] = struct{}{}
	}
	teamProjects, projectTeams := map[string]string{}, map[string]string{}
	channelChats, chatChannels := map[string]string{}, map[string]string{}
	for _, channel := range manifest.Channels {
		channel.RuntimeKey = uuid.NewSHA1(runtimeTemplateNamespace, []byte(strings.Join([]string{
			channel.OrganizationID, channel.ProjectID, channel.ChatID, channel.RoleID, channel.SessionID,
			channel.Locale, channel.BotStableKey, channel.LifecycleActorID,
		}, "\x00"))).String()
		locale, localeOK := i18n.ResolveLocale(channel.Locale)
		_, botOK := result.bots[channel.BotStableKey]
		if invalidProviderID(channel.TeamID) || invalidProviderID(channel.ChannelID) ||
			uuid.Validate(channel.OrganizationID) != nil || uuid.Validate(channel.ProjectID) != nil ||
			uuid.Validate(channel.ChatID) != nil || uuid.Validate(channel.RoleID) != nil ||
			uuid.Validate(channel.LifecycleActorID) != nil ||
			(channel.SessionID != "" && uuid.Validate(channel.SessionID) != nil) || !localeOK || !botOK {
			return Manifest{}, nil, errors.New("mattermost channel mapping is invalid")
		}
		channel.Locale = locale
		key := channel.TeamID + "\x00" + channel.ChannelID
		if _, exists := result.channels[key]; exists {
			return Manifest{}, nil, errors.New("mattermost channel mapping is duplicated")
		}
		projectScope := channel.OrganizationID + "\x00" + channel.ProjectID
		if existing, exists := teamProjects[channel.TeamID]; exists && existing != projectScope {
			return Manifest{}, nil, errors.New("mattermost team mapping is ambiguous")
		}
		if existing, exists := projectTeams[projectScope]; exists && existing != channel.TeamID {
			return Manifest{}, nil, errors.New("mattermost project mapping is ambiguous")
		}
		chatScope := projectScope + "\x00" + channel.ChatID
		if existing, exists := channelChats[channel.ChannelID]; exists && existing != chatScope {
			return Manifest{}, nil, errors.New("mattermost channel chat mapping is ambiguous")
		}
		if existing, exists := chatChannels[chatScope]; exists && existing != channel.ChannelID {
			return Manifest{}, nil, errors.New("mattermost chat mapping is ambiguous")
		}
		teamProjects[channel.TeamID], projectTeams[projectScope] = projectScope, channel.TeamID
		channelChats[channel.ChannelID], chatChannels[chatScope] = chatScope, channel.ChannelID
		result.channels[key] = channel
		if _, exists := result.templates[channel.RuntimeKey]; exists {
			return Manifest{}, nil, errors.New("mattermost runtime route template is duplicated")
		}
		result.templates[channel.RuntimeKey] = channel
		for _, assignment := range channel.Assignments {
			_, assignmentBotOK := result.bots[assignment.BotStableKey]
			if invalidUsername(assignment.MentionUsername) || invalidProviderID(assignment.MattermostUserID) ||
				uuid.Validate(assignment.RoleID) != nil || !assignmentBotOK {
				return Manifest{}, nil, errors.New("mattermost agent assignment is invalid")
			}
			assignmentKey := key + "\x00" + strings.ToLower(assignment.MentionUsername)
			if _, exists := result.assignments[assignmentKey]; exists {
				return Manifest{}, nil, errors.New("mattermost agent assignment is ambiguous")
			}
			result.assignments[assignmentKey] = assignment
		}
	}
	for _, actor := range manifest.Actors {
		if invalidProviderID(actor.MattermostUserID) || uuid.Validate(actor.ActorID) != nil ||
			uuid.Validate(actor.OrganizationID) != nil || uuid.Validate(actor.ProjectID) != nil {
			return Manifest{}, nil, errors.New("mattermost actor mapping is invalid")
		}
		key := actor.MattermostUserID + "\x00" + actor.OrganizationID + "\x00" + actor.ProjectID
		if _, exists := result.actors[key]; exists {
			return Manifest{}, nil, errors.New("mattermost actor mapping is duplicated")
		}
		result.actors[key] = actor
	}
	ownerDeliveries := map[string]struct{}{}
	for _, channel := range result.channels {
		if !channel.OwnerDelivery {
			continue
		}
		for _, actor := range result.actors {
			if actor.ProjectID != channel.ProjectID || actor.OrganizationID != channel.OrganizationID {
				continue
			}
			key := channel.ProjectID + "\x00" + actor.ActorID
			if _, exists := ownerDeliveries[key]; exists {
				return Manifest{}, nil, errors.New("mattermost owner delivery route is ambiguous")
			}
			ownerDeliveries[key] = struct{}{}
		}
	}
	for _, channel := range result.channels {
		ownerFound := false
		for _, actor := range result.actors {
			if actor.ActorID == channel.LifecycleActorID && actor.OrganizationID == channel.OrganizationID &&
				actor.ProjectID == channel.ProjectID {
				ownerFound = true
				break
			}
		}
		if !ownerFound {
			return Manifest{}, nil, errors.New("mattermost mapping owner is outside the server-owned actor scope")
		}
	}
	if len(ownerDeliveries) == 0 {
		return Manifest{}, nil, errors.New("mattermost owner delivery route is missing")
	}
	return manifest, result, nil
}

func (current *index) resolveAssignment(teamID, channelID, username, userID string) (AgentAssignment, error) {
	assignment, ok := current.assignments[teamID+"\x00"+channelID+"\x00"+strings.ToLower(username)]
	if !ok || assignment.MattermostUserID != userID {
		return AgentAssignment{}, errors.New("mattermost agent assignment is unauthorized")
	}
	return assignment, nil
}

func (current *index) assigned(teamID, channelID, username string) (AgentAssignment, bool) {
	assignment, ok := current.assignments[teamID+"\x00"+channelID+"\x00"+strings.ToLower(username)]
	return assignment, ok
}

func (current *index) channelBoundaries() []entity.Boundary {
	values := make([]entity.Boundary, 0, len(current.channels))
	for _, binding := range current.channels {
		values = append(values, entity.Boundary{
			OrganizationID: binding.OrganizationID, ProjectID: binding.ProjectID,
			ChatID: binding.ChatID, MappingOwnerActorID: binding.LifecycleActorID,
			TeamID: binding.TeamID, ChannelID: binding.ChannelID,
		})
	}
	slices.SortFunc(values, func(left, right entity.Boundary) int { return strings.Compare(left.ChannelID, right.ChannelID) })
	return values
}

func (current *index) resolveOwner(principal entity.TeamPrincipal) (ActorBinding, error) {
	var resolved ActorBinding
	for _, actor := range current.actors {
		if actor.ActorID != principal.ActorID || actor.OrganizationID != principal.OrganizationID || actor.ProjectID != principal.ProjectID {
			continue
		}
		if resolved.ActorID != "" {
			return ActorBinding{}, errors.New("mattermost owner mapping is ambiguous")
		}
		resolved = actor
	}
	if resolved.ActorID == "" {
		return ActorBinding{}, errors.New("mattermost owner is outside the server-owned mapping")
	}
	return resolved, nil
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

func invalidUsername(value string) bool {
	if len(value) < 1 || len(value) > 64 || value != strings.ToLower(value) {
		return true
	}
	for _, symbol := range value {
		if (symbol < 'a' || symbol > 'z') && (symbol < '0' || symbol > '9') && symbol != '.' && symbol != '_' && symbol != '-' {
			return true
		}
	}
	return false
}
