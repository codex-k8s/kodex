package mattermost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	domainmattermost "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/mattermost"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/enum"
	model "github.com/mattermost/mattermost/server/public/model"
)

const (
	maximumCatalogBots   = 10000
	maximumCatalogTokens = 10000
	providerTokenPage    = 100
)

var requiredBotIdentityPermissions = []string{
	model.PermissionAddUserToTeam.Id,
	model.PermissionCreateBot.Id,
	model.PermissionCreateUserAccessToken.Id,
	model.PermissionManageBots.Id,
	model.PermissionReadBots.Id,
	model.PermissionReadUserAccessToken.Id,
	model.PermissionRevokeUserAccessToken.Id,
	model.PermissionViewMembers.Id,
	model.PermissionViewTeam.Id,
}

var errBotIdentityPermissionProfile = errors.New("mattermost bot identity permission profile is not ready")

func (client *Client) CheckBotIdentityLifecycle(ctx context.Context) error {
	if client.primary == nil {
		return errors.New("mattermost bot identity administrator is unavailable")
	}
	_, response, err := client.primary.api.GetBots(ctx, 0, 1, "")
	if err != nil || response == nil {
		return errors.New("mattermost bot identity catalog working path is not ready")
	}
	return nil
}

// CheckBotIdentityPermissions без mutation доказывает тот же effective role
// path, от которого Mattermost зависит для create, Team membership, token
// lifecycle, bot readback и revoke. Роли читаются для exact authenticated
// provider owner и exact Team; одного успешного GetBots для этого недостаточно.
func (client *Client) CheckBotIdentityPermissions(ctx context.Context, principal entity.TeamPrincipal,
	providerTeamID string,
) error {
	if client.primary == nil || client.index == nil || invalidProviderID(providerTeamID) {
		return errBotIdentityPermissionProfile
	}
	owner, err := client.index.resolveOwner(principal)
	if err != nil {
		return errBotIdentityPermissionProfile
	}
	user, response, err := client.primary.api.GetMe(ctx, "")
	if err != nil || response == nil || user == nil || user.Id != owner.MattermostUserID ||
		user.DeleteAt != 0 || user.IsBot {
		return errBotIdentityPermissionProfile
	}
	member, response, err := client.primary.api.GetTeamMember(ctx, providerTeamID, user.Id, "")
	if err != nil || response == nil || member == nil || member.TeamId != providerTeamID ||
		member.UserId != user.Id || member.DeleteAt != 0 {
		return errBotIdentityPermissionProfile
	}
	config, response, err := client.primary.api.GetConfig(ctx)
	if err != nil || response == nil || !botIdentityFeaturesReady(config) {
		return errBotIdentityPermissionProfile
	}
	roleNames := append(user.GetRoles(), member.GetRoles()...)
	slices.Sort(roleNames)
	roleNames = slices.Compact(roleNames)
	if len(roleNames) == 0 {
		return errBotIdentityPermissionProfile
	}
	roles, response, err := client.primary.api.GetRolesByNames(ctx, roleNames)
	if err != nil || response == nil || !botIdentityPermissionProfile(roleNames, roles) {
		return errBotIdentityPermissionProfile
	}
	return nil
}

func botIdentityFeaturesReady(config *model.Config) bool {
	return config != nil && config.ServiceSettings.EnableBotAccountCreation != nil &&
		*config.ServiceSettings.EnableBotAccountCreation && config.ServiceSettings.EnableUserAccessTokens != nil &&
		*config.ServiceSettings.EnableUserAccessTokens
}

func botIdentityPermissionProfile(roleNames []string, roles []*model.Role) bool {
	expectedRoles := make(map[string]struct{}, len(roleNames))
	for _, name := range roleNames {
		if name == "" {
			return false
		}
		expectedRoles[name] = struct{}{}
	}
	if len(expectedRoles) != len(roleNames) || len(roles) != len(roleNames) {
		return false
	}
	permissions := make(map[string]struct{}, len(requiredBotIdentityPermissions))
	observedRoles := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		if role == nil {
			return false
		}
		if _, expected := expectedRoles[role.Name]; !expected {
			return false
		}
		if _, duplicate := observedRoles[role.Name]; duplicate {
			return false
		}
		observedRoles[role.Name] = struct{}{}
		for _, permission := range role.Permissions {
			permissions[permission] = struct{}{}
		}
	}
	for _, required := range requiredBotIdentityPermissions {
		if _, allowed := permissions[required]; !allowed {
			return false
		}
	}
	return true
}

func (client *Client) ListBotIdentities(ctx context.Context, principal entity.TeamPrincipal,
	providerTeamID string, offset, limit uint32,
) ([]entity.AgentMattermostBotIdentity, bool, error) {
	owner, ownerErr := client.index.resolveOwner(principal)
	if ownerErr != nil || invalidProviderID(providerTeamID) ||
		limit == 0 || limit > 100 || offset > maximumCatalogBots {
		return nil, false, domainmattermost.ErrBotForbidden
	}
	page := int(offset / limit)
	bots, response, err := client.primary.api.GetBots(ctx, page, int(limit), "")
	if err != nil {
		return nil, false, botProviderReadError(response, err)
	}
	if len(bots) > int(limit) {
		return nil, false, domainmattermost.ErrBotConflict
	}
	result := make([]entity.AgentMattermostBotIdentity, 0, min(len(bots), int(limit)))
	for _, bot := range bots[:min(len(bots), int(limit))] {
		if bot == nil || invalidProviderID(bot.UserId) || bot.DeleteAt != 0 ||
			bot.OwnerId != owner.MattermostUserID {
			continue
		}
		member, memberResponse, memberErr := client.primary.api.GetTeamMember(ctx, providerTeamID, bot.UserId, "")
		if memberErr != nil {
			if memberResponse != nil && memberResponse.StatusCode == http.StatusNotFound {
				continue
			}
			return nil, false, botProviderReadError(memberResponse, memberErr)
		}
		if member == nil || member.TeamId != providerTeamID || member.UserId != bot.UserId || member.DeleteAt != 0 {
			continue
		}
		result = append(result, safeBotIdentity(bot, providerTeamID))
	}
	slices.SortFunc(result, func(left, right entity.AgentMattermostBotIdentity) int {
		return strings.Compare(left.Username, right.Username)
	})
	return result, len(bots) == int(limit), nil
}

func (client *Client) CreateBotIdentity(ctx context.Context, principal entity.TeamPrincipal,
	intent entity.AgentMattermostBotCreateIntent, providerTeamID string,
) (entity.AgentMattermostBotIdentity, error) {
	owner, err := client.index.resolveOwner(principal)
	if err != nil || invalidProviderID(providerTeamID) {
		return entity.AgentMattermostBotIdentity{}, domainmattermost.ErrBotForbidden
	}
	if existing, response, readErr := client.primary.api.GetUserByUsername(ctx, intent.Username, ""); readErr == nil && existing != nil {
		return entity.AgentMattermostBotIdentity{}, domainmattermost.ErrBotConflict
	} else if readErr != nil && (response == nil || response.StatusCode != http.StatusNotFound) {
		return entity.AgentMattermostBotIdentity{}, botProviderReadError(response, readErr)
	}
	created, response, err := client.primary.api.CreateBot(ctx, &model.Bot{
		Username: intent.Username, DisplayName: intent.DisplayName,
		Description: botOperationMarker(intent.ProviderCorrelation), OwnerId: owner.MattermostUserID,
	})
	if err != nil {
		if response != nil && response.StatusCode >= 400 && response.StatusCode < 500 {
			return entity.AgentMattermostBotIdentity{}, domainmattermost.ErrBotConflict
		}
		return entity.AgentMattermostBotIdentity{}, domainmattermost.ErrBotAmbiguousEffect
	}
	if !createdBotMatches(created, intent, owner.MattermostUserID) {
		return entity.AgentMattermostBotIdentity{}, domainmattermost.ErrBotAmbiguousEffect
	}
	return client.readCreatedBot(ctx, principal, intent, providerTeamID)
}

func (client *Client) RecoverCreatedBotIdentity(ctx context.Context, principal entity.TeamPrincipal,
	intent entity.AgentMattermostBotCreateIntent, providerTeamID string,
) (entity.AgentMattermostBotIdentity, error) {
	return client.readCreatedBot(ctx, principal, intent, providerTeamID)
}

func (client *Client) readCreatedBot(ctx context.Context, principal entity.TeamPrincipal,
	intent entity.AgentMattermostBotCreateIntent, providerTeamID string,
) (entity.AgentMattermostBotIdentity, error) {
	owner, err := client.index.resolveOwner(principal)
	if err != nil {
		return entity.AgentMattermostBotIdentity{}, domainmattermost.ErrBotForbidden
	}
	user, response, err := client.primary.api.GetUserByUsername(ctx, intent.Username, "")
	if err != nil {
		if response != nil && response.StatusCode == http.StatusNotFound {
			return entity.AgentMattermostBotIdentity{}, domainmattermost.ErrBotNotFound
		}
		return entity.AgentMattermostBotIdentity{}, botProviderReadError(response, err)
	}
	if user == nil || invalidProviderID(user.Id) || !user.IsBot || user.DeleteAt != 0 {
		return entity.AgentMattermostBotIdentity{}, domainmattermost.ErrBotConflict
	}
	bot, response, err := client.primary.api.GetBotIncludeDeleted(ctx, user.Id, "")
	if err != nil {
		return entity.AgentMattermostBotIdentity{}, botProviderReadError(response, err)
	}
	if !createdBotMatches(bot, intent, owner.MattermostUserID) {
		return entity.AgentMattermostBotIdentity{}, domainmattermost.ErrBotConflict
	}
	result := safeBotIdentity(bot, providerTeamID)
	result.ProviderCausalitySHA256 = botCausalityDigest(intent.ProviderCorrelation, bot)
	return result, nil
}

func (client *Client) ReadBotIdentity(ctx context.Context, principal entity.TeamPrincipal,
	providerUserID, providerTeamID string,
) (entity.AgentMattermostBotIdentity, error) {
	owner, ownerErr := client.index.resolveOwner(principal)
	if ownerErr != nil || invalidProviderID(providerUserID) || invalidProviderID(providerTeamID) {
		return entity.AgentMattermostBotIdentity{}, domainmattermost.ErrBotForbidden
	}
	bot, response, err := client.primary.api.GetBotIncludeDeleted(ctx, providerUserID, "")
	if err != nil {
		if response != nil && response.StatusCode == http.StatusNotFound {
			return entity.AgentMattermostBotIdentity{}, domainmattermost.ErrBotNotFound
		}
		return entity.AgentMattermostBotIdentity{}, botProviderReadError(response, err)
	}
	if bot == nil || bot.UserId != providerUserID || bot.OwnerId != owner.MattermostUserID {
		return entity.AgentMattermostBotIdentity{}, domainmattermost.ErrBotConflict
	}
	identity := safeBotIdentity(bot, providerTeamID)
	if bot.DeleteAt != 0 {
		return identity, nil
	}
	member, response, err := client.primary.api.GetTeamMember(ctx, providerTeamID, providerUserID, "")
	if err != nil || member == nil || member.TeamId != providerTeamID || member.UserId != providerUserID || member.DeleteAt != 0 {
		return entity.AgentMattermostBotIdentity{}, botProviderReadError(response, err)
	}
	return identity, nil
}

func (client *Client) EnsureBotTeamMembership(ctx context.Context, principal entity.TeamPrincipal,
	identity entity.AgentMattermostBotIdentity,
) (entity.AgentMattermostBotIdentity, error) {
	current, err := client.readOwnedBotWithoutTeam(ctx, principal, identity.ProviderUserID, identity.ProviderTeamID)
	if err != nil || !sameBotPredecessor(identity, current) {
		return entity.AgentMattermostBotIdentity{}, domainmattermost.ErrBotConflict
	}
	member, response, err := client.primary.api.GetTeamMember(ctx, identity.ProviderTeamID, identity.ProviderUserID, "")
	if err != nil && (response == nil || response.StatusCode != http.StatusNotFound) {
		return entity.AgentMattermostBotIdentity{}, botProviderReadError(response, err)
	}
	if member == nil || member.TeamId != identity.ProviderTeamID || member.UserId != identity.ProviderUserID || member.DeleteAt != 0 {
		if _, response, err = client.primary.api.AddTeamMember(ctx, identity.ProviderTeamID, identity.ProviderUserID); err != nil {
			return entity.AgentMattermostBotIdentity{}, botProviderMutationError(response, err)
		}
	}
	return client.ReadBotIdentity(ctx, principal, identity.ProviderUserID, identity.ProviderTeamID)
}

func (client *Client) readOwnedBotWithoutTeam(ctx context.Context, principal entity.TeamPrincipal,
	providerUserID, providerTeamID string,
) (entity.AgentMattermostBotIdentity, error) {
	owner, ownerErr := client.index.resolveOwner(principal)
	if ownerErr != nil || invalidProviderID(providerUserID) || invalidProviderID(providerTeamID) {
		return entity.AgentMattermostBotIdentity{}, domainmattermost.ErrBotForbidden
	}
	bot, response, err := client.primary.api.GetBotIncludeDeleted(ctx, providerUserID, "")
	if err != nil {
		return entity.AgentMattermostBotIdentity{}, botProviderReadError(response, err)
	}
	if bot == nil || bot.UserId != providerUserID || bot.OwnerId != owner.MattermostUserID || bot.DeleteAt != 0 {
		return entity.AgentMattermostBotIdentity{}, domainmattermost.ErrBotConflict
	}
	return safeBotIdentity(bot, providerTeamID), nil
}

func (client *Client) CreateBotAccessToken(ctx context.Context, principal entity.TeamPrincipal,
	identity entity.AgentMattermostBotIdentity,
	correlation string,
) (string, string, error) {
	if err := client.requireOwnedBotPredecessor(ctx, principal, identity); err != nil {
		return "", "", err
	}
	description := botTokenDescription(correlation)
	token, response, err := client.primary.api.CreateUserAccessToken(ctx, identity.ProviderUserID, description)
	if err != nil {
		return "", "", botProviderMutationError(response, err)
	}
	if token == nil || invalidProviderID(token.Id) || token.UserId != identity.ProviderUserID ||
		token.Description != description || !token.IsActive || token.Token == "" {
		return "", "", domainmattermost.ErrBotAmbiguousEffect
	}
	if err := client.requireOwnedBotPredecessor(ctx, principal, identity); err != nil {
		// Exact новый token закрывается до выдачи material за provider boundary.
		// Неуспешная компенсация остаётся неоднозначным исходом.
		response, revokeErr := client.primary.api.RevokeUserAccessToken(ctx, token.Id)
		if revokeErr != nil {
			return "", "", botProviderMutationError(response, revokeErr)
		}
		readback, response, readErr := client.primary.api.GetUserAccessToken(ctx, token.Id)
		if readErr != nil || readback == nil || readback.UserId != identity.ProviderUserID || readback.IsActive {
			return "", "", botProviderReadError(response, readErr)
		}
		return "", "", domainmattermost.ErrBotConflict
	}
	return token.Id, token.Token, nil
}

// ResolveBotAccessToken доказывает exact operation token без mutation. Этот
// read path связывает уже материализованный Vault secret с provider token ID
// после crash между Vault CAS и PostgreSQL checkpoint.
func (client *Client) ResolveBotAccessToken(ctx context.Context, principal entity.TeamPrincipal,
	identity entity.AgentMattermostBotIdentity,
	correlation string,
) (string, bool, error) {
	if invalidProviderID(identity.ProviderUserID) || correlation == "" || len(correlation) > 128 ||
		strings.ContainsAny(correlation, "\x00\r\n") {
		return "", false, domainmattermost.ErrBotForbidden
	}
	if err := client.requireOwnedBotPredecessor(ctx, principal, identity); err != nil {
		return "", false, err
	}
	description := botTokenDescription(correlation)
	var found *model.UserAccessToken
	for offset := 0; offset < maximumCatalogTokens; offset += providerTokenPage {
		tokens, response, err := client.primary.api.GetUserAccessTokensForUser(ctx, identity.ProviderUserID,
			offset/providerTokenPage, providerTokenPage)
		if err != nil {
			return "", false, botProviderReadError(response, err)
		}
		for _, candidate := range tokens {
			if candidate == nil || candidate.UserId != identity.ProviderUserID || candidate.Description != description {
				continue
			}
			if found != nil && found.Id != candidate.Id {
				return "", false, domainmattermost.ErrBotConflict
			}
			found = candidate
		}
		if len(tokens) < providerTokenPage {
			break
		}
		if offset+providerTokenPage == maximumCatalogTokens {
			return "", false, domainmattermost.ErrBotConflict
		}
	}
	if found == nil {
		return "", false, nil
	}
	if invalidProviderID(found.Id) {
		return "", false, domainmattermost.ErrBotConflict
	}
	return found.Id, found.IsActive, nil
}

// RecoverBotAccessToken закрывает точный operation token, секрет которого мог
// быть потерян после provider accept. Только после этого service может создать
// новую credential attempt; два активных token одного operation запрещены.
func (client *Client) RecoverBotAccessToken(ctx context.Context, principal entity.TeamPrincipal,
	identity entity.AgentMattermostBotIdentity,
	correlation string,
) (string, bool, error) {
	tokenID, active, err := client.ResolveBotAccessToken(ctx, principal, identity, correlation)
	if err != nil || tokenID == "" {
		return "", false, err
	}
	if !active {
		return tokenID, false, nil
	}
	if response, revokeErr := client.primary.api.RevokeUserAccessToken(ctx, tokenID); revokeErr != nil {
		return "", false, botProviderMutationError(response, revokeErr)
	}
	readback, response, readErr := client.primary.api.GetUserAccessToken(ctx, tokenID)
	if readErr != nil || readback == nil || readback.Id != tokenID ||
		readback.UserId != identity.ProviderUserID || readback.IsActive {
		return "", false, botProviderReadError(response, readErr)
	}
	if err := client.requireOwnedBotPredecessor(ctx, principal, identity); err != nil {
		return "", false, domainmattermost.ErrBotAmbiguousEffect
	}
	return tokenID, true, nil
}

func (client *Client) RevokeBotAccessToken(ctx context.Context,
	principal entity.TeamPrincipal,
	identity entity.AgentMattermostBotIdentity,
) (bool, error) {
	if invalidProviderID(identity.ProviderUserID) || invalidProviderID(identity.ProviderTokenID) {
		return false, domainmattermost.ErrBotForbidden
	}
	if err := client.requireOwnedBotPredecessor(ctx, principal, identity); err != nil {
		return false, err
	}
	token, response, err := client.primary.api.GetUserAccessToken(ctx, identity.ProviderTokenID)
	if err != nil {
		return false, botProviderReadError(response, err)
	}
	if token == nil || token.Id != identity.ProviderTokenID || token.UserId != identity.ProviderUserID {
		return false, domainmattermost.ErrBotConflict
	}
	if !token.IsActive {
		return false, nil
	}
	if response, err := client.primary.api.RevokeUserAccessToken(ctx, identity.ProviderTokenID); err != nil {
		return false, botProviderMutationError(response, err)
	}
	readback, response, err := client.primary.api.GetUserAccessToken(ctx, identity.ProviderTokenID)
	if err != nil || readback == nil || readback.UserId != identity.ProviderUserID || readback.IsActive {
		return false, botProviderReadError(response, err)
	}
	if err := client.requireOwnedBotPredecessor(ctx, principal, identity); err != nil {
		return false, domainmattermost.ErrBotAmbiguousEffect
	}
	return true, nil
}

func (client *Client) RevokeBotIdentity(ctx context.Context, principal entity.TeamPrincipal,
	identity entity.AgentMattermostBotIdentity,
) (entity.AgentMattermostBotIdentity, bool, error) {
	if _, err := client.index.resolveOwner(principal); err != nil {
		return entity.AgentMattermostBotIdentity{}, false, domainmattermost.ErrBotForbidden
	}
	current, err := client.ReadBotIdentity(ctx, principal, identity.ProviderUserID, identity.ProviderTeamID)
	if err != nil {
		return entity.AgentMattermostBotIdentity{}, false, err
	}
	if current.Status == enum.AgentBotIdentityRevoked {
		return current, false, nil
	}
	if !sameBotPredecessor(identity, current) {
		return entity.AgentMattermostBotIdentity{}, false, domainmattermost.ErrBotConflict
	}
	if current.Status != enum.AgentBotIdentityAvailable {
		return entity.AgentMattermostBotIdentity{}, false, domainmattermost.ErrBotConflict
	}
	bot, response, err := client.primary.api.DisableBot(ctx, identity.ProviderUserID)
	if err != nil {
		return entity.AgentMattermostBotIdentity{}, false, botProviderMutationError(response, err)
	}
	if bot == nil || bot.UserId != identity.ProviderUserID || bot.DeleteAt == 0 {
		return entity.AgentMattermostBotIdentity{}, false, domainmattermost.ErrBotAmbiguousEffect
	}
	result, err := client.ReadBotIdentity(ctx, principal, identity.ProviderUserID, identity.ProviderTeamID)
	if err != nil || result.Status != enum.AgentBotIdentityRevoked {
		return entity.AgentMattermostBotIdentity{}, false, domainmattermost.ErrBotAmbiguousEffect
	}
	return result, true, nil
}

func (client *Client) VerifyRuntimeBotCredential(ctx context.Context, principal entity.TeamPrincipal,
	identity entity.AgentMattermostBotIdentity, token string,
) error {
	if token == "" || identity.Status != enum.AgentBotIdentityAvailable {
		return domainmattermost.ErrBotForbidden
	}
	if err := client.requireOwnedBotPredecessor(ctx, principal, identity); err != nil {
		return err
	}
	if invalidProviderID(identity.ProviderTokenID) {
		return domainmattermost.ErrBotForbidden
	}
	providerToken, response, err := client.primary.api.GetUserAccessToken(ctx, identity.ProviderTokenID)
	if err != nil {
		return botProviderReadError(response, err)
	}
	if providerToken == nil || providerToken.Id != identity.ProviderTokenID ||
		providerToken.UserId != identity.ProviderUserID || !providerToken.IsActive {
		return domainmattermost.ErrBotConflict
	}
	api := model.NewAPIv4Client(client.config.SiteURL)
	api.AuthToken, api.AuthType, api.HTTPClient = token, model.HeaderBearer, client.httpClient
	user, response, err := api.GetMe(ctx, "")
	if err != nil {
		return botProviderReadError(response, err)
	}
	if user == nil || user.Id != identity.ProviderUserID || !user.IsBot || user.DeleteAt != 0 {
		return domainmattermost.ErrBotConflict
	}
	return nil
}

func (client *Client) requireOwnedBotPredecessor(ctx context.Context, principal entity.TeamPrincipal,
	identity entity.AgentMattermostBotIdentity,
) error {
	current, err := client.ReadBotIdentity(ctx, principal, identity.ProviderUserID, identity.ProviderTeamID)
	if err != nil {
		return err
	}
	if !sameBotPredecessor(identity, current) {
		return domainmattermost.ErrBotConflict
	}
	return nil
}

func sameBotPredecessor(expected, current entity.AgentMattermostBotIdentity) bool {
	return expected.ProviderUserID != "" && expected.ProviderUserID == current.ProviderUserID &&
		expected.ProviderTeamID != "" && expected.ProviderTeamID == current.ProviderTeamID &&
		expected.ProviderSnapshotSHA256 != "" && expected.ProviderSnapshotSHA256 == current.ProviderSnapshotSHA256 &&
		current.Status == enum.AgentBotIdentityAvailable
}

func safeBotIdentity(bot *model.Bot, providerTeamID string) entity.AgentMattermostBotIdentity {
	status := enum.AgentBotIdentityAvailable
	if bot.DeleteAt != 0 {
		status = enum.AgentBotIdentityRevoked
	}
	observedAt := time.Now().UTC()
	version := uint64(max(bot.CreateAt, bot.UpdateAt, bot.DeleteAt))
	if version == 0 {
		version = 1
	}
	snapshot := strings.Join([]string{
		"mattermost-bot-snapshot-v1", bot.UserId, bot.Username,
		bot.DisplayName, bot.Description, bot.OwnerId, fmt.Sprint(bot.CreateAt), fmt.Sprint(bot.UpdateAt),
		fmt.Sprint(bot.DeleteAt), providerTeamID,
	}, "\x00")
	digest := sha256.Sum256([]byte(snapshot))
	return entity.AgentMattermostBotIdentity{
		ProviderBotID: bot.UserId, ProviderUserID: bot.UserId, ProviderTeamID: providerTeamID,
		Username: bot.Username, DisplayName: bot.DisplayName, Status: status, ProviderVersion: version,
		ProviderSnapshotSHA256: hex.EncodeToString(digest[:]), ObservedAt: observedAt,
		CreatedAt: time.UnixMilli(bot.CreateAt).UTC(), UpdatedAt: time.UnixMilli(max(bot.UpdateAt, bot.DeleteAt)).UTC(),
	}
}

func createdBotMatches(bot *model.Bot, intent entity.AgentMattermostBotCreateIntent, ownerID string) bool {
	return bot != nil && !invalidProviderID(bot.UserId) && bot.Username == intent.Username &&
		bot.DisplayName == intent.DisplayName && bot.Description == botOperationMarker(intent.ProviderCorrelation) &&
		bot.OwnerId == ownerID && bot.DeleteAt == 0
}

func botOperationMarker(correlation string) string {
	return "mattercodex-agent-bot-operation:" + correlation
}

func botTokenDescription(correlation string) string {
	return "mattercodex-agent-bot-token:" + correlation
}

func botCausalityDigest(correlation string, bot *model.Bot) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		"mattermost-agent-bot-create-proof-v1",
		correlation, bot.UserId, fmt.Sprint(bot.CreateAt),
	}, "\x00")))
	return hex.EncodeToString(digest[:])
}

func botProviderReadError(response *model.Response, err error) error {
	if err == nil {
		return domainmattermost.ErrBotConflict
	}
	if response == nil {
		return domainmattermost.ErrBotAmbiguousEffect
	}
	switch response.StatusCode {
	case http.StatusNotFound:
		return domainmattermost.ErrBotNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		return domainmattermost.ErrBotForbidden
	default:
		return domainmattermost.ErrBotAmbiguousEffect
	}
}

func botProviderMutationError(response *model.Response, err error) error {
	if err == nil {
		return domainmattermost.ErrBotConflict
	}
	if response != nil && response.StatusCode >= 400 && response.StatusCode < 500 {
		return domainmattermost.ErrBotForbidden
	}
	return domainmattermost.ErrBotAmbiguousEffect
}
