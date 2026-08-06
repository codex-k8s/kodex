package mattermost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
		teams, _, err := client.primary.api.GetTeamsForUser(ctx, actor.MattermostUserID, "")
		if err != nil || len(teams) > maximumCatalogTeams {
			return errors.New("mattermost team catalog working path is not ready")
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
		fresh, _, readErr := client.primary.api.GetTeam(ctx, listed.Id, "")
		if readErr != nil || fresh == nil || fresh.Id != listed.Id || fresh.DeleteAt != 0 {
			return nil, false, domainmattermost.ErrTeamForbidden
		}
		member, _, memberErr := client.primary.api.GetTeamMember(ctx, fresh.Id, actor.MattermostUserID, "")
		if memberErr != nil || member == nil || member.TeamId != fresh.Id || member.UserId != actor.MattermostUserID || member.DeleteAt != 0 {
			return nil, false, domainmattermost.ErrTeamForbidden
		}
		active = append(active, fresh)
	}
	if int(offset) >= len(active) {
		return []entity.MattermostTeam{}, false, nil
	}
	end := min(int(offset+limit), len(active))
	result := make([]entity.MattermostTeam, 0, end-int(offset))
	for _, team := range active[offset:end] {
		result = append(result, safeTeam(team))
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
	if err := client.ensureOwnerMembership(ctx, created.Id, actor.MattermostUserID); err != nil {
		return entity.MattermostTeam{}, domainmattermost.ErrAmbiguousEffect
	}
	return client.readCreatedTeam(ctx, actor.MattermostUserID, intent)
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
	if team == nil || team.Name != intent.Slug || team.DisplayName != intent.DisplayName || team.Type != model.TeamOpen || team.DeleteAt != 0 {
		return entity.MattermostTeam{}, domainmattermost.ErrTeamConflict
	}
	if err := client.ensureOwnerMembership(ctx, team.Id, actor.MattermostUserID); err != nil {
		return entity.MattermostTeam{}, err
	}
	return client.readCreatedTeam(ctx, actor.MattermostUserID, intent)
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

func (client *Client) readCreatedTeam(ctx context.Context, userID string, intent entity.MattermostTeamCreateIntent) (entity.MattermostTeam, error) {
	team, response, err := client.primary.api.GetTeamByName(ctx, intent.Slug, "")
	if err != nil {
		return entity.MattermostTeam{}, providerReadError(response, err)
	}
	if team == nil || team.Name != intent.Slug || team.DisplayName != intent.DisplayName || team.Type != model.TeamOpen || team.DeleteAt != 0 {
		return entity.MattermostTeam{}, domainmattermost.ErrTeamConflict
	}
	member, response, err := client.primary.api.GetTeamMember(ctx, team.Id, userID, "")
	if err != nil || member == nil || member.TeamId != team.Id || member.UserId != userID || member.DeleteAt != 0 {
		return entity.MattermostTeam{}, providerReadError(response, err)
	}
	return safeTeam(team), nil
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
	value := strings.Join([]string{"mattermost-team-snapshot-v1", team.Id, team.Name, team.DisplayName,
		team.Type, time.UnixMilli(team.CreateAt).UTC().Format(time.RFC3339Nano),
		time.UnixMilli(team.UpdateAt).UTC().Format(time.RFC3339Nano),
		time.UnixMilli(team.DeleteAt).UTC().Format(time.RFC3339Nano)}, "\x00")
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
