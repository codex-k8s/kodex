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
	maximumCatalogTeams          = 10000
	teamMembershipNotReadyDetail = "mattermost team membership working path is not ready"
)

func (client *Client) CheckTeamLifecycle(ctx context.Context) error {
	seenProjects := map[string]struct{}{}
	for _, actor := range client.manifest.Actors {
		projectScope := actor.OrganizationID + "\x00" + actor.ProjectID
		if _, checked := seenProjects[projectScope]; checked {
			continue
		}
		seenProjects[projectScope] = struct{}{}
		user, _, err := client.primary.api.GetUser(ctx, actor.MattermostUserID, "")
		if err != nil || user == nil || user.Id != actor.MattermostUserID || user.DeleteAt != 0 {
			return errors.New("mattermost team owner readback working path is not ready")
		}
		teams, _, err := client.primary.api.GetTeamsForUser(ctx, actor.MattermostUserID, "")
		if err != nil || len(teams) > maximumCatalogTeams {
			return errors.New("mattermost team catalog working path is not ready")
		}
		if len(teams) == 0 {
			continue
		}
		membershipReadback := false
		for _, team := range teams {
			if team == nil || invalidProviderID(team.Id) || team.DeleteAt != 0 {
				continue
			}
			member, _, err := client.primary.api.GetTeamMember(ctx, team.Id, actor.MattermostUserID, "")
			if err != nil || member == nil || member.TeamId != team.Id ||
				member.UserId != actor.MattermostUserID || member.DeleteAt != 0 {
				return errors.New(teamMembershipNotReadyDetail)
			}
			membershipReadback = true
			break
		}
		if !membershipReadback {
			return errors.New(teamMembershipNotReadyDetail)
		}
	}
	if len(seenProjects) == 0 {
		return errors.New("mattermost team readiness owner mapping is missing")
	}
	return nil
}

func (client *Client) TeamReadinessBindings() []entity.MattermostReadinessBinding {
	seen := map[string]struct{}{}
	result := make([]entity.MattermostReadinessBinding, 0, len(client.manifest.Channels))
	for _, channel := range client.manifest.Channels {
		key := channel.OrganizationID + "\x00" + channel.ProjectID + "\x00" + channel.LifecycleActorID
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, entity.MattermostReadinessBinding{Principal: entity.TeamPrincipal{
			ActorID: channel.LifecycleActorID, OrganizationID: channel.OrganizationID, ProjectID: channel.ProjectID,
		}})
	}
	slices.SortFunc(result, func(left, right entity.MattermostReadinessBinding) int {
		leftKey := left.Principal.ProjectID + "\x00" + left.Principal.ActorID
		rightKey := right.Principal.ProjectID + "\x00" + right.Principal.ActorID
		return strings.Compare(leftKey, rightKey)
	})
	return result
}

func (client *Client) ReadOwner(ctx context.Context, principal entity.TeamPrincipal) (entity.MattermostOwnerObservation, error) {
	actor, err := client.index.resolveOwner(principal)
	if err != nil {
		return entity.MattermostOwnerObservation{}, domainmattermost.ErrTeamForbidden
	}
	user, response, err := client.primary.api.GetUser(ctx, actor.MattermostUserID, "")
	if err != nil || user == nil || user.Id != actor.MattermostUserID || user.DeleteAt != 0 {
		return entity.MattermostOwnerObservation{}, providerReadError(response, err)
	}
	observedAt := time.Now().UTC()
	value := strings.Join([]string{
		"mattermost-owner-snapshot-v1", user.Id,
		time.UnixMilli(user.CreateAt).UTC().Format(time.RFC3339Nano),
		time.UnixMilli(user.UpdateAt).UTC().Format(time.RFC3339Nano),
		time.UnixMilli(user.DeleteAt).UTC().Format(time.RFC3339Nano),
	}, "\x00")
	digest := sha256.Sum256([]byte(value))
	return entity.MattermostOwnerObservation{
		ProviderObjectRef: user.Id,
		SnapshotSHA256:    hex.EncodeToString(digest[:]), ObservedAt: observedAt,
	}, nil
}

func (client *Client) ListTeams(ctx context.Context, principal entity.TeamPrincipal, offset, limit uint32) ([]entity.MattermostTeam, bool, error) {
	actor, err := client.index.resolveOwner(principal)
	if err != nil {
		return nil, false, domainmattermost.ErrTeamForbidden
	}
	teams, response, err := client.primary.api.GetTeamsForUser(ctx, actor.MattermostUserID, "")
	if err != nil {
		return nil, false, providerReadError(response, err)
	}
	if len(teams) > maximumCatalogTeams || limit == 0 || offset > maximumCatalogTeams {
		return nil, false, domainmattermost.ErrTeamConflict
	}
	slices.SortFunc(teams, func(left, right *model.Team) int {
		if left == nil {
			return 1
		}
		if right == nil {
			return -1
		}
		return strings.Compare(left.Name, right.Name)
	})
	active := make([]*model.Team, 0, len(teams))
	for _, listed := range teams {
		if listed == nil || listed.DeleteAt != 0 || invalidProviderID(listed.Id) {
			continue
		}
		active = append(active, listed)
	}
	if int(offset) >= len(active) {
		return []entity.MattermostTeam{}, false, nil
	}
	end := min(int(offset+limit), len(active))
	result := make([]entity.MattermostTeam, 0, end-int(offset))
	for _, listed := range active[offset:end] {
		fresh, _, readErr := client.primary.api.GetTeam(ctx, listed.Id, "")
		if readErr != nil || fresh == nil || fresh.Id != listed.Id || fresh.DeleteAt != 0 {
			return nil, false, domainmattermost.ErrTeamForbidden
		}
		member, _, memberErr := client.primary.api.GetTeamMember(ctx, fresh.Id, actor.MattermostUserID, "")
		if memberErr != nil || member == nil || member.TeamId != fresh.Id || member.UserId != actor.MattermostUserID || member.DeleteAt != 0 {
			return nil, false, domainmattermost.ErrTeamForbidden
		}
		result = append(result, safeTeam(fresh))
	}
	return result, end < len(active), nil
}

func (client *Client) CreateTeam(ctx context.Context, principal entity.TeamPrincipal, intent entity.MattermostTeamCreateIntent) (entity.MattermostTeam, error) {
	actor, err := client.index.resolveOwner(principal)
	if err != nil {
		return entity.MattermostTeam{}, domainmattermost.ErrTeamForbidden
	}
	if existing, response, readErr := client.primary.api.GetTeamByName(ctx, intent.Slug, ""); readErr == nil && existing != nil {
		return entity.MattermostTeam{}, domainmattermost.ErrTeamConflict
	} else if readErr != nil && (response == nil || response.StatusCode != http.StatusNotFound) {
		return entity.MattermostTeam{}, providerReadError(response, readErr)
	}
	created, response, err := client.primary.api.CreateTeam(ctx, &model.Team{
		Name: intent.Slug, DisplayName: intent.DisplayName, Type: model.TeamOpen,
		Description: providerOperationMarker(intent.ProviderCorrelation),
	})
	if err != nil {
		if response != nil && response.StatusCode >= http.StatusBadRequest && response.StatusCode < http.StatusInternalServerError {
			return entity.MattermostTeam{}, providerMutationError(response)
		}
		return entity.MattermostTeam{}, domainmattermost.ErrAmbiguousEffect
	}
	if created == nil || invalidProviderID(created.Id) || created.Name != intent.Slug || created.DisplayName != intent.DisplayName {
		return entity.MattermostTeam{}, domainmattermost.ErrAmbiguousEffect
	}
	return client.readCreatedTeam(ctx, actor.MattermostUserID, intent, false)
}

func (client *Client) RecoverCreatedTeam(ctx context.Context, principal entity.TeamPrincipal, intent entity.MattermostTeamCreateIntent) (entity.MattermostTeam, error) {
	actor, err := client.index.resolveOwner(principal)
	if err != nil {
		return entity.MattermostTeam{}, domainmattermost.ErrTeamForbidden
	}
	team, response, err := client.primary.api.GetTeamByName(ctx, intent.Slug, "")
	if err != nil {
		if response != nil && response.StatusCode == http.StatusNotFound {
			return entity.MattermostTeam{}, domainmattermost.ErrTeamNotFound
		}
		return entity.MattermostTeam{}, providerReadError(response, err)
	}
	if !createdTeamMatches(team, intent) {
		return entity.MattermostTeam{}, domainmattermost.ErrTeamConflict
	}
	return client.readCreatedTeam(ctx, actor.MattermostUserID, intent, false)
}

func (client *Client) EnsureCreatedTeamOwner(ctx context.Context, principal entity.TeamPrincipal,
	intent entity.MattermostTeamCreateIntent, providerTeamID string,
) (entity.MattermostTeam, error) {
	actor, err := client.index.resolveOwner(principal)
	if err != nil || invalidProviderID(providerTeamID) {
		return entity.MattermostTeam{}, domainmattermost.ErrTeamForbidden
	}
	team, response, err := client.primary.api.GetTeam(ctx, providerTeamID, "")
	if err != nil {
		return entity.MattermostTeam{}, providerReadError(response, err)
	}
	if !createdTeamMatches(team, intent) {
		return entity.MattermostTeam{}, domainmattermost.ErrTeamConflict
	}
	if err := client.ensureOwnerMembership(ctx, team.Id, actor.MattermostUserID); err != nil {
		return entity.MattermostTeam{}, err
	}
	return client.readCreatedTeam(ctx, actor.MattermostUserID, intent, true)
}

func (client *Client) ReadTeam(ctx context.Context, principal entity.TeamPrincipal, providerTeamID string) (entity.MattermostTeam, error) {
	actor, err := client.index.resolveOwner(principal)
	if err != nil || invalidProviderID(providerTeamID) {
		return entity.MattermostTeam{}, domainmattermost.ErrTeamForbidden
	}
	team, response, err := client.primary.api.GetTeam(ctx, providerTeamID, "")
	if err != nil {
		if response != nil && response.StatusCode == http.StatusNotFound {
			return entity.MattermostTeam{}, domainmattermost.ErrTeamNotFound
		}
		return entity.MattermostTeam{}, providerReadError(response, err)
	}
	if team == nil || team.Id != providerTeamID {
		return entity.MattermostTeam{}, domainmattermost.ErrTeamConflict
	}
	if team.DeleteAt != 0 {
		return safeTeam(team), nil
	}
	member, response, err := client.primary.api.GetTeamMember(ctx, providerTeamID, actor.MattermostUserID, "")
	if err != nil || member == nil || member.TeamId != providerTeamID || member.UserId != actor.MattermostUserID || member.DeleteAt != 0 {
		return entity.MattermostTeam{}, providerReadError(response, err)
	}
	return safeTeam(team), nil
}

func (client *Client) BuildRuntimeRoutes(ctx context.Context, principal entity.TeamPrincipal,
	providerTeamID string,
) ([]entity.MattermostRuntimeRoute, error) {
	team, err := client.ReadTeam(ctx, principal, providerTeamID)
	if err != nil || team.Status != enum.MattermostTeamActive {
		return nil, domainmattermost.ErrTeamForbidden
	}
	routes := make([]entity.MattermostRuntimeRoute, 0)
	for _, template := range client.index.templates {
		if template.OrganizationID != principal.OrganizationID || template.ProjectID != principal.ProjectID ||
			template.LifecycleActorID != principal.ActorID {
			continue
		}
		source, _, readErr := client.primary.api.GetChannel(ctx, template.ChannelID)
		if readErr != nil || source == nil || source.Id != template.ChannelID || source.TeamId != template.TeamID ||
			source.Name == "" {
			return nil, domainmattermost.ErrTeamConflict
		}
		target, response, readErr := client.primary.api.GetChannelByName(ctx, source.Name, providerTeamID, "")
		if readErr != nil || target == nil || target.TeamId != providerTeamID || target.Name != source.Name || target.DeleteAt != 0 {
			return nil, providerReadError(response, readErr)
		}
		boundary := entity.Boundary{
			OrganizationID: template.OrganizationID, ProjectID: template.ProjectID, ChatID: template.ChatID,
			MappingOwnerActorID: template.LifecycleActorID, RoleID: template.RoleID, Locale: template.Locale,
			BotStableKey: template.BotStableKey, TeamID: providerTeamID, ChannelID: target.Id,
			SessionID: template.SessionID,
		}
		routeDigest := runtimeRouteDigest(template.RuntimeKey, boundary, team.ProviderSnapshotSHA256, template.OwnerDelivery)
		routes = append(routes, entity.MattermostRuntimeRoute{
			TemplateKey: template.RuntimeKey, Principal: principal, ProviderTeamID: providerTeamID,
			ProviderSnapshotSHA256: team.ProviderSnapshotSHA256, Boundary: boundary,
			OwnerDelivery: template.OwnerDelivery, RouteDigestSHA256: routeDigest,
		})
	}
	if len(routes) == 0 {
		return nil, domainmattermost.ErrTeamConflict
	}
	slices.SortFunc(routes, func(left, right entity.MattermostRuntimeRoute) int {
		return strings.Compare(left.Boundary.ChannelID, right.Boundary.ChannelID)
	})
	return routes, nil
}

func runtimeRouteDigest(templateKey string, boundary entity.Boundary, providerSnapshot string, ownerDelivery bool) string {
	value := strings.Join([]string{
		"mattermost-runtime-route-v1", templateKey, boundary.OrganizationID, boundary.ProjectID,
		boundary.ChatID, boundary.MappingOwnerActorID, boundary.RoleID, boundary.Locale, boundary.BotStableKey,
		boundary.TeamID, boundary.ChannelID, boundary.SessionID, providerSnapshot,
		fmt.Sprint(ownerDelivery),
	}, "\x00")
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func (client *Client) readCreatedTeam(ctx context.Context, userID string, intent entity.MattermostTeamCreateIntent,
	requireMembership bool,
) (entity.MattermostTeam, error) {
	team, response, err := client.primary.api.GetTeamByName(ctx, intent.Slug, "")
	if err != nil {
		return entity.MattermostTeam{}, providerReadError(response, err)
	}
	if !createdTeamMatches(team, intent) {
		return entity.MattermostTeam{}, domainmattermost.ErrTeamConflict
	}
	if requireMembership {
		member, response, err := client.primary.api.GetTeamMember(ctx, team.Id, userID, "")
		if err != nil || member == nil || member.TeamId != team.Id || member.UserId != userID || member.DeleteAt != 0 {
			return entity.MattermostTeam{}, providerReadError(response, err)
		}
	}
	result := safeTeam(team)
	result.ProviderCausalitySHA256 = providerCausalityDigest(intent.ProviderCorrelation, team)
	return result, nil
}

func createdTeamMatches(team *model.Team, intent entity.MattermostTeamCreateIntent) bool {
	return team != nil && !invalidProviderID(team.Id) && team.Name == intent.Slug &&
		team.DisplayName == intent.DisplayName && team.Description == providerOperationMarker(intent.ProviderCorrelation) &&
		team.Type == model.TeamOpen && team.DeleteAt == 0
}

func providerOperationMarker(correlation string) string {
	return "mattercodex-operation:" + correlation
}

func providerCausalityDigest(correlation string, team *model.Team) string {
	value := strings.Join([]string{"mattermost-team-create-proof-v1", correlation, team.Id,
		time.UnixMilli(team.CreateAt).UTC().Format(time.RFC3339Nano)}, "\x00")
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func (client *Client) ensureOwnerMembership(ctx context.Context, teamID, userID string) error {
	member, response, err := client.primary.api.GetTeamMember(ctx, teamID, userID, "")
	if err == nil && member != nil && member.TeamId == teamID && member.UserId == userID && member.DeleteAt == 0 {
		return nil
	}
	if response == nil || response.StatusCode != http.StatusNotFound {
		return providerReadError(response, err)
	}
	if _, response, err = client.primary.api.AddTeamMember(ctx, teamID, userID); err != nil {
		member, _, readErr := client.primary.api.GetTeamMember(ctx, teamID, userID, "")
		if readErr != nil || member == nil || member.TeamId != teamID || member.UserId != userID || member.DeleteAt != 0 {
			if response != nil && response.StatusCode >= http.StatusBadRequest && response.StatusCode < http.StatusInternalServerError {
				return providerMutationError(response)
			}
			return domainmattermost.ErrAmbiguousEffect
		}
	}
	return nil
}

func safeTeam(team *model.Team) entity.MattermostTeam {
	status := enum.MattermostTeamActive
	if team.DeleteAt != 0 {
		status = enum.MattermostTeamDeleted
	}
	observed := time.Now().UTC()
	return entity.MattermostTeam{
		ProviderTeamID: team.Id, DisplayName: team.DisplayName, Slug: team.Name, Status: status,
		ProviderSnapshotSHA256: providerTeamDigest(team), CreatedAt: time.UnixMilli(team.CreateAt).UTC(),
		UpdatedAt: time.UnixMilli(max(team.UpdateAt, team.DeleteAt)).UTC(), ObservedAt: observed,
	}
}

func providerTeamDigest(team *model.Team) string {
	value := strings.Join([]string{
		"mattermost-team-snapshot-v1", team.Id, team.Name, team.DisplayName,
		team.Description, team.Type, time.UnixMilli(team.CreateAt).UTC().Format(time.RFC3339Nano),
		time.UnixMilli(team.UpdateAt).UTC().Format(time.RFC3339Nano),
		time.UnixMilli(team.DeleteAt).UTC().Format(time.RFC3339Nano),
	}, "\x00")
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func providerReadError(response *model.Response, err error) error {
	if response != nil {
		switch response.StatusCode {
		case http.StatusNotFound:
			return domainmattermost.ErrTeamNotFound
		case http.StatusUnauthorized, http.StatusForbidden:
			return domainmattermost.ErrTeamForbidden
		case http.StatusConflict:
			return domainmattermost.ErrTeamConflict
		}
	}
	if err == nil {
		return domainmattermost.ErrTeamConflict
	}
	return err
}

func providerMutationError(response *model.Response) error {
	if response == nil {
		return domainmattermost.ErrAmbiguousEffect
	}
	switch response.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return domainmattermost.ErrTeamForbidden
	case http.StatusBadRequest, http.StatusConflict:
		return domainmattermost.ErrTeamConflict
	default:
		return domainmattermost.ErrAmbiguousEffect
	}
}
